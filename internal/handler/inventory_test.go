package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestCreateInventoryEntry(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-create@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "INV-PART-01", nil)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["part_id"] != partID {
		t.Errorf("expected part_id=%s, got %v", partID, body["part_id"])
	}
	if body["quantity_on_hand"].(float64) != 0 {
		t.Errorf("expected quantity_on_hand=0, got %v", body["quantity_on_hand"])
	}
}

func TestCreateInventoryEntry_DuplicatePart(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-dup@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "INV-PART-DUP", nil)

	payload := map[string]any{"part_id": partID}
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID), payload, token)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID), payload, token)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate inventory entry, got %d", status)
	}
}

func TestListInventory(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-list@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Create 3 inventory entries.
	for i := range 3 {
		partID := testutil.CreatePart(t, testDB, "global", fmt.Sprintf("LIST-PART-%02d", i), nil)
		testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
			fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
			map[string]any{"part_id": partID}, token)
	}

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory?limit=2&offset=0", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if body["total"].(float64) < 3 {
		t.Errorf("expected total >= 3, got %v", body["total"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit=2, got %d", len(items))
	}
}

func TestListInventory_SearchQuery(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-search@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)

	p1 := testutil.CreatePart(t, testDB, "global", "SEARCH-ME-01", nil)
	p2 := testutil.CreatePart(t, testDB, "global", "UNRELATED-99", nil)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID), map[string]any{"part_id": p1}, token)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID), map[string]any{"part_id": p2}, token)

	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory?q=SEARCH-ME", teamID), nil, token)
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 result for q=SEARCH-ME, got %d", len(items))
	}
}

func TestGetInventoryEntry(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-get@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "GET-PART-01", nil)

	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID}, token)
	entryID, _ := created["id"].(string)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s", teamID, entryID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["id"] != entryID {
		t.Errorf("expected id=%s, got %v", entryID, body["id"])
	}
}

func TestGetInventoryEntry_NotFound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-get404@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory/00000000-0000-0000-0000-000000000000", teamID),
		nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestUpdateInventoryEntry(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-update@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "UPD-PART-01", nil)

	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID}, token)
	entryID, _ := created["id"].(string)

	minStock := int32(10)
	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s", teamID, entryID),
		map[string]any{"minimum_stock_quantity": minStock}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["minimum_stock_quantity"].(float64) != float64(minStock) {
		t.Errorf("expected minimum_stock_quantity=%d, got %v", minStock, body["minimum_stock_quantity"])
	}
}

func TestCreateAdjustment(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-adj@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "ADJ-PART-01", nil)

	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID}, token)
	entryID, _ := created["id"].(string)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryID),
		map[string]any{"delta": 5, "reason": "initial stock"}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["delta"].(float64) != 5 {
		t.Errorf("expected delta=5, got %v", body["delta"])
	}
	if body["quantity_on_hand"].(float64) != 5 {
		t.Errorf("expected quantity_on_hand=5 after +5 adjustment, got %v", body["quantity_on_hand"])
	}
}

func TestCreateAdjustment_ZeroDeltaRejected(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-zeroadj@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "ZEROADJ-PART", nil)

	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID}, token)
	entryID, _ := created["id"].(string)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryID),
		map[string]any{"delta": 0}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for delta=0, got %d", status)
	}
}

func TestListAdjustments(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-listadj@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "LISTADJ-PART", nil)

	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID}, token)
	entryID, _ := created["id"].(string)

	// Create two adjustments.
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryID),
		map[string]any{"delta": 10, "reason": "receipt"}, token)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryID),
		map[string]any{"delta": -3, "reason": "used"}, token)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["total"].(float64) != 2 {
		t.Errorf("expected total=2, got %v", body["total"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 adjustments, got %d", len(items))
	}
}

func TestReplenishPreview(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-replenish@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "LOW-STOCK-PART", nil)

	// Create inventory entry with minimum_stock_quantity=5 and quantity_on_hand=0.
	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID, "minimum_stock_quantity": 5}, token)
	entryID, _ := created["id"].(string)
	_ = entryID

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory/replenish-preview", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) < 1 {
		t.Errorf("expected at least 1 item in replenish preview, got %d", len(items))
	}
	// Verify the shortfall is reported.
	first, _ := items[0].(map[string]any)
	if first["shortfall"].(float64) != 5 {
		t.Errorf("expected shortfall=5, got %v", first["shortfall"])
	}
}

func TestListInventory_BelowMinStock(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-belowmin@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Part A: below min stock (qty=0, min=5).
	pA := testutil.CreatePart(t, testDB, "global", "BELOW-MIN-A", nil)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": pA, "minimum_stock_quantity": 5}, token)

	// Part B: above min stock (qty=10, min=5).
	pB := testutil.CreatePart(t, testDB, "global", "ABOVE-MIN-B", nil)
	entryB, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": pB, "minimum_stock_quantity": 5}, token)
	entryBID, _ := entryB["id"].(string)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryBID),
		map[string]any{"delta": 10, "reason": "setup"}, token)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory?below_min_stock=true", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 below-min-stock item, got %d", len(items))
	}
}
