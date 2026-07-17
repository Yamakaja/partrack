package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// collectionsTestSetup creates a member user with a team and returns
// (userID, teamID, token).
func collectionsTestSetup(t *testing.T, email string) (string, string, string) {
	t.Helper()
	userID := testutil.CreateUser(t, testDB, email, "pw", "Coll")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Coll Team")
	token := testutil.CreateSession(t, testDB, userID)
	return userID, teamID, token
}

func createCollectionViaAPI(t *testing.T, teamID, token, name string) string {
	t.Helper()
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/collections", teamID),
		map[string]any{"name": name}, token)
	if status != http.StatusCreated {
		t.Fatalf("create collection: expected 201, got %d: %v", status, body)
	}
	return body["id"].(string)
}

func TestCollections_CRUDLifecycle(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-crud@example.com")

	// Create with blank name → auto-named.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/collections", teamID),
		map[string]any{}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	collID := body["id"].(string)
	if name := body["name"].(string); name == "" {
		t.Error("expected auto-generated name for blank create")
	}

	// Rename + set the collect toggle.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID),
		map[string]any{"name": "Desk pile", "collect_capture_qty": true}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["name"].(string) != "Desk pile" {
		t.Errorf("expected renamed collection, got %v", body["name"])
	}
	if v, _ := body["collect_capture_qty"].(bool); !v {
		t.Errorf("expected collect_capture_qty=true, got %v", body["collect_capture_qty"])
	}

	// Archive: disappears from the default listing, appears with ?archived=true.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID),
		map[string]any{"archived": true}, token)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d", status)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections", teamID), nil, token)
	if got := len(body["collections"].([]any)); got != 0 {
		t.Errorf("expected 0 active collections after archive, got %d", got)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections?archived=true", teamID), nil, token)
	if got := len(body["collections"].([]any)); got != 1 {
		t.Errorf("expected 1 collection incl. archived, got %d", got)
	}

	// Hard delete.
	_, status = testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", status)
	}
	_, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	if status != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", status)
	}
}

func TestCollections_UserIsolation(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-owner@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "Private")

	// A second member of the SAME team must not see or reach the list.
	otherID := testutil.CreateUser(t, testDB, "coll-other@example.com", "pw", "Other")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	otherToken := testutil.CreateSession(t, testDB, otherID)

	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections", teamID), nil, otherToken)
	if got := len(body["collections"].([]any)); got != 0 {
		t.Errorf("teammate sees %d foreign collections, want 0", got)
	}

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil},
		{http.MethodPatch, fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), map[string]any{"name": "hijack"}},
		{http.MethodDelete, fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil},
		{http.MethodPost, fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID), map[string]any{"part_id": collID}},
	} {
		_, status := testutil.Do(t, testEnv.Server, tc.method, tc.path, tc.body, otherToken)
		if status != http.StatusNotFound {
			t.Errorf("%s %s as non-owner: expected 404, got %d", tc.method, tc.path, status)
		}
	}
}

func TestCollectionItems_UpsertMergeSemantics(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-merge@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "Merge")
	partID := testutil.CreatePart(t, testDB, "global", "COLL-MERGE-01", nil)

	itemsPath := fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID)

	// Bare add: quantity stays NULL.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
		map[string]any{"part_id": partID}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["quantity"] != nil {
		t.Errorf("bare add: expected quantity null, got %v", body["quantity"])
	}
	itemID := body["id"].(string)

	// Re-add bare: still NULL, still the same row (collapse).
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
		map[string]any{"part_id": partID}, token)
	if body["id"].(string) != itemID {
		t.Errorf("re-add created a new row: %v != %v", body["id"], itemID)
	}
	if body["quantity"] != nil {
		t.Errorf("bare re-add: expected quantity to stay null, got %v", body["quantity"])
	}

	// Add with qty onto NULL: NULL counts as 0, so quantity becomes 100.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
		map[string]any{"part_id": partID, "quantity": 100}, token)
	if q := body["quantity"].(float64); q != 100 {
		t.Errorf("expected quantity 100, got %v", q)
	}

	// Add with qty again: sums.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
		map[string]any{"part_id": partID, "quantity": 50}, token)
	if q := body["quantity"].(float64); q != 150 {
		t.Errorf("expected summed quantity 150, got %v", q)
	}

	// Bare add after quantities exist: leaves the quantity untouched.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
		map[string]any{"part_id": partID}, token)
	if q := body["quantity"].(float64); q != 150 {
		t.Errorf("bare add must not change quantity: got %v", q)
	}

	// PATCH explicit null clears the quantity.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("%s/%s", itemsPath, itemID),
		map[string]any{"quantity": nil}, token)
	if status != http.StatusOK {
		t.Fatalf("clear qty: expected 200, got %d: %v", status, body)
	}
	if body["quantity"] != nil {
		t.Errorf("expected cleared quantity, got %v", body["quantity"])
	}

	// Zero / negative quantities are rejected.
	for _, bad := range []int{0, -5} {
		_, status = testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
			map[string]any{"part_id": partID, "quantity": bad}, token)
		if status != http.StatusBadRequest {
			t.Errorf("quantity %d: expected 400, got %d", bad, status)
		}
	}
}

func TestCollectionItems_PartScopeGuard(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-scope@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "Scope")

	// A part scoped to a DIFFERENT team must not be addable.
	otherOwner := testutil.CreateUser(t, testDB, "coll-scope-other@example.com", "pw", "O")
	otherTeam := testutil.CreateTeam(t, testDB, otherOwner, "member", "Other Team")
	foreignPart := testutil.CreatePart(t, testDB, "team", "COLL-FOREIGN-01", &otherTeam)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID),
		map[string]any{"part_id": foreignPart}, token)
	if status != http.StatusForbidden {
		t.Errorf("foreign team part: expected 403, got %d", status)
	}
}

func TestCollectionItems_ListAndBulkAdd(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-bulk@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "Bulk")
	partA := testutil.CreatePart(t, testDB, "global", "COLL-BULK-A", nil)
	partB := testutil.CreatePart(t, testDB, "global", "COLL-BULK-B", nil)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s/items/bulk", teamID, collID),
		map[string]any{"items": []any{
			map[string]any{"part_id": partA, "quantity": 10, "note": "half reel"},
			map[string]any{"part_id": partB},
		}}, token)
	if status != http.StatusOK {
		t.Fatalf("bulk add: expected 200, got %d: %v", status, body)
	}
	if added := body["added"].(float64); added != 2 {
		t.Errorf("expected added=2, got %v", added)
	}

	body, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %v", status, body)
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// Insertion order: partA first, with the PartCell fields joined in.
	first := items[0].(map[string]any)
	if first["mpn"].(string) != "COLL-BULK-A" {
		t.Errorf("expected first item COLL-BULK-A, got %v", first["mpn"])
	}
	if first["quantity"].(float64) != 10 {
		t.Errorf("expected quantity 10, got %v", first["quantity"])
	}
	if first["note"].(string) != "half reel" {
		t.Errorf("expected note, got %v", first["note"])
	}
	if _, hasLifecycle := first["lifecycle_status"]; !hasLifecycle {
		t.Error("expected lifecycle_status in item response")
	}

	// The listing rolls up counts.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections", teamID), nil, token)
	coll := body["collections"].([]any)[0].(map[string]any)
	if c := coll["item_count"].(float64); c != 2 {
		t.Errorf("expected item_count 2, got %v", c)
	}
	if q := coll["total_quantity"].(float64); q != 10 {
		t.Errorf("expected total_quantity 10, got %v", q)
	}
	if u := coll["unquantified_count"].(float64); u != 1 {
		t.Errorf("expected unquantified_count 1, got %v", u)
	}

	// Delete one item.
	itemID := first["id"].(string)
	_, status = testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s/items/%s", teamID, collID, itemID), nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("delete item: expected 204, got %d", status)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	if got := len(body["items"].([]any)); got != 1 {
		t.Errorf("expected 1 item after delete, got %d", got)
	}
}

func TestCollections_ViewerForbidden(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID := testutil.CreateUser(t, testDB, "coll-viewer@example.com", "pw", "V")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "V Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections", teamID), nil, token)
	if status != http.StatusForbidden {
		t.Errorf("viewer list: expected 403, got %d", status)
	}
}

func TestBulkAddCartItems(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID := testutil.CreateUser(t, testDB, "cart-bulk@example.com", "pw", "CB")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "CB Team")
	token := testutil.CreateSession(t, testDB, userID)
	partA := testutil.CreatePart(t, testDB, "global", "CART-BULK-A", nil)
	partB := testutil.CreatePart(t, testDB, "global", "CART-BULK-B", nil)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items/bulk", teamID),
		map[string]any{"items": []any{
			map[string]any{"part_id": partA, "quantity": 25},
			map[string]any{"part_id": partB, "quantity": 100},
		}}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if added := body["added"].(float64); added != 2 {
		t.Errorf("expected added=2, got %v", added)
	}

	// Re-adding partA merges quantities (cart merge rule).
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items/bulk", teamID),
		map[string]any{"items": []any{map[string]any{"part_id": partA, "quantity": 5}}}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 cart items, got %d", len(items))
	}
	qtyByMpn := map[string]float64{}
	for _, it := range items {
		m := it.(map[string]any)
		qtyByMpn[m["part_mpn"].(string)] = m["quantity"].(float64)
	}
	if qtyByMpn["CART-BULK-A"] != 30 {
		t.Errorf("expected CART-BULK-A qty 30 after merge, got %v", qtyByMpn["CART-BULK-A"])
	}
	if qtyByMpn["CART-BULK-B"] != 100 {
		t.Errorf("expected CART-BULK-B qty 100, got %v", qtyByMpn["CART-BULK-B"])
	}

	// Empty and invalid payloads are rejected before any write.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items/bulk", teamID),
		map[string]any{"items": []any{}}, token)
	if status != http.StatusBadRequest {
		t.Errorf("empty items: expected 400, got %d", status)
	}
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items/bulk", teamID),
		map[string]any{"items": []any{map[string]any{"part_id": partA, "quantity": 0}}}, token)
	if status != http.StatusBadRequest {
		t.Errorf("zero quantity: expected 400, got %d", status)
	}
}

func TestCollections_ClearItems(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-clear@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "Clear me")
	partA := testutil.CreatePart(t, testDB, "global", "COLL-CLEAR-01", nil)
	partB := testutil.CreatePart(t, testDB, "global", "COLL-CLEAR-02", nil)

	itemsPath := fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID)
	for _, p := range []string{partA, partB} {
		if body, status := testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
			map[string]any{"part_id": p, "quantity": 5}, token); status != http.StatusCreated {
			t.Fatalf("add item: expected 201, got %d: %v", status, body)
		}
	}

	// A teammate cannot clear a list they don't own (owner filter → 404).
	otherID := testutil.CreateUser(t, testDB, "coll-clear-other@example.com", "pw", "Other")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	otherToken := testutil.CreateSession(t, testDB, otherID)
	if _, status := testutil.Do(t, testEnv.Server, http.MethodDelete, itemsPath, nil, otherToken); status != http.StatusNotFound {
		t.Errorf("clear as non-owner: expected 404, got %d", status)
	}

	body, status := testutil.Do(t, testEnv.Server, http.MethodDelete, itemsPath, nil, token)
	if status != http.StatusOK {
		t.Fatalf("clear: expected 200, got %d: %v", status, body)
	}
	if n := body["removed"].(float64); n != 2 {
		t.Errorf("expected removed=2, got %v", n)
	}

	// The list itself survives, empty.
	body, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("get after clear: expected 200, got %d", status)
	}
	if items := body["items"].([]any); len(items) != 0 {
		t.Errorf("expected empty items after clear, got %d", len(items))
	}
}
