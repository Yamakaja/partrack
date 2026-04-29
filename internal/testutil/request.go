package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

// WaitForBuildSelections polls GET /api/v1/teams/{teamID}/builds/{buildID} until
// selections_checked_at is non-null or the 3-second timeout elapses. Fatal on timeout.
func WaitForBuildSelections(t *testing.T, srv *httptest.Server, teamID, buildID, token string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		body, status := Do(t, srv, http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
		if status == http.StatusOK && body["selections_checked_at"] != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for build substitute selections to be computed")
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
