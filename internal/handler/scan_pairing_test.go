package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// TestScanPairingFlow verifies the ticket-based pairing handshake: the desktop
// gets a pairing ticket (not a token), the phone exchanges it once for a durable
// token, and the ticket cannot be reused.
func TestScanPairingFlow(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "pair@example.com", "pw", "Pair")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Pair Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Desktop creates a scan session → pairing ticket, no token.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/scan-sessions", teamID), map[string]any{}, token)
	if status != http.StatusCreated {
		t.Fatalf("create scan session: expected 201, got %d: %v", status, body)
	}
	if body["token"] != nil {
		t.Error("create scan session must not return a durable token")
	}
	ticket, _ := body["pairing_ticket"].(string)
	if ticket == "" {
		t.Fatal("expected a pairing_ticket")
	}

	// Phone redeems the pairing ticket (unauthenticated) → durable token.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/pair", map[string]any{"ticket": ticket}, "")
	if status != http.StatusOK {
		t.Fatalf("pair: expected 200, got %d: %v", status, body)
	}
	scanToken, _ := body["token"].(string)
	if scanToken == "" {
		t.Fatal("pair did not return a token")
	}

	// The durable token works as a Bearer scan token.
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/scan/session", nil, scanToken)
	if status != http.StatusOK {
		t.Errorf("scan token should authenticate /scan/session: got %d", status)
	}

	// The pairing ticket is single-use.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/pair", map[string]any{"ticket": ticket}, "")
	if status != http.StatusUnauthorized {
		t.Errorf("reused pairing ticket: expected 401, got %d", status)
	}

	// The phone can mint a single-use stream ticket with its Bearer token.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/scan/stream-ticket", nil, scanToken)
	if status != http.StatusOK {
		t.Fatalf("stream-ticket: expected 200, got %d: %v", status, body)
	}
	if body["ticket"] == nil || body["ticket"] == "" {
		t.Error("expected a stream ticket")
	}

	// A bogus stream ticket on the SSE endpoint is rejected with the distinct code.
	resp := testutil.DoRaw(t, testEnv.Server, http.MethodGet, "/api/v1/scan/stream?ticket=st_bogus", nil, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bogus stream ticket: expected 401, got %d", resp.StatusCode)
	}
}
