package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Do sends a JSON request to srv, optionally authenticated with a Bearer token,
// and returns the decoded JSON body and HTTP status code.
// Pass token="" for unauthenticated requests.
func Do(t *testing.T, srv *httptest.Server, method, path string, body any, token string) (map[string]any, int) {
	t.Helper()
	resp := DoRaw(t, srv, method, path, body, token)
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if len(data) > 0 {
		json.Unmarshal(data, &result) //nolint:errcheck
	}
	return result, resp.StatusCode
}

// DoRaw sends a JSON request and returns the raw *http.Response.
// The caller is responsible for closing the response body.
func DoRaw(t *testing.T, srv *httptest.Server, method, path string, body any, token string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("testutil: DoRaw marshal: %v", err)
		}
		r = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, srv.URL+path, r)
	if err != nil {
		t.Fatalf("testutil: DoRaw new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("testutil: DoRaw execute: %v", err)
	}
	return resp
}
