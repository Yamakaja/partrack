// Package digikey implements a distributor.Client for the DigiKey ProductSearch v4 API.
// Authentication uses the OAuth2 client credentials flow (2-legged); tokens are cached
// in memory per client_id and proactively refreshed before expiry.
package digikey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// Credentials is the JSON structure stored in distributor_configs.api_credentials for DigiKey.
type Credentials struct {
	ClientID         string `json:"client_id"`
	ClientSecret     string `json:"client_secret"`
	LocaleSite       string `json:"locale_site"`                   // default "US"
	LocaleCurrency   string `json:"locale_currency"`               // default "USD"
	LocaleLanguage   string `json:"locale_language"`               // default "en"
	AccountID        string `json:"account_id"`                    // optional; required for quote/MyList operations
	Scope            string `json:"scope"`                         // 3-legged OAuth scope; default "openid"
	Sandbox          bool   `json:"sandbox"`                       // true → sandbox-api.digikey.com
	ThreeLeggedToken string `json:"_three_legged_token,omitempty"` // injected by handler; never stored in DB
}

// Client is the DigiKey distributor implementation.
type Client struct {
	mu          sync.Mutex
	tokens      map[string]*cachedToken    // keyed by client_id
	rateLimits  map[string]*rateLimitEntry // keyed by client_id
	httpClient  *http.Client
	debug       bool
	searchCache sync.Map // key: clientID+"/"+sku → searchCacheEntry
}

type rateLimitEntry struct {
	limit     int
	remaining int
	updatedAt time.Time
}

type searchCacheEntry struct {
	product   product
	expiresAt time.Time
}

type cachedToken struct {
	accessToken string
	expiry      time.Time
}

// New returns a new DigiKey Client ready for use.
func New() *Client {
	return &Client{
		tokens:     make(map[string]*cachedToken),
		rateLimits: make(map[string]*rateLimitEntry),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetDebug enables or disables verbose request/response logging for all DigiKey API calls.
// When enabled, each call logs the method, URL, request headers, request body,
// response status, and response body. Intended for debugging credential and API issues only.
func (c *Client) SetDebug(enabled bool) {
	c.debug = enabled
}

// ---- host helpers ----

func baseURL(creds Credentials) string {
	if creds.Sandbox {
		return "https://sandbox-api.digikey.com"
	}
	return "https://api.digikey.com"
}

func tokenURL(creds Credentials) string {
	return baseURL(creds) + "/v1/oauth2/token"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ---- HTTP execution ----

// setHeaders applies the standard DigiKey auth and locale headers to a request.
func setHeaders(req *http.Request, creds Credentials, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-DIGIKEY-Client-Id", creds.ClientID)
	req.Header.Set("X-DIGIKEY-Locale-Site", orDefault(creds.LocaleSite, "US"))
	req.Header.Set("X-DIGIKEY-Locale-Currency", orDefault(creds.LocaleCurrency, "USD"))
	req.Header.Set("X-DIGIKEY-Locale-Language", orDefault(creds.LocaleLanguage, "en"))
	if creds.AccountID != "" {
		req.Header.Set("X-DIGIKEY-Account-Id", creds.AccountID)
	}
}

// doRequest performs an authenticated HTTP request, decoding a JSON response into out.
// For POST requests body is marshalled to JSON and Content-Type is set; for GET pass body=nil.
// Returns (true, nil) on 200, (false, nil) on 404, (false, err) otherwise.
// Retries once on 401 (token refresh) and once on 429 (Retry-After backoff).
func (c *Client) doRequest(ctx context.Context, method, rawURL string, body any, creds Credentials, out any) (bool, error) {
	token, err := c.getToken(ctx, creds)
	if err != nil {
		return false, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		var bodyReader io.Reader
		var encodedBody []byte
		if body != nil {
			var err error
			encodedBody, err = json.Marshal(body)
			if err != nil {
				return false, fmt.Errorf("digikey: encode request body: %w", err)
			}
			bodyReader = bytes.NewReader(encodedBody)
		}

		req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
		if err != nil {
			return false, fmt.Errorf("digikey: build request: %w", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		setHeaders(req, creds, token)

		if c.debug {
			log.Printf("digikey [request] attempt=%d %s %s", attempt+1, method, rawURL)
			for k, vs := range req.Header {
				log.Printf("digikey [request]   %s: %s", k, strings.Join(vs, ", "))
			}
			if encodedBody != nil {
				log.Printf("digikey [request] body=%s", encodedBody)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return false, fmt.Errorf("digikey: request: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.updateRateLimit(creds.ClientID, resp.Header)

		if c.debug {
			log.Printf("digikey [response] status=%d", resp.StatusCode)
			for k, vs := range resp.Header {
				log.Printf("digikey [response] %s: %s", k, vs)
			}
			log.Printf("digikey [response] body=%s", respBody)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if err := json.Unmarshal(respBody, out); err != nil {
				return false, fmt.Errorf("digikey: decode response: %w", err)
			}
			return true, nil

		case http.StatusNotFound:
			return false, fmt.Errorf("digikey: 404 not found: %s", respBody)

		case http.StatusBadRequest:
			return false, fmt.Errorf("digikey: 400 bad request: %s", respBody)

		case http.StatusUnauthorized:
			if attempt == 0 {
				c.invalidateToken(creds.ClientID)
				token, err = c.getToken(ctx, creds)
				if err != nil {
					return false, err
				}
				continue
			}
			return false, fmt.Errorf("digikey: 401 unauthorized after token refresh")

		case http.StatusTooManyRequests:
			if attempt == 0 {
				wait := 5 * time.Second
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, err := strconv.Atoi(ra); err == nil {
						wait = time.Duration(secs) * time.Second
					}
				}
				select {
				case <-ctx.Done():
					return false, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return false, fmt.Errorf("digikey: 429 rate limited")

		default:
			return false, fmt.Errorf("digikey: unexpected status %d: %s", resp.StatusCode, respBody)
		}
	}
	return false, fmt.Errorf("digikey: max retries exceeded")
}

func (c *Client) doGet(ctx context.Context, rawURL string, creds Credentials, out any) (bool, error) {
	return c.doRequest(ctx, http.MethodGet, rawURL, nil, creds, out)
}

func (c *Client) doPostJSON(ctx context.Context, rawURL string, body any, creds Credentials, out any) (bool, error) {
	return c.doRequest(ctx, http.MethodPost, rawURL, body, creds, out)
}

// updateRateLimit parses X-Ratelimit-Limit and X-Ratelimit-Remaining headers and
// stores the values in the per-client-id rate limit map. No-op if headers are absent.
func (c *Client) updateRateLimit(clientID string, h http.Header) {
	limit, errL := strconv.Atoi(h.Get("X-Ratelimit-Limit"))
	remaining, errR := strconv.Atoi(h.Get("X-Ratelimit-Remaining"))
	if errL != nil || errR != nil {
		return
	}
	c.mu.Lock()
	c.rateLimits[clientID] = &rateLimitEntry{limit: limit, remaining: remaining, updatedAt: time.Now()}
	c.mu.Unlock()
}

// RateLimitStatus implements distributor.RateLimitReporter. It returns the last observed
// rate limit headers for the client_id in credentials, or nil if none have been recorded yet.
func (c *Client) RateLimitStatus(credentials json.RawMessage) *distributor.RateLimitStatus {
	var creds Credentials
	if err := json.Unmarshal(credentials, &creds); err != nil || creds.ClientID == "" {
		return nil
	}
	c.mu.Lock()
	entry := c.rateLimits[creds.ClientID]
	c.mu.Unlock()
	if entry == nil {
		return nil
	}
	return &distributor.RateLimitStatus{
		Limit:     entry.limit,
		Remaining: entry.remaining,
		UpdatedAt: entry.updatedAt,
	}
}
