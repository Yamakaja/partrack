package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Yamakaja/partrack/internal/config"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

const (
	sessionTTL        = 30 * 24 * time.Hour
	sessionCookieName = "session"
	oidcStateCookie   = "oidc_state"
	oidcStateMaxAge   = 600 // 10 minutes
)

// oidcEntry holds a configured OIDC provider. Discovery runs lazily so an IdP
// that is unreachable at startup does not disable SSO until the next restart.
type oidcEntry struct {
	cfg      config.OIDCProvider
	mu       sync.Mutex
	provider *oidc.Provider // nil until discovery has succeeded
}

// initOIDC registers all configured OIDC providers and attempts eager
// discovery; failures are logged and retried on first use.
func (h *Handler) initOIDC() {
	h.oidcEntries = make(map[string]*oidcEntry)
	for _, p := range h.Config.OIDC {
		entry := &oidcEntry{cfg: p}
		h.oidcEntries[p.Name] = entry
		if _, err := entry.getProvider(context.Background()); err != nil {
			log.Printf("handler: OIDC provider %q failed to initialise (will retry on demand): %v", p.Name, err)
		}
	}
}

// getProvider returns the discovered *oidc.Provider, running discovery on
// first use or retrying after an earlier failure. Safe for concurrent use.
func (e *oidcEntry) getProvider(ctx context.Context) (*oidc.Provider, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.provider != nil {
		return e.provider, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, e.cfg.Issuer)
	if err != nil {
		return nil, err
	}
	e.provider = provider
	return provider, nil
}

// oauth2Config builds the oauth2.Config for a provider. The redirect URL is
// derived from app.base_url (validated at startup), never from request
// headers: TLS is terminated upstream so the request scheme is wrong, and the
// Host header is client-controlled.
func (h *Handler) oauth2Config(entry *oidcEntry, provider *oidc.Provider) oauth2.Config {
	return oauth2.Config{
		ClientID:     entry.cfg.ClientID,
		ClientSecret: entry.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  h.Config.App.BaseURL + "/api/v1/auth/oidc/" + entry.cfg.Name + "/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// dummyBcryptHash is a valid bcrypt hash compared against on the user-not-found /
// no-credential login paths so they take the same time as a real password check,
// closing the timing side channel that would otherwise reveal account existence.
var dummyBcryptHash, _ = bcrypt.GenerateFromPassword([]byte("partrack-dummy-password"), bcrypt.DefaultCost)

// generateToken returns a cryptographically random 32-byte hex string.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashToken returns the SHA-256 hex digest of a raw token.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// userResponse is the common user shape returned in API responses.
type userResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsActive    bool   `json:"is_active"`
	IsAdmin     bool   `json:"is_admin"`
	CreatedAt   string `json:"created_at"`
}


func meRowToUserResponse(r dbgen.GetUserWithMembershipsRow) userResponse {
	return userResponse{
		ID:          r.ID.String(),
		DisplayName: r.DisplayName,
		Email:       r.Email,
		IsActive:    r.IsActive,
		IsAdmin:     r.IsAdmin,
		CreatedAt:   r.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func loginRowToUserResponse(r dbgen.GetUserByEmailRow) userResponse {
	return userResponse{
		ID:          r.ID.String(),
		DisplayName: r.DisplayName,
		Email:       r.Email,
		IsActive:    r.IsActive,
		IsAdmin:     r.IsAdmin,
		CreatedAt:   r.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// Login handles POST /api/v1/auth/login
// Body: {email, password}. Creates a session, sets cookie, returns user object.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required", "BAD_REQUEST")
		return
	}

	// All failure paths below return the same generic 401 (and spend comparable
	// time) so an attacker cannot distinguish unknown-user / no-password /
	// inactive / wrong-password — i.e. cannot enumerate accounts.
	const invalidMsg, invalidCode = "invalid credentials", "INVALID_CREDENTIALS"

	user, err := h.Queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(req.Password)) //nolint:errcheck // equalize timing
			writeError(w, http.StatusUnauthorized, invalidMsg, invalidCode)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	cred, err := h.Queries.GetUserCredential(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(req.Password)) //nolint:errcheck // equalize timing
			writeError(w, http.StatusUnauthorized, invalidMsg, invalidCode)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, invalidMsg, invalidCode)
		return
	}
	// Password is correct; an inactive account still returns the generic error so
	// account state is not disclosed.
	if !user.IsActive {
		writeError(w, http.StatusUnauthorized, invalidMsg, invalidCode)
		return
	}

	rawToken, _, expiresAt, err := h.createSession(r.Context(), user.ID, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.setSessionCookie(w, r, rawToken, expiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"user": loginRowToUserResponse(user)})
}

// Logout handles POST /api/v1/auth/logout
// Invalidates the current session.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	// Prefer deleting by raw token hash (most precise).
	var rawToken string
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		rawToken = cookie.Value
	} else {
		auth := r.Header.Get("Authorization")
		if len(auth) > 7 && auth[:7] == "Bearer " {
			rawToken = auth[7:]
		}
	}

	if rawToken != "" {
		hash := hashToken(rawToken)
		if err := h.Queries.DeleteSessionByTokenHash(r.Context(), hash); err != nil {
			log.Printf("handler: logout: %v", err)
		}
		h.Cache.EvictSession(hash)
	} else if user != nil && user.SessionID() != "" {
		var sid, uid pgtype.UUID
		if sid.Scan(user.SessionID()) == nil && uid.Scan(user.ID) == nil {
			h.Queries.DeleteSession(r.Context(), dbgen.DeleteSessionParams{ID: sid, UserID: uid}) //nolint:errcheck
		}
		h.Cache.EvictSessionByID(user.SessionID())
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// OIDCInitiate handles GET /api/v1/auth/oidc/{provider}
// Initiates the OIDC authorization code flow.
func (h *Handler) OIDCInitiate(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.oidcEntries[chi.URLParam(r, "provider")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown OIDC provider", "NOT_FOUND")
		return
	}
	provider, err := entry.getProvider(r.Context())
	if err != nil {
		log.Printf("handler: OIDC provider %q unavailable: %v", entry.cfg.Name, err)
		writeError(w, http.StatusServiceUnavailable, "OIDC provider unavailable", "OIDC_PROVIDER_UNAVAILABLE")
		return
	}

	state, err := generateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	nonce, err := generateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	verifier := oauth2.GenerateVerifier()

	// The cookie holds all three per-login secrets (state, nonce, PKCE
	// verifier). They are random values in an HttpOnly cookie that only the
	// legitimate browser holds, so no signing is needed.
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state + "." + nonce + "." + verifier,
		Path:     "/api/v1/auth/oidc",
		MaxAge:   oidcStateMaxAge,
		HttpOnly: true,
		Secure:   h.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	cfg := h.oauth2Config(entry, provider)
	http.Redirect(w, r, cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// redirectLoginError sends the browser back to the login page with a
// machine-readable error code. The callback runs as a top-level navigation,
// so a JSON error body would render as raw text in the user's browser.
func redirectLoginError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusFound)
}

// OIDCCallback handles GET /api/v1/auth/oidc/{provider}/callback
// Exchanges the authorization code for a session. Failures redirect back to
// the login page with an error code rather than writing JSON.
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.oidcEntries[chi.URLParam(r, "provider")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown OIDC provider", "NOT_FOUND")
		return
	}
	provider, err := entry.getProvider(r.Context())
	if err != nil {
		log.Printf("handler: OIDC provider %q unavailable: %v", entry.cfg.Name, err)
		redirectLoginError(w, r, "OIDC_PROVIDER_UNAVAILABLE")
		return
	}

	// Verify CSRF state and recover the nonce + PKCE verifier minted at initiate.
	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil {
		redirectLoginError(w, r, "INVALID_STATE")
		return
	}
	parts := strings.SplitN(stateCookie.Value, ".", 3)
	if len(parts) != 3 {
		redirectLoginError(w, r, "INVALID_STATE")
		return
	}
	state, nonce, verifier := parts[0], parts[1], parts[2]
	if subtle.ConstantTimeCompare([]byte(state), []byte(r.URL.Query().Get("state"))) != 1 {
		redirectLoginError(w, r, "INVALID_STATE")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    "",
		Path:     "/api/v1/auth/oidc",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})

	callbackCfg := h.oauth2Config(entry, provider)
	oauthToken, err := callbackCfg.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		redirectLoginError(w, r, "OIDC_EXCHANGE_FAILED")
		return
	}

	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok {
		redirectLoginError(w, r, "OIDC_NO_ID_TOKEN")
		return
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: entry.cfg.ClientID}).Verify(r.Context(), rawIDToken)
	if err != nil {
		redirectLoginError(w, r, "OIDC_VERIFY_FAILED")
		return
	}
	// Bind the ID token to this browser's login attempt (replay protection).
	if idToken.Nonce != nonce {
		redirectLoginError(w, r, "OIDC_VERIFY_FAILED")
		return
	}

	var claims oidcClaims
	if err := idToken.Claims(&claims); err != nil {
		redirectLoginError(w, r, "OIDC_CLAIMS_ERROR")
		return
	}

	user, err := h.resolveOIDCUser(r.Context(), entry.cfg, claims)
	if err != nil {
		switch {
		case errors.Is(err, errOIDCNotProvisioned):
			redirectLoginError(w, r, "PROVISIONING_DISABLED")
		case errors.Is(err, errOIDCNoEmail):
			redirectLoginError(w, r, "OIDC_NO_EMAIL")
		case errors.Is(err, errOIDCEmailUnverified):
			redirectLoginError(w, r, "OIDC_EMAIL_UNVERIFIED")
		default:
			log.Printf("handler: OIDC user resolution failed: %v", err)
			redirectLoginError(w, r, "INTERNAL_ERROR")
		}
		return
	}
	if !user.IsActive {
		redirectLoginError(w, r, "ACCOUNT_INACTIVE")
		return
	}

	rawToken, _, expiresAt, err := h.createSession(r.Context(), user.ID, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		redirectLoginError(w, r, "INTERNAL_ERROR")
		return
	}

	h.setSessionCookie(w, r, rawToken, expiresAt)
	http.Redirect(w, r, "/", http.StatusFound)
}

var (
	errOIDCNotProvisioned  = errors.New("auto-provisioning disabled")
	errOIDCNoEmail         = errors.New("id_token has no email claim")
	errOIDCEmailUnverified = errors.New("id_token email is not verified")
)

// oidcClaims is the subset of ID token claims partrack consumes.
type oidcClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// resolveOIDCUser finds the user for an OIDC login. Resolution order:
//  1. an identity already linked to this (provider, subject)
//  2. auto-link to an existing user with the same verified email
//  3. auto-provision a new user, if enabled for the provider
//
// Linking and provisioning key on the email claim, so both require the IdP to
// attest the address with email_verified=true — otherwise anyone able to set
// an arbitrary unverified email at the IdP could take over the matching account.
func (h *Handler) resolveOIDCUser(ctx context.Context, cfg config.OIDCProvider, claims oidcClaims) (dbgen.GetUserWithMembershipsRow, error) {
	var zero dbgen.GetUserWithMembershipsRow

	identity, err := h.Queries.GetOIDCIdentity(ctx, dbgen.GetOIDCIdentityParams{
		Provider: cfg.Name,
		Subject:  claims.Subject,
	})
	if err == nil {
		return h.Queries.GetUserWithMemberships(ctx, identity.UserID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}

	if claims.Email == "" {
		return zero, errOIDCNoEmail
	}
	if !claims.EmailVerified {
		return zero, errOIDCEmailUnverified
	}

	// Auto-link: attach this identity to the existing user with the same email.
	existing, err := h.Queries.GetUserByEmail(ctx, claims.Email)
	if err == nil {
		_, err = h.Queries.CreateOIDCIdentity(ctx, dbgen.CreateOIDCIdentityParams{
			UserID:        existing.ID,
			Provider:      cfg.Name,
			Subject:       claims.Subject,
			ProviderEmail: &claims.Email,
		})
		if err != nil {
			return zero, fmt.Errorf("link oidc identity: %w", err)
		}
		return h.Queries.GetUserWithMemberships(ctx, existing.ID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, err
	}

	if !cfg.AutoProvision {
		return zero, errOIDCNotProvisioned
	}

	displayName := claims.Name
	if displayName == "" {
		displayName = claims.Email
	}

	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := h.Queries.WithTx(tx)

	user, err := qtx.CreateUser(ctx, dbgen.CreateUserParams{
		DisplayName: displayName,
		Email:       claims.Email,
	})
	if err != nil {
		return zero, fmt.Errorf("create user: %w", err)
	}
	_, err = qtx.CreateOIDCIdentity(ctx, dbgen.CreateOIDCIdentityParams{
		UserID:        user.ID,
		Provider:      cfg.Name,
		Subject:       claims.Subject,
		ProviderEmail: &claims.Email,
	})
	if err != nil {
		return zero, fmt.Errorf("create oidc identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return dbgen.GetUserWithMembershipsRow{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		IsActive:    user.IsActive,
		IsAdmin:     user.IsAdmin,
		CreatedAt:   user.CreatedAt,
	}, nil
}

// OIDCProviders handles GET /api/v1/auth/providers
// Lists configured SSO providers so the login page can render buttons.
func (h *Handler) OIDCProviders(w http.ResponseWriter, r *http.Request) {
	type providerItem struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	items := make([]providerItem, 0, len(h.Config.OIDC))
	for _, p := range h.Config.OIDC {
		displayName := p.DisplayName
		if displayName == "" {
			displayName = p.Name
		}
		items = append(items, providerItem{Name: p.Name, DisplayName: displayName})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": items})
}

// Me handles GET /api/v1/auth/me
// Returns the current user profile and team memberships.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	ctxUser := middleware.UserFromContext(r.Context())
	if ctxUser == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}

	var userID pgtype.UUID
	if err := userID.Scan(ctxUser.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	user, err := h.Queries.GetUserWithMemberships(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	type membershipItem struct {
		TeamID   string `json:"team_id"`
		TeamName string `json:"team_name"`
		Role     string `json:"role"`
	}

	var items []membershipItem
	if ctxUser.IsAdmin {
		teams, err := h.Queries.ListAllTeams(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		items = make([]membershipItem, len(teams))
		for i, t := range teams {
			items[i] = membershipItem{
				TeamID:   t.ID.String(),
				TeamName: t.Name,
				Role:     "admin",
			}
		}
	} else {
		memberships, err := h.Queries.ListUserTeamMemberships(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		items = make([]membershipItem, len(memberships))
		for i, m := range memberships {
			items[i] = membershipItem{
				TeamID:   m.TeamID.String(),
				TeamName: m.TeamName,
				Role:     string(m.Role),
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":        meRowToUserResponse(user),
		"memberships": items,
	})
}

// createSession generates a token, stores the session, and returns (rawToken, hash, expiresAt, err).
func (h *Handler) createSession(ctx context.Context, userID pgtype.UUID, userAgent, ipAddr string) (string, string, time.Time, error) {
	rawToken, err := generateToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	tokenHash := hashToken(rawToken)
	expiresAt := time.Now().Add(sessionTTL)

	_, err = h.Queries.CreateSession(ctx, dbgen.CreateSessionParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		UserAgent: &userAgent,
		IpAddress: &ipAddr,
	})
	if err != nil {
		return "", "", time.Time{}, err
	}
	return rawToken, tokenHash, expiresAt, nil
}

// cookieSecure reports whether auth cookies should carry the Secure flag. TLS is
// normally terminated at a reverse proxy (so r.TLS is nil), hence the config flag
// is authoritative; it defaults to true and should only be disabled for local
// HTTP development.
func (h *Handler) cookieSecure(r *http.Request) bool {
	return h.Config.App.SecureCookies || r.TLS != nil
}

// setSessionCookie writes the HTTP-only session cookie to the response.
func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, rawToken string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// GetResetPasswordInfo handles GET /api/v1/auth/reset-password?token=...
// Validates the token and returns the target user's display name so the
// frontend can show "you are resetting the password for <name>".
func (h *Handler) GetResetPasswordInfo(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("token")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "token is required", "TOKEN_INVALID")
		return
	}

	row, err := h.Queries.GetPasswordResetToken(r.Context(), hashToken(raw))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired token", "TOKEN_INVALID")
		return
	}
	if row.UsedAt.Valid || row.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusBadRequest, "invalid or expired token", "TOKEN_INVALID")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"display_name": row.DisplayName,
		"email":        row.Email,
		"expires_at":   row.ExpiresAt.Time.UTC().Format(time.RFC3339),
	})
}

// CompletePasswordReset handles POST /api/v1/auth/reset-password
// Consumes the token, sets the new password, and logs the user in.
// Body: {token, password}
func (h *Handler) CompletePasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "token and password are required", "BAD_REQUEST")
		return
	}

	row, err := h.Queries.GetPasswordResetToken(r.Context(), hashToken(req.Token))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired token", "TOKEN_INVALID")
		return
	}
	if row.UsedAt.Valid || row.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusBadRequest, "invalid or expired token", "TOKEN_INVALID")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := h.Queries.AdminSetPassword(r.Context(), dbgen.AdminSetPasswordParams{
		UserID:       row.UserID,
		PasswordHash: string(hash),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := h.Queries.UsePasswordResetToken(r.Context(), row.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	rawToken, _, expiresAt, err := h.createSession(r.Context(), row.UserID, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.setSessionCookie(w, r, rawToken, expiresAt)

	user, err := h.Queries.GetUserWithMemberships(r.Context(), row.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	memberships, err := h.Queries.ListUserTeamMemberships(r.Context(), row.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	type membershipItem struct {
		TeamID   string `json:"team_id"`
		TeamName string `json:"team_name"`
		Role     string `json:"role"`
	}
	items := make([]membershipItem, len(memberships))
	for i, m := range memberships {
		items[i] = membershipItem{TeamID: m.TeamID.String(), TeamName: m.TeamName, Role: string(m.Role)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": meRowToUserResponse(user), "memberships": items})
}
