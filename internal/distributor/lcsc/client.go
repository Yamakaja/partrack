// Package lcsc implements distributor.Client for the LCSC/JLCPCB component search API.
// The underlying endpoint is JLCPCB's internal parts-browser API — public and
// unauthenticated. No API key or OAuth is required.
package lcsc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

const (
	apiBase = "https://jlcpcb.com"
	apiPath = "/api/overseas-pcb-order/v1/shoppingCart/smtGood/selectSmtComponentList"
)

// cacheEntry holds a cached component with an expiry time.
type cacheEntry struct {
	comp      component
	expiresAt time.Time
}

// Client is the LCSC/JLCPCB distributor implementation.
type Client struct {
	httpClient  *http.Client
	searchCache sync.Map // key: componentCode → cacheEntry (1h TTL)
}

// New returns a new LCSC Client ready for use.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// CredentialSpec returns nil — LCSC requires no credentials.
func (c *Client) CredentialSpec() []distributor.CredentialField {
	return nil
}

// doPost marshals req to JSON, sends a POST to the JLCPCB parts API with the
// required browser-like headers, and returns the parsed searchData on success.
// Returns an error if the HTTP status is not 200 or if the API response code is not 200.
func (c *Client) doPost(ctx context.Context, req searchRequest) (*searchData, error) {
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("lcsc: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+apiPath, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("lcsc: build request: %w", err)
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Origin", "https://jlcpcb.com")
	httpReq.Header.Set("Referer", "https://jlcpcb.com/parts")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("lcsc: request: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lcsc: HTTP %d: %s", resp.StatusCode, body)
	}

	var envelope searchResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("lcsc: decode response: %w", err)
	}
	if envelope.Code != 200 {
		msg := ""
		if envelope.Message != nil {
			msg = *envelope.Message
		}
		return nil, fmt.Errorf("lcsc: API error code %d: %s", envelope.Code, msg)
	}

	return &envelope.Data, nil
}
