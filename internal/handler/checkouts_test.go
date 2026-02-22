package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListCheckouts_Empty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "co-list@example.com", "pw", "CO")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "CO Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/checkouts", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestCreateCheckout_WithInventory(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "co-inv@example.com", "pw", "CO")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "CO Team")
	partID := testutil.CreatePart(t, testDB, "global", "CO-PART-01", nil)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 10)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/checkouts", teamID),
		map[string]any{
			"reason": "Lab use",
			"items":  []any{map[string]any{"part_id": partID, "quantity": 3}},
		}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if !item["adjusted"].(bool) {
		t.Error("expected adjusted=true when inventory entry exists")
	}

	// Verify stock deduction.
	var qty int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 7 {
		t.Errorf("expected stock=7 after checkout of 3, got %d", qty)
	}
}

func TestCreateCheckout_WithoutInventory(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "co-noinv@example.com", "pw", "CO")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "CO Team")
	// Part has no inventory entry in this team.
	partID := testutil.CreatePart(t, testDB, "global", "CO-NOINV-01", nil)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/checkouts", teamID),
		map[string]any{
			"items": []any{map[string]any{"part_id": partID, "quantity": 5}},
		}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]any)
	if item["adjusted"].(bool) {
		t.Error("expected adjusted=false when no inventory entry exists")
	}
}

func TestCreateCheckout_EmptyItems(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "co-empty@example.com", "pw", "CO")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "CO Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/checkouts", teamID),
		map[string]any{"items": []any{}}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestGetCheckout(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "co-get@example.com", "pw", "CO")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "CO Team")
	partID := testutil.CreatePart(t, testDB, "global", "CO-GET-01", nil)
	token := testutil.CreateSession(t, testDB, userID)

	// Create checkout via HTTP.
	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/checkouts", teamID),
		map[string]any{
			"reason": "Testing",
			"items":  []any{map[string]any{"part_id": partID, "quantity": 2}},
		}, token)
	if status != http.StatusCreated {
		t.Fatalf("create expected 201, got %d: %v", status, created)
	}
	checkoutID := created["id"].(string)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/checkouts/%s", teamID, checkoutID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["id"] != checkoutID {
		t.Errorf("expected id=%s, got %v", checkoutID, body["id"])
	}
	if body["reason"] != "Testing" {
		t.Errorf("expected reason=Testing, got %v", body["reason"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}
