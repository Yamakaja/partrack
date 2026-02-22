package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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

// oidcEntry holds a cached OIDC provider and its config.
type oidcEntry struct {
	provider *oidc.Provider
	cfg      config.OIDCProvider
}

// initOIDC eagerly initialises all configured OIDC providers.
// Providers that fail (e.g. unreachable at startup) are skipped and their
// endpoints return 503.
func (h *Handler) initOIDC() {
	h.oidcEntries = make(map[string]*oidcEntry)
	for _, p := range h.Config.OIDC {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		provider, err := oidc.NewProvider(ctx, p.Issuer)
		cancel()
		if err != nil {
			log.Printf("handler: OIDC provider %q failed to initialise: %v", p.Name, err)
			continue
		}
		h.oidcEntries[p.Name] = &oidcEntry{provider: provider, cfg: p}
	}
}

// oauth2Config builds the oauth2.Config for a provider, deriving the redirect
// URL from the current request host.
func (e *oidcEntry) oauth2Config(r *http.Request) oauth2.Config {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return oauth2.Config{
		ClientID:     e.cfg.ClientID,
		ClientSecret: e.cfg.ClientSecret,
		Endpoint:     e.provider.Endpoint(),
		RedirectURL:  fmt.Sprintf("%s://%s/api/v1/auth/oidc/%s/callback", scheme, r.Host, e.cfg.Name),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

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
	CreatedAt   string `json:"created_at"`
}

func toUserResponse(u dbgen.User) userResponse {
	return userResponse{
		ID:          u.ID.String(),
		DisplayName: u.DisplayName,
		Email:       u.Email,
		IsActive:    u.IsActive,
		CreatedAt:   u.CreatedAt.Time.UTC().Format(time.RFC3339),
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

	user, err := h.Queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid credentials", "INVALID_CREDENTIALS")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if !user.IsActive {
		writeError(w, http.StatusUnauthorized, "account is inactive", "ACCOUNT_INACTIVE")
		return
	}

	cred, err := h.Queries.GetUserCredential(r.Context(), user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "password login not available for this account", "NO_PASSWORD")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials", "INVALID_CREDENTIALS")
		return
	}

	rawToken, _, expiresAt, err := h.createSession(r.Context(), user.ID, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	setSessionCookie(w, rawToken, expiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserResponse(user)})
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

	state, err := generateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     "/api/v1/auth/oidc",
		MaxAge:   oidcStateMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	cfg := entry.oauth2Config(r)
	http.Redirect(w, r, cfg.AuthCodeURL(state), http.StatusFound)
}

// OIDCCallback handles GET /api/v1/auth/oidc/{provider}/callback
// Exchanges the authorization code for a session.
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.oidcEntries[chi.URLParam(r, "provider")]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown OIDC provider", "NOT_FOUND")
		return
	}

	// Verify CSRF state.
	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		writeError(w, http.StatusBadRequest, "invalid state parameter", "INVALID_STATE")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    "",
		Path:     "/api/v1/auth/oidc",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	callbackCfg := entry.oauth2Config(r)
	oauthToken, err := callbackCfg.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "code exchange failed", "OIDC_EXCHANGE_FAILED")
		return
	}

	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok {
		writeError(w, http.StatusBadRequest, "missing id_token", "OIDC_NO_ID_TOKEN")
		return
	}
	idToken, err := entry.provider.Verifier(&oidc.Config{ClientID: entry.cfg.ClientID}).Verify(r.Context(), rawIDToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "id_token verification failed", "OIDC_VERIFY_FAILED")
		return
	}

	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read claims", "OIDC_CLAIMS_ERROR")
		return
	}

	user, err := h.resolveOIDCUser(r.Context(), entry.cfg.Name, claims.Subject, claims.Email, claims.Name, entry.cfg)
	if err != nil {
		if errors.Is(err, errOIDCNotProvisioned) {
			writeError(w, http.StatusForbidden, "auto-provisioning is disabled for this provider", "PROVISIONING_DISABLED")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if !user.IsActive {
		writeError(w, http.StatusUnauthorized, "account is inactive", "ACCOUNT_INACTIVE")
		return
	}

	rawToken, _, expiresAt, err := h.createSession(r.Context(), user.ID, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	setSessionCookie(w, rawToken, expiresAt)
	http.Redirect(w, r, "/", http.StatusFound)
}

var errOIDCNotProvisioned = errors.New("auto-provisioning disabled")

// resolveOIDCUser finds or auto-provisions the user for an OIDC login.
func (h *Handler) resolveOIDCUser(ctx context.Context, provider, subject, email, name string, cfg config.OIDCProvider) (dbgen.User, error) {
	identity, err := h.Queries.GetOIDCIdentity(ctx, dbgen.GetOIDCIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	if err == nil {
		return h.Queries.GetUserWithMemberships(ctx, identity.UserID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return dbgen.User{}, err
	}

	if !cfg.AutoProvision {
		return dbgen.User{}, errOIDCNotProvisioned
	}

	displayName := name
	if displayName == "" {
		displayName = email
	}
	user, err := h.Queries.CreateUser(ctx, dbgen.CreateUserParams{
		DisplayName: displayName,
		Email:       email,
	})
	if err != nil {
		return dbgen.User{}, fmt.Errorf("create user: %w", err)
	}
	_, err = h.Queries.CreateOIDCIdentity(ctx, dbgen.CreateOIDCIdentityParams{
		UserID:        user.ID,
		Provider:      provider,
		Subject:       subject,
		ProviderEmail: &email,
	})
	if err != nil {
		return dbgen.User{}, fmt.Errorf("create oidc identity: %w", err)
	}
	return user, nil
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

	memberships, err := h.Queries.ListUserTeamMemberships(r.Context(), userID)
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
		items[i] = membershipItem{
			TeamID:   m.TeamID.String(),
			TeamName: m.TeamName,
			Role:     string(m.Role),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":        toUserResponse(user),
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

// setSessionCookie writes the HTTP-only session cookie to the response.
func setSessionCookie(w http.ResponseWriter, rawToken string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    rawToken,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
