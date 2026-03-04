package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestGetCart_Empty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-empty@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty cart, got %d items", len(items))
	}
}

func TestAddCartItem(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-add@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	partID := testutil.CreatePart(t, testDB, "global", "CART-PART-01", nil)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 5}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["part_id"] != partID {
		t.Errorf("expected part_id=%s, got %v", partID, body["part_id"])
	}
	if body["quantity"].(float64) != 5 {
		t.Errorf("expected quantity=5, got %v", body["quantity"])
	}
}

func TestAddCartItem_ZeroQuantity(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-zeroqty@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	partID := testutil.CreatePart(t, testDB, "global", "CART-ZERO", nil)
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 0}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestUpdateCartItem_Quantity(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-upd@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	partID := testutil.CreatePart(t, testDB, "global", "CART-UPD-01", nil)
	token := testutil.CreateSession(t, testDB, userID)

	added, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 3}, token)
	itemID := added["id"].(string)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/cart/items/%s", teamID, itemID),
		map[string]any{"quantity": 10}, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
}

func TestRemoveCartItem(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-rm@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	partID := testutil.CreatePart(t, testDB, "global", "CART-RM-01", nil)
	token := testutil.CreateSession(t, testDB, userID)

	added, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 1}, token)
	itemID := added["id"].(string)

	_, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/cart/items/%s", teamID, itemID), nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// Cart should now be empty.
	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty cart after remove, got %d items", len(items))
	}
}

func TestClearCart(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-clear@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Add two items.
	for i, mpn := range []string{"CART-CLR-A", "CART-CLR-B"} {
		p := testutil.CreatePart(t, testDB, "global", mpn, nil)
		testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
			fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
			map[string]any{"part_id": p, "quantity": i + 1}, token)
	}

	_, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty cart after clear, got %d items", len(items))
	}
}

func TestConvertCart_EmptyCart(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-conv-empty@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/convert", teamID), nil, token)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", status)
	}
}

func TestConvertCart_MissingListing(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-conv-nolist@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	partID := testutil.CreatePart(t, testDB, "global", "CART-NOLIST", nil)
	token := testutil.CreateSession(t, testDB, userID)

	// Add item without listing.
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 1}, token)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/convert", teamID), nil, token)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", status)
	}
}

func TestConvertCart(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-conv@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	partID := testutil.CreatePart(t, testDB, "global", "CART-CONV-01", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "CART-CONV-SKU-01")
	token := testutil.CreateSession(t, testDB, userID)

	// Add item with listing.
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 10, "listing_id": listingID}, token)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/convert", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	orderIDs, _ := body["order_ids"].([]any)
	if len(orderIDs) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orderIDs))
	}

	// Cart should now be empty.
	cartBody, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	items, _ := cartBody["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty cart after convert, got %d items", len(items))
	}
}

// TestBuildDrivenShortfall tests that submitting a build for fulfillment drives
// cart bom_shortfall items (replaces the old add-shortfall endpoint).
func TestBuildDrivenShortfall(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-shortfall@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Set up project + revision with resolved item; part has 0 stock → shortfall.
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Shortfall-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "SF-PART-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 3) // needs 3
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	// No inventory entry → stock=0, need=3, shortfall=3.

	// Create a draft build.
	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 1}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201 creating build, got %d: %v", status, created)
	}
	buildID := created["id"].(string)

	// Submit the build → pending; cart shortfall should now show 1 item.
	submitBody, submitStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildID), nil, token)
	if submitStatus != http.StatusOK {
		t.Fatalf("expected 200 on submit, got %d: %v", submitStatus, submitBody)
	}

	cartBody, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	items, _ := cartBody["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 bom_shortfall cart item after submit, got %d", len(items))
	}
}

// TestUnallocatedOnOrderReducesShortfall verifies that excess on an existing
// placed order (quantity_ordered - quantity_committed_to_builds) is counted as
// available supply when computing a new build's cart shortfall (Decision 3).
func TestUnallocatedOnOrderReducesShortfall(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-unalloc@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	token := testutil.CreateSession(t, testDB, userID)

	partID := testutil.CreatePart(t, testDB, "global", "UNALLOC-PART-01", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "UNALLOC-SKU-01")
	testutil.CreateInventoryEntry(t, testDB, teamID, partID) // 0 stock

	// Placed order: 100 ordered, 0 received.
	orderID := testutil.CreateOrder(t, testDB, teamID, distID, userID)
	testutil.CreateOrderLineItem(t, testDB, orderID, listingID, 100)
	testutil.PlaceOrder(t, testDB, orderID)

	// Build A is 'ordered', waiting on 5 units from the above order.
	projectA := testutil.CreateProject(t, testDB, teamID, userID, "Unalloc-PCB-A")
	revA := testutil.CreateRevision(t, testDB, projectA, userID, "v1")
	buildA := testutil.CreateBuild(t, testDB, projectA, revA, teamID, userID, 5, "ordered")
	testutil.CreateBuildPartAllocation(t, testDB, buildA, partID, 5, 0, 0)
	testutil.LinkBuildToOrder(t, testDB, buildA, orderID)

	// Build B needs 10 units of the same part. Unallocated inbound = 100 - 5 = 95.
	// Shortfall for Build B = max(0, 10 - stock(0) - unallocated(95)) = 0.
	projectB := testutil.CreateProject(t, testDB, teamID, userID, "Unalloc-PCB-B")
	revB := testutil.CreateRevision(t, testDB, projectB, userID, "v1")
	itemB := testutil.CreateBOMLineItem(t, testDB, revB, "R1", 10) // 10 per board
	testutil.ResolveBOMLineItem(t, testDB, itemB, partID)

	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectB, "bom_revision_id": revB, "quantity": 1}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, created)
	}
	buildBID := created["id"].(string)

	// Submit Build B → cart shortfall should be 0 (excess order covers demand).
	submitBody, submitStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildBID), nil, token)
	if submitStatus != http.StatusOK {
		t.Fatalf("expected 200 on submit, got %d: %v", submitStatus, submitBody)
	}

	cartBody, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	items, _ := cartBody["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 cart items (excess on-order covers demand), got %d", len(items))
	}
}

func TestAddLowStock(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "cart-lowstock@example.com", "pw", "Cart")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Cart Team")
	partID := testutil.CreatePart(t, testDB, "global", "LS-PART-01", nil)
	token := testutil.CreateSession(t, testDB, userID)

	// Inventory entry: min=5, qty=2 → below min.
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 2)
	testDB.Exec(context.Background(), `UPDATE inventory_entries SET minimum_stock_quantity = 5 WHERE id = $1`, entryID) //nolint:errcheck

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/add-low-stock", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["added_count"].(float64) != 1 {
		t.Errorf("expected added_count=1, got %v", body["added_count"])
	}
}
