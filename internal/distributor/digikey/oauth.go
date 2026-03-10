package digikey

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// ---- token management ----

// getToken returns a valid bearer token, fetching a new one if the cached one expires soon.
// If a ThreeLeggedToken is set in creds (injected by handler), it is returned directly.
func (c *Client) getToken(ctx context.Context, creds Credentials) (string, error) {
	if creds.ThreeLeggedToken != "" {
		return creds.ThreeLeggedToken, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	tok := c.tokens[creds.ClientID]
	if tok != nil && time.Now().Add(60*time.Second).Before(tok.expiry) {
		return tok.accessToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", creds.ClientID)
	form.Set("client_secret", creds.ClientSecret)

	tokenURLStr := tokenURL(creds)
	if c.debug {
		log.Printf("digikey [token] POST %s  client_id=%s sandbox=%v", tokenURLStr, creds.ClientID, creds.Sandbox)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURLStr, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("digikey: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("digikey: token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if c.debug {
		log.Printf("digikey [token] response status=%d body=%s", resp.StatusCode, body)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("digikey: token endpoint %d: %s", resp.StatusCode, body)
	}

	var tokResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return "", fmt.Errorf("digikey: decode token response: %w", err)
	}

	c.tokens[creds.ClientID] = &cachedToken{
		accessToken: tokResp.AccessToken,
		expiry:      time.Now().Add(time.Duration(tokResp.ExpiresIn) * time.Second),
	}
	return tokResp.AccessToken, nil
}

func (c *Client) invalidateToken(clientID string) {
	c.mu.Lock()
	delete(c.tokens, clientID)
	c.mu.Unlock()
}

// ---- ThreeLeggedAuthClient implementation ----

// AuthorizeURL builds the DigiKey OAuth2 authorization redirect URL.
func (c *Client) AuthorizeURL(rawCreds json.RawMessage, redirectURI, state string) (string, error) {
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return "", fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}
	scope := orDefault(creds.Scope, "openid")
	authURL := baseURL(creds) + "/v1/oauth2/authorize"
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {creds.ClientID},
		"redirect_uri":  {redirectURI},
		"scope":         {scope},
		"state":         {state},
	}
	return authURL + "?" + params.Encode(), nil
}

// ExchangeCode exchanges an authorization code for access + refresh tokens.
func (c *Client) ExchangeCode(ctx context.Context, rawCreds json.RawMessage, code, redirectURI string) (*distributor.OAuthTokens, error) {
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}
	return c.postTokenForm(ctx, creds, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
	})
}

// refreshToken exchanges a refresh token for a new access token.
func (c *Client) refreshToken(ctx context.Context, rawCreds json.RawMessage, refreshTok string) (*distributor.OAuthTokens, error) {
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}
	return c.postTokenForm(ctx, creds, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshTok},
		"client_id":     {creds.ClientID},
		"client_secret": {creds.ClientSecret},
	})
}

// postTokenForm POSTs form-encoded values to the DigiKey token endpoint and returns tokens.
func (c *Client) postTokenForm(ctx context.Context, creds Credentials, form url.Values) (*distributor.OAuthTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL(creds), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("digikey: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("digikey: token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("digikey: token endpoint %d: %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("digikey: decode token response: %w", err)
	}
	return &distributor.OAuthTokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
	}, nil
}

// ---- CredentialEnricher implementation ----

// EnrichCredentials implements distributor.CredentialEnricher.
// It fetches a stored 3-legged OAuth token, refreshes it if needed,
// lazily resolves the account ID on first use, and injects both into
// the base 2-legged credentials JSON.
func (c *Client) EnrichCredentials(ctx context.Context, base json.RawMessage, provider distributor.CredentialProvider) (json.RawMessage, error) {
	tok, err := provider.GetOAuthToken(ctx)
	if err != nil || tok == nil {
		return nil, distributor.ErrOAuthRequired
	}

	accessToken := tok.AccessToken

	// Refresh if expiring within 60 seconds.
	if time.Now().Add(60 * time.Second).After(tok.ExpiresAt) {
		newToks, err := c.refreshToken(ctx, base, tok.RefreshToken)
		if err != nil {
			_ = provider.DeleteOAuthToken(ctx)
			return nil, distributor.ErrOAuthRequired
		}
		tok = &distributor.StoredToken{
			AccessToken:  newToks.AccessToken,
			RefreshToken: newToks.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(newToks.ExpiresIn) * time.Second),
			AccountID:    tok.AccountID, // preserved across refresh
		}
		_ = provider.StoreOAuthToken(ctx, tok)
		accessToken = tok.AccessToken
	}

	// Lazy account ID resolution: if not yet stored, try to fetch it.
	if tok.AccountID == "" {
		if id, err := c.getPrimaryAccountID(ctx, base, accessToken); err == nil && id != "" {
			updated := *tok
			updated.AccountID = id
			_ = provider.StoreOAuthToken(ctx, &updated) // best-effort
			tok = &updated
		}
		// Non-fatal: proceed without account ID (e.g. sandbox environment)
	}

	// Inject DigiKey-specific fields into the base 2-legged credentials.
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	m["_three_legged_token"] = accessToken
	if tok.AccountID != "" {
		m["account_id"] = tok.AccountID
	}
	return json.Marshal(m)
}

// getPrimaryAccountID calls GET /CustomerResource/v1/associatedaccounts using the
// provided 3-legged access token and returns the first AccountId found.
func (c *Client) getPrimaryAccountID(ctx context.Context, rawCreds json.RawMessage, accessToken string) (string, error) {
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return "", fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}

	endpoint := baseURL(creds) + "/CustomerResource/v1/associatedaccounts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("digikey: build associatedaccounts request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-DIGIKEY-Client-Id", creds.ClientID)

	if c.debug {
		log.Printf("digikey [request] GET %s", endpoint)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("digikey: associatedaccounts request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if c.debug {
		log.Printf("digikey [response] status=%d body=%s", resp.StatusCode, body)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("digikey: associatedaccounts %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Accounts []struct {
			AccountId int32 `json:"AccountId"`
		} `json:"Accounts"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("digikey: decode associatedaccounts response: %w", err)
	}
	if len(result.Accounts) == 0 {
		return "", fmt.Errorf("digikey: no accounts associated with this OAuth token")
	}
	return fmt.Sprintf("%d", result.Accounts[0].AccountId), nil
}
