// Package mouser implements a distributor.Client for the Mouser Electronics Search API v1.
// Authentication uses a simple API key passed as a query parameter — no OAuth required.
package mouser

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

const apiBase = "https://api.mouser.com"

// Credentials is the JSON structure stored in distributor_configs.api_credentials for Mouser.
type Credentials struct {
	APIKey string `json:"api_key"`
}

// cacheEntry holds a cached mouserPart with an expiry time.
type cacheEntry struct {
	part      mouserPart
	expiresAt time.Time
}

// Client is the Mouser distributor implementation.
type Client struct {
	httpClient  *http.Client
	searchCache sync.Map // key: apiKey+"/"+MouserPartNumber → cacheEntry (1h TTL)
}

// New returns a new Mouser Client ready for use.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// CredentialSpec describes the fields required to configure a Mouser API integration.
func (c *Client) CredentialSpec() []distributor.CredentialField {
	return []distributor.CredentialField{
		{
			Key:         "api_key",
			Label:       "API Key",
			Required:    true,
			Secret:      true,
			Description: "Mouser Search API key from the Mouser API management portal (https://www.mouser.com/api-hub/)",
		},
	}
}

// doPost performs an authenticated POST request, marshalling body to JSON and
// decoding the response into out. Returns an error on non-200 status or decode failure.
func (c *Client) doPost(ctx context.Context, path, apiKey string, body, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("mouser: encode request body: %w", err)
	}

	url := apiBase + path + "?apiKey=" + apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("mouser: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mouser: request: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mouser: status %d: %s", resp.StatusCode, respBody)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("mouser: decode response: %w", err)
	}
	return nil
}

// doGet performs an authenticated GET request, decoding the response into out.
// Returns an error on non-200 status or decode failure.
func (c *Client) doGet(ctx context.Context, path, apiKey string, out any) error {
	url := apiBase + path + "?apiKey=" + apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("mouser: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mouser: request: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mouser: status %d: %s", resp.StatusCode, respBody)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("mouser: decode response: %w", err)
	}
	return nil
}

// parseCredentials unmarshals rawCreds into Credentials, erroring if APIKey is empty.
func parseCredentials(rawCreds json.RawMessage) (Credentials, error) {
	if rawCreds == nil {
		return Credentials{}, fmt.Errorf("mouser: credentials required")
	}
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return Credentials{}, fmt.Errorf("mouser: unmarshal credentials: %w", err)
	}
	if creds.APIKey == "" {
		return Credentials{}, fmt.Errorf("mouser: api_key is required")
	}
	return creds, nil
}

// checkErrors returns an error if the Mouser API reported application-level errors.
func checkErrors(errs []errorEntity) error {
	if len(errs) == 0 {
		return nil
	}
	msg := errs[0].Message
	if msg == "" {
		msg = errs[0].Code
	}
	return fmt.Errorf("mouser: API error: %s", msg)
}
