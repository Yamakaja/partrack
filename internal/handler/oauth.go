package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// oauthState is JSON-encoded into the base64url state parameter.
type oauthState struct {
	TeamID        string `json:"team_id"`
	DistributorID string `json:"distributor_id"`
	ReturnTo      string `json:"return_to"`
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

	// Build the state param.
	stateData, _ := json.Marshal(oauthState{
		TeamID:        teamID.String(),
		DistributorID: dist.ID.String(),
		ReturnTo:      returnTo,
	})
	state := base64.URLEncoding.EncodeToString(stateData)

	// Assert ThreeLeggedAuthClient.
	client := h.Distributors.Lookup(apiType)
	tlc, ok := client.(distributor.ThreeLeggedAuthClient)
	if !ok {
		writeError(w, http.StatusInternalServerError, "distributor does not support OAuth", "OAUTH_NOT_SUPPORTED")
		return
	}

	authURL, err := tlc.AuthorizeURL(cfg.ApiCredentials, h.oauthRedirectURI(apiType), state)
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

	// Decode state.
	stateRaw := q.Get("state")
	stateBytes, err := base64.URLEncoding.DecodeString(stateRaw)
	if err != nil {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	var state oauthState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
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
	var teamID, distributorID pgtype.UUID
	if err := teamID.Scan(state.TeamID); err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=invalid_state", http.StatusFound)
		return
	}
	if err := distributorID.Scan(state.DistributorID); err != nil {
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

// dbCredentialProvider adapts DB queries to distributor.CredentialProvider
// for a specific (team, distributor) pair.
type dbCredentialProvider struct {
	queries       *dbgen.Queries
	teamID        pgtype.UUID
	distributorID pgtype.UUID
}

func (p *dbCredentialProvider) GetOAuthToken(ctx context.Context) (*distributor.StoredToken, error) {
	tok, err := p.queries.GetOAuthToken(ctx, dbgen.GetOAuthTokenParams{
		TeamID:        p.teamID,
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
		DistributorID: p.distributorID,
	})
}

// ptrTo returns a pointer to v.
func ptrTo[T any](v T) *T { return &v }
