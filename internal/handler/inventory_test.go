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

func TestBulkAssignStorage(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "inv-bulk-assign@example.com", "pw", "Inv")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Inv Team")
	token := testutil.CreateSession(t, testDB, userID)
	partA := testutil.CreatePart(t, testDB, "global", "INV-BULK-A", nil)
	partB := testutil.CreatePart(t, testDB, "global", "INV-BULK-B", nil)
	entryA := testutil.CreateInventoryEntry(t, testDB, teamID, partA)

	locBody, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID),
		map[string]any{"label": "Drawer 9"}, token)
	if status != http.StatusCreated {
		t.Fatalf("create location: expected 201, got %d: %v", status, locBody)
	}
	locID := locBody["id"].(string)

	// partA is tracked (existing entry moves), partB is not (zero-qty entry created).
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/bulk-assign-storage", teamID),
		map[string]any{"part_ids": []string{partA, partB}, "storage_location_id": locID}, token)
	if status != http.StatusOK {
		t.Fatalf("bulk assign: expected 200, got %d: %v", status, body)
	}
	if n := body["assigned"].(float64); n != 2 {
		t.Errorf("expected assigned=2, got %v", n)
	}
	if n := body["created"].(float64); n != 1 {
		t.Errorf("expected created=1, got %v", n)
	}

	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s", teamID, entryA), nil, token)
	if got, _ := body["storage_location_id"].(string); got != locID {
		t.Errorf("entry A: expected storage_location_id=%s, got %v", locID, body["storage_location_id"])
	}

	// The created entry for partB exists with zero stock and the location.
	var qty int32
	var gotLoc string
	err := testDB.QueryRow(t.Context(),
		`SELECT quantity_on_hand, storage_location_id FROM inventory_entries WHERE team_id = $1 AND part_id = $2`,
		teamID, partB).Scan(&qty, &gotLoc)
	if err != nil {
		t.Fatalf("expected inventory entry for part B: %v", err)
	}
	if qty != 0 {
		t.Errorf("created entry: expected quantity_on_hand=0, got %d", qty)
	}
	if gotLoc != locID {
		t.Errorf("created entry: expected location %s, got %s", locID, gotLoc)
	}

	// Guards: empty batch, foreign location, foreign team-scoped part.
	if _, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/bulk-assign-storage", teamID),
		map[string]any{"part_ids": []string{}, "storage_location_id": locID}, token); status != http.StatusBadRequest {
		t.Errorf("empty part_ids: expected 400, got %d", status)
	}

	otherOwner := testutil.CreateUser(t, testDB, "inv-bulk-other@example.com", "pw", "O")
	otherTeam := testutil.CreateTeam(t, testDB, otherOwner, "member", "Other Team")
	foreignPart := testutil.CreatePart(t, testDB, "team", "INV-BULK-FOREIGN", &otherTeam)
	if _, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/bulk-assign-storage", teamID),
		map[string]any{"part_ids": []string{foreignPart}, "storage_location_id": locID}, token); status != http.StatusForbidden {
		t.Errorf("foreign part: expected 403, got %d", status)
	}

	otherToken := testutil.CreateSession(t, testDB, otherOwner)
	otherLoc, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", otherTeam),
		map[string]any{"label": "Foreign Bin"}, otherToken)
	if _, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/bulk-assign-storage", teamID),
		map[string]any{"part_ids": []string{partA}, "storage_location_id": otherLoc["id"].(string)}, token); status != http.StatusNotFound {
		t.Errorf("foreign location: expected 404, got %d", status)
	}
}
