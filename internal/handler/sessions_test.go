package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListSessions(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sessions@example.com", "pw", "Sessions")
	token := testutil.CreateSession(t, testDB, userID)
	// Create a second session so the list has >0 items.
	testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/sessions", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	total, _ := body["total"].(float64)
	if total < 2 {
		t.Errorf("expected at least 2 sessions, got %v", total)
	}
}

func TestListSessions_Unauthenticated(t *testing.T) {
	_, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/sessions", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestRevokeSession_OwnSession(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "revoke@example.com", "pw", "Revoke")
	token := testutil.CreateSession(t, testDB, userID)

	// List sessions to get the session ID.
	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/sessions", nil, token)
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected at least one session")
	}
	sessionID := items[0].(map[string]any)["id"].(string)

	// Revoke the session by ID (using a second token so we can still auth).
	token2 := testutil.CreateSession(t, testDB, userID)
	_, status := testutil.Do(t, testEnv.Server,
		http.MethodDelete, fmt.Sprintf("/api/v1/sessions/%s", sessionID), nil, token2)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// The revoked token must now be invalid.
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, token)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 after session revocation, got %d", status)
	}
}

func TestRevokeSession_AnotherUsersSession(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	user1ID := testutil.CreateUser(t, testDB, "sess-u1@example.com", "pw", "U1")
	user2ID := testutil.CreateUser(t, testDB, "sess-u2@example.com", "pw", "U2")
	token1 := testutil.CreateSession(t, testDB, user1ID)
	token2 := testutil.CreateSession(t, testDB, user2ID)

	// Get user2's session ID.
	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/sessions", nil, token2)
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected session for user2")
	}
	sess2ID := items[0].(map[string]any)["id"].(string)

	// User1 tries to revoke user2's session — must get 404.
	_, status := testutil.Do(t, testEnv.Server,
		http.MethodDelete, fmt.Sprintf("/api/v1/sessions/%s", sess2ID), nil, token1)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}

	// User2's session must still be valid.
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, token2)
	if status != http.StatusOK {
		t.Fatalf("user2 should still be authenticated, got %d", status)
	}
}
