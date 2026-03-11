package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// oauthState is JSON-encoded into the signed state parameter.
type oauthState struct {
	TeamID        string `json:"team_id"`
	UserID        string `json:"user_id"`
	DistributorID string `json:"distributor_id"`
	ReturnTo      string `json:"return_to"`
}

// signState encodes state as base64url(JSON).base64url(HMAC-SHA256).
func signState(state oauthState, secret string) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	payloadEnc := base64.URLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadEnc)) //nolint:errcheck
	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return payloadEnc + "." + sig, nil
}

// verifyAndDecodeState verifies the HMAC signature and returns the decoded state.
func verifyAndDecodeState(raw, secret string) (oauthState, error) {
	dot := strings.LastIndex(raw, ".")
	if dot < 0 {
		return oauthState{}, errors.New("invalid state format")
	}
	payloadEnc, sigEnc := raw[:dot], raw[dot+1:]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadEnc)) //nolint:errcheck
	expectedSig := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sigEnc), []byte(expectedSig)) {
		return oauthState{}, errors.New("invalid state signature")
	}
	payload, err := base64.URLEncoding.DecodeString(payloadEnc)
	if err != nil {
		return oauthState{}, err
	}
	var s oauthState
	return s, json.Unmarshal(payload, &s)
}

// oauthRedirectURI constructs the absolute OAuth callback URI for the given distributor api_type.
func (h *Handler) oauthRedirectURI(apiType string) string {
	return h.Config.App.BaseURL + "/api/v1/oauth/callback/" + apiType
}

// InitiateOAuth handles GET /api/v1/teams/{teamId}/oauth/{distributorApiType}/initiate
// Role: purchaser+
// Returns {"auth_url": "..."} for the frontend to redirect the user.
func (h *Handler) InitiateOAuth(w http.ResponseWriter, r *http.Request) {
	apiType := chi.URLParam(r, "distributorApiType")

	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "INVALID_TEAM_ID")
		return
	}

	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" {
		returnTo = "/"
	}

	ctx := r.Context()

	// Look up the distributor record by api_type.
	dist, err := h.Queries.GetDistributorByApiType(ctx, ptrTo(apiType))
	if err != nil {
		writeError(w, http.StatusNotFound, "distributor not configured", "DISTRIBUTOR_NOT_FOUND")
		return
	}

	// Get this team's credentials for the distributor.
	cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        teamID,
		DistributorID: dist.ID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "distributor not configured for this team", "DISTRIBUTOR_CONFIG_NOT_FOUND")
		return
	}

	// Build the signed state param (includes user ID so the callback can scope the token).
	user := middleware.UserFromContext(r.Context())
	signed, err := signState(oauthState{
		TeamID:        teamID.String(),
		UserID:        user.ID,
		DistributorID: dist.ID.String(),
		ReturnTo:      returnTo,
	}, h.Config.Auth.SessionSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build state", "INTERNAL_ERROR")
		return
	}

	// Assert ThreeLeggedAuthClient.
	client := h.Distributors.Lookup(apiType)
	tlc, ok := client.(distributor.ThreeLeggedAuthClient)
	if !ok {
		writeError(w, http.StatusInternalServerError, "distributor does not support OAuth", "OAUTH_NOT_SUPPORTED")
		return
	}

	authURL, err := tlc.AuthorizeURL(cfg.ApiCredentials, h.oauthRedirectURI(apiType), signed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build authorization URL", "OAUTH_URL_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
}

// OAuthCallback handles GET /api/v1/oauth/callback/{distributorApiType} (unauthenticated).
// The distributor redirects here after user authorization.
func (h *Handler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	apiType := chi.URLParam(r, "distributorApiType")
	q := r.URL.Query()

	// Verify HMAC and decode state.
	stateRaw := q.Get("state")
	state, err := verifyAndDecodeState(stateRaw, h.Config.Auth.SessionSecret)
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	returnTo := state.ReturnTo
	if returnTo == "" {
		returnTo = "/"
	}

	// Handle error from distributor.
	if errParam := q.Get("error"); errParam != "" {
		http.Redirect(w, r, returnTo+"?oauth_error="+errParam, http.StatusFound)
		return
	}

	code := q.Get("code")
	if code == "" {
		http.Redirect(w, r, returnTo+"?oauth_error=missing_code", http.StatusFound)
		return
	}

	ctx := r.Context()

	// Parse IDs from state.
	var teamID, distributorID, userID pgtype.UUID
	if err := teamID.Scan(state.TeamID); err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=invalid_state", http.StatusFound)
		return
	}
	if err := distributorID.Scan(state.DistributorID); err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=invalid_state", http.StatusFound)
		return
	}
	if err := userID.Scan(state.UserID); err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=invalid_state", http.StatusFound)
		return
	}

	// Get credentials for this team+distributor.
	cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        teamID,
		DistributorID: distributorID,
	})
	if err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=config_not_found", http.StatusFound)
		return
	}

	// Exchange code for tokens.
	client := h.Distributors.Lookup(apiType)
	tlc, ok := client.(distributor.ThreeLeggedAuthClient)
	if !ok {
		http.Redirect(w, r, returnTo+"?oauth_error=oauth_not_supported", http.StatusFound)
		return
	}

	tokens, err := tlc.ExchangeCode(ctx, cfg.ApiCredentials, code, h.oauthRedirectURI(apiType))
	if err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=token_exchange_failed", http.StatusFound)
		return
	}

	// Compute expiry and store. Account ID is resolved lazily by EnrichCredentials.
	expiresAt := pgtype.Timestamptz{}
	expiresAt.Scan(time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)) //nolint:errcheck

	_, err = h.Queries.UpsertOAuthToken(ctx, dbgen.UpsertOAuthTokenParams{
		TeamID:        teamID,
		UserID:        userID,
		DistributorID: distributorID,
		AccessToken:   tokens.AccessToken,
		RefreshToken:  tokens.RefreshToken,
		ExpiresAt:     expiresAt,
		AccountID:     "", // resolved lazily on first EnrichCredentials call
	})
	if err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=store_failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, returnTo+"?oauth_success=1", http.StatusFound)
}

// GetOAuthStatus handles GET /api/v1/teams/{teamId}/oauth/{distributorApiType}/status
// Role: purchaser+
// Returns the current user's OAuth connection status for the distributor.
func (h *Handler) GetOAuthStatus(w http.ResponseWriter, r *http.Request) {
	apiType := chi.URLParam(r, "distributorApiType")

	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "INVALID_TEAM_ID")
		return
	}

	ctx := r.Context()
	user := middleware.UserFromContext(ctx)

	dist, err := h.Queries.GetDistributorByApiType(ctx, ptrTo(apiType))
	if err != nil {
		writeError(w, http.StatusNotFound, "distributor not found", "DISTRIBUTOR_NOT_FOUND")
		return
	}

	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid user ID", "INTERNAL_ERROR")
		return
	}

	tok, err := h.Queries.GetOAuthToken(ctx, dbgen.GetOAuthTokenParams{
		TeamID:        teamID,
		UserID:        userID,
		DistributorID: dist.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{
			"connected":  false,
			"account_id": "",
			"updated_at": "",
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"connected":  true,
		"account_id": tok.AccountID,
		"updated_at": tok.UpdatedAt.Time.Format(time.RFC3339),
	})
}

// DisconnectOAuth handles DELETE /api/v1/teams/{teamId}/oauth/{distributorApiType}/token
// Role: purchaser+
// Removes the current user's stored OAuth token for the distributor.
func (h *Handler) DisconnectOAuth(w http.ResponseWriter, r *http.Request) {
	apiType := chi.URLParam(r, "distributorApiType")

	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "INVALID_TEAM_ID")
		return
	}

	ctx := r.Context()
	user := middleware.UserFromContext(ctx)

	dist, err := h.Queries.GetDistributorByApiType(ctx, ptrTo(apiType))
	if err != nil {
		writeError(w, http.StatusNotFound, "distributor not found", "DISTRIBUTOR_NOT_FOUND")
		return
	}

	var userID pgtype.UUID
	if err := userID.Scan(user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid user ID", "INTERNAL_ERROR")
		return
	}

	if err := h.Queries.DeleteOAuthToken(ctx, dbgen.DeleteOAuthTokenParams{
		TeamID:        teamID,
		UserID:        userID,
		DistributorID: dist.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// dbCredentialProvider adapts DB queries to distributor.CredentialProvider
// for a specific (team, user, distributor) triple.
type dbCredentialProvider struct {
	queries       *dbgen.Queries
	teamID        pgtype.UUID
	userID        pgtype.UUID
	distributorID pgtype.UUID
}

func (p *dbCredentialProvider) GetOAuthToken(ctx context.Context) (*distributor.StoredToken, error) {
	tok, err := p.queries.GetOAuthToken(ctx, dbgen.GetOAuthTokenParams{
		TeamID:        p.teamID,
		UserID:        p.userID,
		DistributorID: p.distributorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // no token stored
	}
	if err != nil {
		return nil, err
	}
	return &distributor.StoredToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.ExpiresAt.Time,
		AccountID:    tok.AccountID,
	}, nil
}

func (p *dbCredentialProvider) StoreOAuthToken(ctx context.Context, tok *distributor.StoredToken) error {
	exp := pgtype.Timestamptz{}
	exp.Scan(tok.ExpiresAt) //nolint:errcheck
	_, err := p.queries.UpsertOAuthToken(ctx, dbgen.UpsertOAuthTokenParams{
		TeamID:        p.teamID,
		UserID:        p.userID,
		DistributorID: p.distributorID,
		AccessToken:   tok.AccessToken,
		RefreshToken:  tok.RefreshToken,
		ExpiresAt:     exp,
		AccountID:     tok.AccountID,
	})
	return err
}

func (p *dbCredentialProvider) DeleteOAuthToken(ctx context.Context) error {
	return p.queries.DeleteOAuthToken(ctx, dbgen.DeleteOAuthTokenParams{
		TeamID:        p.teamID,
		UserID:        p.userID,
		DistributorID: p.distributorID,
	})
}

// ptrTo returns a pointer to v.
func ptrTo[T any](v T) *T { return &v }
