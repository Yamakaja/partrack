package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

// APIError is a structured error response from the server.
type APIError struct {
	Status  int
	Message string `json:"error"`
	Code    string `json:"code"`
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Code != "INTERNAL_ERROR" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Code)
	}
	return e.Message
}

// exitCode maps an HTTP status onto the process exit taxonomy.
func (e *APIError) exitCode() int {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ExitAuth
	case http.StatusNotFound:
		return ExitNotFound
	case http.StatusConflict:
		return ExitAmbiguous
	default:
		return ExitFailure
	}
}

// Client talks to a partrack server. It authenticates with a bearer token for
// ordinary use; the login flow drives it with a session cookie instead, which is
// why it keeps a cookie jar.
type Client struct {
	BaseURL string
	Token   string
	Debug   bool

	http *http.Client
}

// NewClient builds a client for the given base URL (the site root, not the API
// prefix). It fails when no URL is configured, since every command needs one.
func NewClient(cfg *Config, debug bool) (*Client, error) {
	if cfg.URL == "" {
		return nil, exitErrf(ExitUsage,
			"no server configured — run `pt auth login --url https://partrack.example.com` or set PARTRACK_URL")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, exitErr(ExitFailure, err)
	}
	return &Client{
		BaseURL: cfg.URL,
		Token:   cfg.Token,
		Debug:   debug,
		http: &http.Client{
			Timeout: 60 * time.Second,
			Jar:     jar,
		},
	}, nil
}

// requireToken returns a usage error when the client has no credentials, so
// commands fail with "run pt auth login" rather than a bare 401.
func (c *Client) requireToken() error {
	if c.Token == "" {
		return exitErrf(ExitAuth, "not authenticated — run `pt auth login`")
	}
	return nil
}

// Get performs a GET against /api/v1<path> and decodes the JSON body into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// Post performs a POST with a JSON body.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

// Patch performs a PATCH with a JSON body.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPatch, path, nil, body, out)
}

// Put performs a PUT with a JSON body.
func (c *Client) Put(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPut, path, nil, body, out)
}

// Delete performs a DELETE.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	endpoint := c.BaseURL + "/api/v1" + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return exitErr(ExitFailure, err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return exitErr(ExitUsage, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	if c.Debug {
		fmt.Fprintf(os.Stderr, "pt: %s %s\n", method, endpoint)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return exitErrf(ExitNetwork, "%s %s: %v", method, redactURL(endpoint), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if c.Debug {
		fmt.Fprintf(os.Stderr, "pt: → %s\n", resp.Status)
	}

	if resp.StatusCode >= 400 {
		return c.apiError(resp, method, path)
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return exitErrf(ExitFailure, "decoding %s response: %v", path, err)
	}
	return nil
}

// apiError converts a >=400 response into an ExitError carrying the server's
// message and the matching exit code. When the server sends no JSON body — a
// bare 404, a proxy error page — the status alone ("not found") tells the user
// nothing, so the request that produced it is named instead.
func (c *Client) apiError(resp *http.Response, method, path string) error {
	apiErr := &APIError{Status: resp.StatusCode}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if len(data) > 0 {
		_ = json.Unmarshal(data, apiErr)
	}
	if apiErr.Message == "" {
		status := strings.ToLower(http.StatusText(resp.StatusCode))
		if status == "" {
			status = fmt.Sprintf("http %d", resp.StatusCode)
		}
		apiErr.Message = fmt.Sprintf("%s: %s %s", status, method, path)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		apiErr.Message += " — run `pt auth login`"
	}
	return exitErr(apiErr.exitCode(), apiErr)
}

// redactURL strips the query string from a URL for error messages, since
// filters can carry data the user would not expect to see echoed.
func redactURL(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i]
	}
	return s
}

// postForSession performs the login POST, keeping the resulting session cookie
// in the jar. It bypasses the bearer header deliberately: the CSRF middleware
// exempts Bearer requests, and we want cookie auth here so `POST /tokens` can
// mint a token for this session.
func (c *Client) postForSession(ctx context.Context, path string, body, out any) error {
	saved := c.Token
	c.Token = ""
	defer func() { c.Token = saved }()
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

// hasSession reports whether the jar holds a session cookie for the base URL.
func (c *Client) hasSession() bool {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return false
	}
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "session" {
			return true
		}
	}
	return false
}
