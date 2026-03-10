package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

// ErrOAuthRequired is returned by getThreeLeggedCreds when no valid token exists.
var ErrOAuthRequired = errors.New("oauth_required")

// digikeyRedirectURI constructs the absolute DigiKey OAuth callback URI.
func (h *Handler) digikeyRedirectURI() string {
	base := h.Config.App.BaseURL
	return base + "/api/v1/oauth/callback/digikey"
}

// InitiateOAuth handles GET /api/v1/teams/{teamId}/oauth/digikey/initiate
// Role: purchaser+
// Returns {"auth_url": "..."} for the frontend to redirect the user.
func (h *Handler) InitiateOAuth(w http.ResponseWriter, r *http.Request) {
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

	// Look up the DigiKey distributor record.
	dist, err := h.Queries.GetDistributorByApiType(ctx, ptrTo("digikey"))
	if err != nil {
		writeError(w, http.StatusNotFound, "DigiKey distributor not configured", "DISTRIBUTOR_NOT_FOUND")
		return
	}

	// Get this team's DigiKey credentials.
	cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        teamID,
		DistributorID: dist.ID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "DigiKey not configured for this team", "DISTRIBUTOR_CONFIG_NOT_FOUND")
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
	client := h.Distributors.Lookup("digikey")
	tlc, ok := client.(distributor.ThreeLeggedAuthClient)
	if !ok {
		writeError(w, http.StatusInternalServerError, "DigiKey client does not support OAuth", "OAUTH_NOT_SUPPORTED")
		return
	}

	authURL, err := tlc.AuthorizeURL(cfg.ApiCredentials, h.digikeyRedirectURI(), state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build authorization URL", "OAUTH_URL_FAILED")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
}

// OAuthCallback handles GET /api/v1/oauth/callback/digikey (unauthenticated).
// DigiKey redirects here after user authorization.
func (h *Handler) OAuthCallback(w http.ResponseWriter, r *http.Request) {
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

	// Handle error from DigiKey.
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
	client := h.Distributors.Lookup("digikey")
	tlc, ok := client.(distributor.ThreeLeggedAuthClient)
	if !ok {
		http.Redirect(w, r, returnTo+"?oauth_error=oauth_not_supported", http.StatusFound)
		return
	}

	tokens, err := tlc.ExchangeCode(ctx, cfg.ApiCredentials, code, h.digikeyRedirectURI())
	if err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=token_exchange_failed", http.StatusFound)
		return
	}

	// Resolve account ID using the freshly-obtained 3-legged token.
	accountID := ""
	if resolver, ok := client.(distributor.AccountIDResolver); ok {
		if id, err := resolver.GetPrimaryAccountID(ctx, cfg.ApiCredentials, tokens.AccessToken); err == nil {
			accountID = id
		}
		// Non-fatal: account ID may not be resolvable in all contexts (e.g. sandbox).
	}

	// Compute expiry and store.
	expiresAt := pgtype.Timestamptz{}
	expiresAt.Scan(time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)) //nolint:errcheck

	_, err = h.Queries.UpsertOAuthToken(ctx, dbgen.UpsertOAuthTokenParams{
		TeamID:        teamID,
		DistributorID: distributorID,
		AccessToken:   tokens.AccessToken,
		RefreshToken:  tokens.RefreshToken,
		ExpiresAt:     expiresAt,
		AccountID:     accountID,
	})
	if err != nil {
		http.Redirect(w, r, returnTo+"?oauth_error=store_failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, returnTo+"?oauth_success=1", http.StatusFound)
}

// getThreeLeggedCreds retrieves (and refreshes if needed) a 3-legged OAuth token
// for the distributor on the given order, and injects it into the credentials JSON.
// Returns ErrOAuthRequired if no token is stored or refresh fails.
func (h *Handler) getThreeLeggedCreds(r *http.Request, order dbgen.GetOrderRow) (json.RawMessage, error) {
	ctx := r.Context()

	tok, err := h.Queries.GetOAuthToken(ctx, dbgen.GetOAuthTokenParams{
		TeamID:        order.TeamID,
		DistributorID: order.DistributorID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOAuthRequired
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth token: %w", err)
	}

	accessToken := tok.AccessToken

	// Refresh if expiring within 60 seconds.
	if time.Now().Add(60 * time.Second).After(tok.ExpiresAt.Time) {
		cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
			TeamID:        order.TeamID,
			DistributorID: order.DistributorID,
		})
		if err != nil {
			return nil, ErrOAuthRequired
		}

		client := h.Distributors.Lookup("digikey")
		tlc, ok := client.(distributor.ThreeLeggedAuthClient)
		if !ok {
			return nil, ErrOAuthRequired
		}

		newToks, err := tlc.RefreshToken(ctx, cfg.ApiCredentials, tok.RefreshToken)
		if err != nil {
			// Refresh failed — delete stored token and require re-auth.
			_ = h.Queries.DeleteOAuthToken(ctx, dbgen.DeleteOAuthTokenParams{
				TeamID:        order.TeamID,
				DistributorID: order.DistributorID,
			})
			return nil, ErrOAuthRequired
		}

		expiresAt := pgtype.Timestamptz{}
		expiresAt.Scan(time.Now().Add(time.Duration(newToks.ExpiresIn) * time.Second)) //nolint:errcheck

		_, _ = h.Queries.UpsertOAuthToken(ctx, dbgen.UpsertOAuthTokenParams{
			TeamID:        order.TeamID,
			DistributorID: order.DistributorID,
			AccessToken:   newToks.AccessToken,
			RefreshToken:  newToks.RefreshToken,
			ExpiresAt:     expiresAt,
			AccountID:     tok.AccountID, // preserve account_id across token refreshes
		})
		accessToken = newToks.AccessToken
	}

	// Get base 2-legged creds and inject 3-legged token.
	cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        order.TeamID,
		DistributorID: order.DistributorID,
	})
	if err != nil {
		return nil, fmt.Errorf("get distributor config: %w", err)
	}

	var credMap map[string]any
	if err := json.Unmarshal(cfg.ApiCredentials, &credMap); err != nil {
		return nil, fmt.Errorf("unmarshal credentials: %w", err)
	}
	credMap["_three_legged_token"] = accessToken
	if tok.AccountID != "" {
		credMap["account_id"] = tok.AccountID
	}
	return json.Marshal(credMap)
}

// ptrTo returns a pointer to v.
func ptrTo[T any](v T) *T { return &v }
