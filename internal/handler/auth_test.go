package handler_test

import (
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestLogin_ValidCredentials(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	testutil.CreateUser(t, testDB, "alice@example.com", "hunter2", "Alice")

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": "alice@example.com", "password": "hunter2"}, "")

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	user, ok := body["user"].(map[string]any)
	if !ok {
		t.Fatal("expected user object in response")
	}
	if user["email"] != "alice@example.com" {
		t.Errorf("wrong email: %v", user["email"])
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	testutil.CreateUser(t, testDB, "bob@example.com", "correct", "Bob")

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": "bob@example.com", "password": "wrong"}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": "nobody@example.com", "password": "pw"}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestLogin_InactiveUser(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inactive@example.com", "pw", "Inactive")
	// Deactivate the user directly.
	if _, err := testDB.Exec(t.Context(), `UPDATE users SET is_active = FALSE WHERE id = $1`, userID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": "inactive@example.com", "password": "pw"}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"email": "a@b.com"}, "") // no password
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestMe_WithBearerToken(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "me@example.com", "pw", "Me")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	user, ok := body["user"].(map[string]any)
	if !ok {
		t.Fatal("expected user object")
	}
	if user["email"] != "me@example.com" {
		t.Errorf("wrong email: %v", user["email"])
	}
}

func TestMe_WithAPIToken(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "apime@example.com", "pw", "APIMe")
	token := testutil.CreateAPIToken(t, testDB, userID, "test-token")

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	user, _ := body["user"].(map[string]any)
	if user["email"] != "apime@example.com" {
		t.Errorf("wrong email: %v", user["email"])
	}
}

func TestMe_Unauthenticated(t *testing.T) {
	_, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestMe_InvalidToken(t *testing.T) {
	_, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, "bad-token")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestLogout_BearerToken(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "logout@example.com", "pw", "Logout")
	token := testutil.CreateSession(t, testDB, userID)

	// Verify authenticated before logout.
	_, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200 before logout, got %d", status)
	}

	_, status = testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/auth/logout", nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204 on logout, got %d", status)
	}

	// After logout the token must be invalid (cache miss + DB miss).
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, token)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", status)
	}
}
