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

func TestCollections_AddSearchResults(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-search@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "From search")

	// Two resistors + one capacitor; the search below matches only the resistors.
	testutil.CreatePart(t, testDB, "global", "SEARCHADD-RES-1", nil)
	testutil.CreatePart(t, testDB, "global", "SEARCHADD-RES-2", nil)
	testutil.CreatePart(t, testDB, "global", "SEARCHADD-CAP-1", nil)

	fromSearch := fmt.Sprintf("/api/v1/teams/%s/collections/%s/items/from-search", teamID, collID)

	// Scoped to the "RES" search → exactly the two resistors, added as bare membership.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fromSearch+"?q=SEARCHADD-RES", nil, token)
	if status != http.StatusOK {
		t.Fatalf("from-search: expected 200, got %d: %v", status, body)
	}
	if added := body["added"].(float64); added != 2 {
		t.Fatalf("expected added=2, got %v", added)
	}

	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items after RES search, got %d", len(items))
	}
	for _, it := range items {
		if q := it.(map[string]any)["quantity"]; q != nil {
			t.Errorf("expected bare membership (null quantity), got %v", q)
		}
	}

	// Widening the search to all three parts is idempotent for the two already
	// present and adds only the capacitor → three items total.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fromSearch+"?q=SEARCHADD", nil, token)
	if status != http.StatusOK {
		t.Fatalf("second from-search: expected 200, got %d: %v", status, body)
	}
	if added := body["added"].(float64); added != 3 {
		t.Errorf("expected added=3, got %v", added)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	if got := len(body["items"].([]any)); got != 3 {
		t.Errorf("expected 3 items after widening, got %d", got)
	}

	// A search that matches nothing adds nothing (not an error).
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fromSearch+"?q=NOSUCHPARTZZZ", nil, token)
	if status != http.StatusOK {
		t.Fatalf("empty-match from-search: expected 200, got %d: %v", status, body)
	}
	if added := body["added"].(float64); added != 0 {
		t.Errorf("expected added=0 for no matches, got %v", added)
	}

	// A teammate who doesn't own the list can't add to it (owner filter → 404).
	otherID := testutil.CreateUser(t, testDB, "coll-search-other@example.com", "pw", "Other")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	otherToken := testutil.CreateSession(t, testDB, otherID)
	if _, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fromSearch+"?q=SEARCHADD", nil, otherToken); status != http.StatusNotFound {
		t.Errorf("from-search as non-owner: expected 404, got %d", status)
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

// The checkbox on list rows is a persisted, cross-device selection: the desktop
// panel scopes its verbs by it and the phone's pick mode writes it. These tests
// pin the write paths and the counts both surfaces read.

func TestCollectionItems_CheckedSelection(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-checked@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "Pile")

	itemsPath := fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID)
	itemIDs := make([]string, 0, 3)
	for i, mpn := range []string{"COLL-CHK-01", "COLL-CHK-02", "COLL-CHK-03"} {
		partID := testutil.CreatePart(t, testDB, "global", mpn, nil)
		body, status := testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
			map[string]any{"part_id": partID, "quantity": 10 * (i + 1)}, token)
		if status != http.StatusCreated {
			t.Fatalf("add item %s: expected 201, got %d: %v", mpn, status, body)
		}
		if body["checked_at"] != nil {
			t.Errorf("new row must start unchecked, got %v", body["checked_at"])
		}
		itemIDs = append(itemIDs, body["id"].(string))
	}

	checkedPath := fmt.Sprintf("%s/checked", itemsPath)

	// PATCH checked=true stamps checked_at; the count follows on the summary.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("%s/%s", itemsPath, itemIDs[0]),
		map[string]any{"checked": true}, token)
	if status != http.StatusOK {
		t.Fatalf("check item: expected 200, got %d: %v", status, body)
	}
	firstCheckedAt, _ := body["checked_at"].(string)
	if firstCheckedAt == "" {
		t.Fatalf("expected checked_at to be set, got %v", body["checked_at"])
	}
	assertCheckedCount(t, teamID, collID, token, 1)

	// Re-checking keeps the original stamp: a redundant write must not reorder
	// "most recently checked".
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("%s/%s", itemsPath, itemIDs[0]),
		map[string]any{"checked": true}, token)
	if got, _ := body["checked_at"].(string); got != firstCheckedAt {
		t.Errorf("re-check changed checked_at: %v → %v", firstCheckedAt, got)
	}

	// An absent `checked` leaves the selection alone (set-if-present, like note).
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("%s/%s", itemsPath, itemIDs[0]),
		map[string]any{"note": "front bin"}, token)
	if got, _ := body["checked_at"].(string); got != firstCheckedAt {
		t.Errorf("note edit must not touch checked_at: %v → %v", firstCheckedAt, got)
	}

	// PATCH checked=false clears it.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("%s/%s", itemsPath, itemIDs[0]),
		map[string]any{"checked": false}, token)
	if body["checked_at"] != nil {
		t.Errorf("expected checked_at cleared, got %v", body["checked_at"])
	}
	assertCheckedCount(t, teamID, collID, token, 0)

	// op=set writes a subset — the row toggle and shift-click range path.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, checkedPath,
		map[string]any{"op": "set", "item_ids": itemIDs[:2], "checked": true}, token)
	if status != http.StatusOK {
		t.Fatalf("op=set: expected 200, got %d: %v", status, body)
	}
	if n := body["checked_count"].(float64); n != 2 {
		t.Errorf("op=set: expected checked_count 2, got %v", n)
	}

	// op=invert flips the whole list: 2 checked of 3 becomes 1.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, checkedPath,
		map[string]any{"op": "invert"}, token)
	if n := body["checked_count"].(float64); n != 1 {
		t.Errorf("op=invert: expected checked_count 1, got %v", n)
	}
	if !itemChecked(t, teamID, collID, token, itemIDs[2]) {
		t.Error("op=invert: expected the previously unchecked row to be checked")
	}

	// op=all / op=none sweep.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, checkedPath,
		map[string]any{"op": "all"}, token)
	if n := body["checked_count"].(float64); n != 3 {
		t.Errorf("op=all: expected checked_count 3, got %v", n)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, checkedPath,
		map[string]any{"op": "none"}, token)
	if n := body["checked_count"].(float64); n != 0 {
		t.Errorf("op=none: expected checked_count 0, got %v", n)
	}

	// Bad ops and a set without ids are rejected.
	if _, status = testutil.Do(t, testEnv.Server, http.MethodPost, checkedPath,
		map[string]any{"op": "sometimes"}, token); status != http.StatusBadRequest {
		t.Errorf("unknown op: expected 400, got %d", status)
	}
	if _, status = testutil.Do(t, testEnv.Server, http.MethodPost, checkedPath,
		map[string]any{"op": "set", "checked": true}, token); status != http.StatusBadRequest {
		t.Errorf("op=set without item_ids: expected 400, got %d", status)
	}

	// Another team member cannot reach the list at all (owner filter → 404).
	otherID := testutil.CreateUser(t, testDB, "coll-checked-other@example.com", "pw", "Other")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	otherToken := testutil.CreateSession(t, testDB, otherID)
	if _, status = testutil.Do(t, testEnv.Server, http.MethodPost, checkedPath,
		map[string]any{"op": "all"}, otherToken); status != http.StatusNotFound {
		t.Errorf("selection op as non-owner: expected 404, got %d", status)
	}
}

func TestCollectionItems_ScopedDelete(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	_, teamID, token := collectionsTestSetup(t, "coll-scoped-del@example.com")
	collID := createCollectionViaAPI(t, teamID, token, "Scoped")

	itemsPath := fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID)
	itemIDs := make([]string, 0, 3)
	for _, mpn := range []string{"COLL-DEL-01", "COLL-DEL-02", "COLL-DEL-03"} {
		partID := testutil.CreatePart(t, testDB, "global", mpn, nil)
		body, status := testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
			map[string]any{"part_id": partID}, token)
		if status != http.StatusCreated {
			t.Fatalf("add item %s: expected 201, got %d: %v", mpn, status, body)
		}
		itemIDs = append(itemIDs, body["id"].(string))
	}

	// item_ids narrows the delete to the selection; the rest of the list stays.
	body, status := testutil.Do(t, testEnv.Server, http.MethodDelete, itemsPath,
		map[string]any{"item_ids": itemIDs[:2]}, token)
	if status != http.StatusOK {
		t.Fatalf("scoped delete: expected 200, got %d: %v", status, body)
	}
	if n := body["removed"].(float64); n != 2 {
		t.Errorf("expected removed=2, got %v", n)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 surviving row, got %d", len(items))
	}
	if got := items[0].(map[string]any)["id"].(string); got != itemIDs[2] {
		t.Errorf("wrong row survived: got %v, want %v", got, itemIDs[2])
	}

	// An empty id list is a no-op rather than a full clear.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodDelete, itemsPath,
		map[string]any{"item_ids": []string{}}, token)
	if n := body["removed"].(float64); n != 0 {
		t.Errorf("empty item_ids: expected removed=0, got %v", n)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	if got := len(body["items"].([]any)); got != 1 {
		t.Errorf("empty item_ids must not clear the list, got %d rows", got)
	}
}

// assertCheckedCount checks the summary listing's checked_count for one list.
func assertCheckedCount(t *testing.T, teamID, collID, token string, want float64) {
	t.Helper()
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("list collections: expected 200, got %d", status)
	}
	for _, raw := range body["collections"].([]any) {
		c := raw.(map[string]any)
		if c["id"].(string) != collID {
			continue
		}
		if got := c["checked_count"].(float64); got != want {
			t.Errorf("checked_count: got %v, want %v", got, want)
		}
		return
	}
	t.Fatalf("collection %s not found in listing", collID)
}

func itemChecked(t *testing.T, teamID, collID, token, itemID string) bool {
	t.Helper()
	body, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, token)
	for _, raw := range body["items"].([]any) {
		it := raw.(map[string]any)
		if it["id"].(string) == itemID {
			return it["checked_at"] != nil
		}
	}
	t.Fatalf("item %s not found", itemID)
	return false
}
