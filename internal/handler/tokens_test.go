package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListTokens(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "toklist@example.com", "pw", "TokList")
	session := testutil.CreateSession(t, testDB, userID)
	testutil.CreateAPIToken(t, testDB, userID, "my-token")

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/tokens", nil, session)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 token item, got %d", len(items))
	}
	// Token hash must never appear in the response.
	raw := fmt.Sprintf("%v", body)
	if strings.Contains(raw, "token_hash") {
		t.Error("token_hash must not appear in list response")
	}
}

func TestCreateToken(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "tokcreate@example.com", "pw", "TokCreate")
	session := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/tokens",
		map[string]any{"name": "ci-token"}, session)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	rawToken, _ := body["token"].(string)
	if !strings.HasPrefix(rawToken, "ptk_") {
		t.Errorf("expected ptk_ prefix, got %s", rawToken)
	}

	// The returned token must be immediately usable.
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, rawToken)
	if status != http.StatusOK {
		t.Fatalf("expected created token to authenticate; got %d", status)
	}
}

func TestCreateToken_MissingName(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "toknoname@example.com", "pw", "TokNoName")
	session := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/tokens",
		map[string]any{}, session)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestRevokeToken(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "tokrevoke@example.com", "pw", "TokRevoke")
	session := testutil.CreateSession(t, testDB, userID)

	// Create via API so we have the token ID.
	body, _ := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/tokens",
		map[string]any{"name": "revoke-me"}, session)
	rawToken, _ := body["token"].(string)
	tokenID, _ := body["id"].(string)

	// Revoke it.
	_, status := testutil.Do(t, testEnv.Server,
		http.MethodDelete, fmt.Sprintf("/api/v1/tokens/%s", tokenID), nil, session)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// Token must no longer authenticate.
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, rawToken)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 after revocation, got %d", status)
	}
}

func TestRevokeToken_InvalidID(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "tokbadid@example.com", "pw", "TokBadID")
	session := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodDelete, "/api/v1/tokens/not-a-uuid", nil, session)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}
