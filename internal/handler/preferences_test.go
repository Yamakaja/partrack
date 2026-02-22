package handler_test

import (
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestGetPreferences_DefaultEmpty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "prefs-empty@example.com", "pw", "Prefs")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/preferences", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	prefs, _ := body["preferences"].(map[string]any)
	if len(prefs) != 0 {
		t.Errorf("expected empty preferences, got %v", prefs)
	}
}

func TestPutAndGetPreferences(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "prefs-put@example.com", "pw", "Prefs")
	token := testutil.CreateSession(t, testDB, userID)

	// Save preferences.
	saved, status := testutil.Do(t, testEnv.Server, http.MethodPut, "/api/v1/preferences",
		map[string]any{"theme": "dark", "density": "compact"}, token)
	if status != http.StatusOK {
		t.Fatalf("PUT expected 200, got %d: %v", status, saved)
	}
	if prefs, _ := saved["preferences"].(map[string]any); prefs["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %v", prefs)
	}

	// Retrieve and confirm round-trip.
	got, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/preferences", nil, token)
	if status != http.StatusOK {
		t.Fatalf("GET expected 200, got %d", status)
	}
	if prefs, _ := got["preferences"].(map[string]any); prefs["theme"] != "dark" {
		t.Errorf("expected theme=dark after GET, got %v", prefs)
	}
}

func TestPutPreferences_InvalidBody(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "prefs-bad@example.com", "pw", "Prefs")
	token := testutil.CreateSession(t, testDB, userID)

	// Send a JSON array instead of an object.
	_, status := testutil.Do(t, testEnv.Server, http.MethodPut, "/api/v1/preferences",
		[]any{"not", "an", "object"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}
