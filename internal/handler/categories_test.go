package handler_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// batteryProductsCategoryID is a seeded, non-parametric category (migration
// 000015) used as a scratch row for the admin category-params test. It is reset
// in cleanup because categories are reference data, not truncated between tests.
const batteryProductsCategoryID = 1

func TestAdminUpdateCategoryParams(t *testing.T) {
	t.Cleanup(func() {
		testDB.Exec(context.Background(), //nolint:errcheck
			"UPDATE categories SET value_param = NULL, primary_params = '[]', generic_code = NULL WHERE id = $1", batteryProductsCategoryID)
		testutil.Truncate(t, testDB)
	})

	adminID := testutil.CreateAdminUser(t, testDB, "catparam-admin@example.com", "pw", "Admin")
	adminToken := testutil.CreateSession(t, testDB, adminID)

	// Configure the (currently empty) category.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/admin/categories/1/params",
		map[string]any{"value_param": "voltage_rating", "primary_params": []string{"voltage_rating", "tolerance"}},
		adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %v", status, body)
	}
	if body["value_param"] != "voltage_rating" {
		t.Errorf("expected value_param=voltage_rating, got %v", body["value_param"])
	}

	// It is reflected on the public categories list (which now exposes value_param).
	cat := getCategory(t, adminToken, batteryProductsCategoryID)
	if cat["value_param"] != "voltage_rating" {
		t.Errorf("list: expected value_param=voltage_rating, got %v", cat["value_param"])
	}
	pp, _ := cat["primary_params"].([]any)
	if len(pp) != 2 || pp[0] != "voltage_rating" || pp[1] != "tolerance" {
		t.Errorf("list: expected [voltage_rating tolerance], got %v", cat["primary_params"])
	}

	// A generic_code is accepted alongside a value_param, and normalised
	// (trimmed + uppercased).
	body, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/admin/categories/1/params",
		map[string]any{"value_param": "voltage_rating", "primary_params": []string{"voltage_rating"}, "generic_code": " bat "},
		adminToken)
	if status != http.StatusOK {
		t.Fatalf("generic_code: expected 200, got %d: %v", status, body)
	}
	if body["generic_code"] != "BAT" {
		t.Errorf("expected generic_code=BAT (normalised), got %v", body["generic_code"])
	}
	cat = getCategory(t, adminToken, batteryProductsCategoryID)
	if cat["generic_code"] != "BAT" {
		t.Errorf("list: expected generic_code=BAT, got %v", cat["generic_code"])
	}

	// A generic_code without a value_param is rejected — generic synthesis needs
	// the magnitude slug.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/admin/categories/1/params",
		map[string]any{"value_param": "", "primary_params": []string{}, "generic_code": "BAT"},
		adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("generic_code without value_param: expected 400, got %d: %v", status, body)
	}

	// An over-long generic_code is rejected.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/admin/categories/1/params",
		map[string]any{"value_param": "voltage_rating", "primary_params": []string{"voltage_rating"}, "generic_code": "WAYTOOLONG"},
		adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("over-long generic_code: expected 400, got %d", status)
	}

	// An unknown slug is rejected.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/admin/categories/1/params",
		map[string]any{"value_param": nil, "primary_params": []string{"not_a_real_slug"}},
		adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("unknown slug: expected 400, got %d: %v", status, body)
	}

	// Empty value_param clears it to null.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/admin/categories/1/params",
		map[string]any{"value_param": "", "primary_params": []string{}},
		adminToken)
	if status != http.StatusOK {
		t.Fatalf("clear: expected 200, got %d: %v", status, body)
	}
	if body["value_param"] != nil {
		t.Errorf("clear: expected value_param=null, got %v", body["value_param"])
	}
	if body["generic_code"] != nil {
		t.Errorf("clear: expected generic_code=null, got %v", body["generic_code"])
	}

	// Non-admins cannot reach the endpoint.
	memberID := testutil.CreateUser(t, testDB, "catparam-member@example.com", "pw", "Member")
	memberToken := testutil.CreateSession(t, testDB, memberID)
	_, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		"/api/v1/admin/categories/1/params",
		map[string]any{"value_param": nil, "primary_params": []string{}},
		memberToken)
	if status != http.StatusForbidden {
		t.Fatalf("non-admin: expected 403, got %d", status)
	}
}

func getCategory(t *testing.T, token string, id int) map[string]any {
	t.Helper()
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/categories", nil, token)
	if status != http.StatusOK {
		t.Fatalf("list categories: expected 200, got %d", status)
	}
	cats, _ := body["categories"].([]any)
	for _, c := range cats {
		m := c.(map[string]any)
		if int(m["id"].(float64)) == id {
			return m
		}
	}
	t.Fatalf("category %d not found", id)
	return nil
}
