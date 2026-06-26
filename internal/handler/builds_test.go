package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListBuilds_Empty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-list@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestPreviewBuild_NoDeductions(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-preview-empty@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Bld-PCB-A")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	// No resolved items → no deductions.
	testutil.CreateBOMLineItem(t, testDB, revID, "U1", 1)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/preview", teamID),
		map[string]any{"bom_revision_id": revID, "quantity": 1}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items (no resolved items), got %d", len(items))
	}
}

func TestPreviewBuild_WithDeductions(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-preview-ded@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Bld-PCB-B")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "BLD-PART-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1,R2", 2)
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 20)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/preview", teamID),
		map[string]any{"bom_revision_id": revID, "quantity": 3}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	d := items[0].(map[string]any)
	// 2 per board × 3 boards = 6 total.
	if d["total_quantity"].(float64) != 6 {
		t.Errorf("expected total_quantity=6, got %v", d["total_quantity"])
	}
	if d["sufficient"].(bool) != true {
		t.Errorf("expected sufficient=true (20 on hand >= 6 needed), got %v", d["sufficient"])
	}

	// Preview must NOT have changed stock.
	var qty int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 20 {
		t.Errorf("preview must not modify stock; expected 20, got %d", qty)
	}
}

func TestCreateBuild(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-create@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Bld-PCB-C")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "BLD-PART-02", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "C1", 1)
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 10)

	// Create draft build — no stock deduction.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{
			"project_id":      projectID,
			"bom_revision_id": revID,
			"quantity":        2,
		}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["status"].(string) != "draft" {
		t.Errorf("expected status=draft, got %v", body["status"])
	}
	if body["quantity"].(float64) != 2 {
		t.Errorf("expected quantity=2, got %v", body["quantity"])
	}
	buildID := body["id"].(string)

	// Stock should be unchanged (draft creates no deduction).
	var qty int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 10 {
		t.Errorf("expected stock=10 after draft create, got %d", qty)
	}

	// Checkout the build — this should deduct stock.
	checkoutBody, checkoutStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/checkout", teamID, buildID),
		map[string]any{}, token)
	if checkoutStatus != http.StatusOK {
		t.Fatalf("expected 200 on checkout, got %d: %v", checkoutStatus, checkoutBody)
	}
	if checkoutBody["status"].(string) != "assembled" {
		t.Errorf("expected status=assembled after checkout, got %v", checkoutBody["status"])
	}
	if checkoutBody["deduction_count"].(float64) != 1 {
		t.Errorf("expected deduction_count=1, got %v", checkoutBody["deduction_count"])
	}

	// Stock should be 10 - 1*2 = 8.
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 8 {
		t.Errorf("expected stock=8 after checkout, got %d", qty)
	}
}

// TestPartialCheckout verifies that checkout with assembled_quantity < build.Quantity
// deducts stock for only the assembled boards and leaves the build active with
// a decremented quantity (US-B12).
func TestPartialCheckout(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-partchk@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Partial-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "PRTCHK-PART-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "U1", 1) // 1 per board
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 10)

	// Create draft build for 4 boards.
	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 4}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, created)
	}
	buildID := created["id"].(string)

	// Partial checkout: assemble only 2 boards.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/checkout", teamID, buildID),
		map[string]any{"assembled_quantity": 2}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["status"].(string) == "assembled" {
		t.Error("partial checkout should not set status=assembled")
	}
	if body["remaining_quantity"].(float64) != 2 {
		t.Errorf("expected remaining_quantity=2, got %v", body["remaining_quantity"])
	}
	if body["deduction_count"].(float64) != 1 {
		t.Errorf("expected deduction_count=1, got %v", body["deduction_count"])
	}

	// Stock should be 10 - 2 = 8.
	var qty int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 8 {
		t.Errorf("expected stock=8 after partial checkout, got %d", qty)
	}

	// Build quantity should be reduced to 2.
	getBuild, getStatus := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
	if getStatus != http.StatusOK {
		t.Fatalf("expected 200 on get-build, got %d", getStatus)
	}
	if getBuild["quantity"].(float64) != 2 {
		t.Errorf("expected build quantity=2 after partial checkout, got %v", getBuild["quantity"])
	}
}

// TestPartialCheckout_AssembledQtyTooLarge verifies that assembled_quantity
// greater than the build quantity is rejected (US-B12 validation).
func TestPartialCheckout_AssembledQtyTooLarge(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-prtbig@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Big-Qty-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")

	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 3}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, created)
	}
	buildID := created["id"].(string)

	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/checkout", teamID, buildID),
		map[string]any{"assembled_quantity": 5}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for assembled_quantity > build.Quantity, got %d", status)
	}
}

// TestCheckout_PendingBuild_Rejected verifies that a pending build cannot be
// checked out (US-B2: only draft and ready are checkoutable).
func TestCheckout_PendingBuild_Rejected(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-chkpend@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Pending-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	// Resolved part with no stock → submit leaves the build genuinely pending
	// (a full-stock build would now be promoted straight to ready on submit).
	partID := testutil.CreatePart(t, testDB, "global", "CHKPEND-PART-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 1)
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)

	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 1}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, created)
	}
	buildID := created["id"].(string)

	// Wait for substitute selections to be computed, then submit → pending.
	testutil.WaitForBuildSelections(t, testEnv.Server, teamID, buildID, token)
	submitBody, submitStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildID), nil, token)
	if submitStatus != http.StatusOK {
		t.Fatalf("expected 200 on submit, got %d: %v", submitStatus, submitBody)
	}
	if submitBody["status"] != "pending" {
		t.Fatalf("expected status=pending after submit, got %v", submitBody["status"])
	}

	// Checkout a pending build should return 409.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/checkout", teamID, buildID),
		map[string]any{}, token)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for checkout of pending build, got %d", status)
	}
}

// TestCancelBuild_ClearsShortfall verifies that cancelling a pending build
// recomputes the cart, removing its bom_shortfall items (US-B9).
func TestCancelBuild_ClearsShortfall(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-cancel-sf@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Cancel-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "CANCEL-SF-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 2) // 2/board
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	// No inventory entry → shortfall = 2.

	// Create + submit build → shortfall in cart.
	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 1}, token)
	buildID := created["id"].(string)

	testutil.WaitForBuildSelections(t, testEnv.Server, teamID, buildID, token)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildID), nil, token)

	cartBefore, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if len(cartBefore["items"].([]any)) == 0 {
		t.Fatal("expected shortfall items in cart after submit")
	}

	// Cancel the build.
	_, cancelStatus := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
	if cancelStatus != http.StatusNoContent {
		t.Fatalf("expected 204 on cancel, got %d", cancelStatus)
	}

	// Cart should now be empty.
	cartAfter, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	itemsAfter, _ := cartAfter["items"].([]any)
	if len(itemsAfter) != 0 {
		t.Errorf("expected empty cart after build cancel, got %d items", len(itemsAfter))
	}
}

// TestToggleBuildCartInclusion verifies that excluding a build from the cart
// removes its shortfall demand, and re-including it restores demand (US-B10).
func TestToggleBuildCartInclusion(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-toggle@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Toggle-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "TOGGLE-PART-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "C1", 1)
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	// No inventory → shortfall.

	// Create + submit → shortfall in cart.
	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 1}, token)
	buildID := created["id"].(string)

	testutil.WaitForBuildSelections(t, testEnv.Server, teamID, buildID, token)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildID), nil, token)

	cartInitial, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if len(cartInitial["items"].([]any)) == 0 {
		t.Fatal("expected shortfall items in cart after submit")
	}

	// Exclude build from cart → shortfall removed.
	_, patchStatus := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/cart-inclusion", teamID, buildID),
		map[string]any{"excluded": true}, token)
	if patchStatus != http.StatusNoContent {
		t.Fatalf("expected 204 on exclude, got %d", patchStatus)
	}

	cartExcluded, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if len(cartExcluded["items"].([]any)) != 0 {
		t.Errorf("expected empty cart after exclude, got %d items", len(cartExcluded["items"].([]any)))
	}

	// Re-include build → shortfall restored.
	testutil.Do(t, testEnv.Server, http.MethodPatch, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/cart-inclusion", teamID, buildID),
		map[string]any{"excluded": false}, token)

	cartRestored, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if len(cartRestored["items"].([]any)) == 0 {
		t.Errorf("expected shortfall items restored after re-include")
	}
}

// TestManualStockAdjustment_TransitionsBuildToReady verifies that a positive
// manual stock adjustment triggers build readiness re-evaluation: an ordered
// build waiting on free stock is transitioned to ready when stock arrives.
func TestManualStockAdjustment_TransitionsBuildToReady(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-adjrdy@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Create part with zero stock.
	partID := testutil.CreatePart(t, testDB, "global", "ADJRDY-PART-01", nil)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)

	// Create a build skeleton so we have a project/revision reference.
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "AdjRdy-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")

	// Insert a build directly in 'ordered' status.
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 5, "ordered")
	testutil.CreateBuildPartAllocation(t, testDB, buildID, partID, 5, 0, 0)

	// Create a manual inventory entry for the adjustment endpoint (entryID already exists).
	invBody, invStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/inventory/%s/adjustments", teamID, entryID),
		map[string]any{"delta": 5, "reason": "batch arrived"}, token)
	if invStatus != http.StatusCreated {
		t.Fatalf("expected 201 on adjustment, got %d: %v", invStatus, invBody)
	}

	// Build should now be in 'ready' status (reservation covered by new stock).
	var buildStatus string
	testDB.QueryRow(context.Background(),
		`SELECT status FROM builds WHERE id = $1`, buildID,
	).Scan(&buildStatus) //nolint:errcheck
	if buildStatus != "ready" {
		t.Errorf("expected build status=ready after stock arrival, got %s", buildStatus)
	}

	// Reservation should have been updated to 5.
	var quantityReserved int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_reserved FROM build_part_allocations WHERE build_id = $1 AND part_id = $2`,
		buildID, partID,
	).Scan(&quantityReserved) //nolint:errcheck
	if quantityReserved != 5 {
		t.Errorf("expected quantity_reserved=5 after manual adjustment, got %d", quantityReserved)
	}
}

// TestConvertCart_TransitionsBuildToOrdered verifies that ConvertCart creates
// part allocations for pending builds and transitions them to ordered when
// parts need to come from the new orders (US-B6, US-B8).
func TestConvertCart_TransitionsBuildToOrdered(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-convord@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Part with 0 stock.
	partID := testutil.CreatePart(t, testDB, "global", "CONVORD-PART-01", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "CONVORD-SKU-01")

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "ConvOrd-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "C1", 3) // 3 per board
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)

	// Insert a pending build directly (skipping the submit flow so we control
	// cart items precisely).
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 1, "pending")

	// Add the part to cart with an explicit listing so ConvertCart can resolve it.
	addBody, addStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partID, "quantity": 3, "listing_id": listingID}, token)
	if addStatus != http.StatusCreated {
		t.Fatalf("expected 201 adding cart item, got %d: %v", addStatus, addBody)
	}

	// Convert cart → creates order, transitions build to ordered with allocations.
	convertBody, convertStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/convert", teamID), nil, token)
	if convertStatus != http.StatusOK {
		t.Fatalf("expected 200 on convert, got %d: %v", convertStatus, convertBody)
	}
	orderIDs, _ := convertBody["order_ids"].([]any)
	if len(orderIDs) != 1 {
		t.Fatalf("expected 1 order created, got %d", len(orderIDs))
	}

	// Build should now be in ordered status.
	var buildStatus string
	testDB.QueryRow(context.Background(),
		`SELECT status FROM builds WHERE id = $1`, buildID,
	).Scan(&buildStatus) //nolint:errcheck
	if buildStatus != "ordered" {
		t.Errorf("expected build status=ordered after convert, got %s", buildStatus)
	}

	// Allocation should exist: quantity_needed = 3 (1 board × 3/board).
	var allocNeeded int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_needed FROM build_part_allocations WHERE build_id = $1 AND part_id = $2`,
		buildID, partID,
	).Scan(&allocNeeded) //nolint:errcheck
	if allocNeeded != 3 {
		t.Errorf("expected quantity_needed=3 in allocation, got %d", allocNeeded)
	}
}

// TestConvertCart_TransitionsBuildToReady verifies that a build with all parts
// covered by existing stock transitions directly to ready during convert.
func TestConvertCart_TransitionsBuildToReady(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-convrdy@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Part A: the build's part with sufficient stock (no shortfall expected).
	partA := testutil.CreatePart(t, testDB, "global", "CONVRDY-PARTA", nil)
	entryA := testutil.CreateInventoryEntry(t, testDB, teamID, partA)
	testutil.SetStockQty(t, testDB, entryA, 10) // plenty of stock

	// Part B: an unrelated part added manually to cart so convert has something to do.
	partB := testutil.CreatePart(t, testDB, "global", "CONVRDY-PARTB", nil)
	distID := testutil.GetTestDistributorID(t, testDB)
	listingB := testutil.CreateListing(t, testDB, partB, distID, "CONVRDY-SKU-B")

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "ConvRdy-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 1) // 1 per board, from part A
	testutil.ResolveBOMLineItem(t, testDB, itemID, partA)

	// Create + submit build for 1 board. Stock=10 >= need=1 → no shortfall.
	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 1}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, created)
	}
	buildID := created["id"].(string)
	testutil.WaitForBuildSelections(t, testEnv.Server, teamID, buildID, token)
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildID), nil, token)

	// Add part B manually to cart so convert has at least one item.
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/cart/items", teamID),
		map[string]any{"part_id": partB, "quantity": 1, "listing_id": listingB}, token)

	// Convert cart → build should transition to ready (all parts covered by stock).
	convertBody, convertStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/cart/convert", teamID), nil, token)
	if convertStatus != http.StatusOK {
		t.Fatalf("expected 200 on convert, got %d: %v", convertStatus, convertBody)
	}

	var buildStatus string
	testDB.QueryRow(context.Background(),
		`SELECT status FROM builds WHERE id = $1`, buildID,
	).Scan(&buildStatus) //nolint:errcheck
	if buildStatus != "ready" {
		t.Errorf("expected build status=ready (all parts covered by stock), got %s", buildStatus)
	}

	// Allocation for part A: reserved=1, needed=1.
	var reserved, needed int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_reserved, quantity_needed FROM build_part_allocations WHERE build_id = $1 AND part_id = $2`,
		buildID, partA,
	).Scan(&reserved, &needed) //nolint:errcheck
	if reserved != 1 || needed != 1 {
		t.Errorf("expected allocation reserved=1, needed=1; got reserved=%d, needed=%d", reserved, needed)
	}
}

// TestSubmitFullStockBuild_ReadyImmediately verifies the fix for the
// "build not ready with plenty of local stock" bug: submitting a build whose
// parts are fully covered by free (non-reserved) stock reserves that stock,
// promotes the build straight to 'ready' (skipping purchasing), and leaves it
// directly checkoutable — without needing a cart convert.
func TestSubmitFullStockBuild_ReadyImmediately(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-rdy-now@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "RdyNow-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "RDYNOW-PART-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 1) // 1 per board
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 10) // plenty of stock for need=2

	// Create draft build for 2 boards (need=2, stock=10 → fully covered).
	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 2}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, created)
	}
	buildID := created["id"].(string)

	// Submit → should go straight to ready (no shortfall, no convert needed).
	testutil.WaitForBuildSelections(t, testEnv.Server, teamID, buildID, token)
	submitBody, submitStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildID), nil, token)
	if submitStatus != http.StatusOK {
		t.Fatalf("expected 200 on submit, got %d: %v", submitStatus, submitBody)
	}
	if submitBody["status"] != "ready" {
		t.Fatalf("expected submit to report status=ready, got %v", submitBody["status"])
	}

	// Build is ready with a soft reservation covering the full demand.
	var buildStatus string
	testDB.QueryRow(context.Background(),
		`SELECT status FROM builds WHERE id = $1`, buildID,
	).Scan(&buildStatus) //nolint:errcheck
	if buildStatus != "ready" {
		t.Fatalf("expected build status=ready, got %s", buildStatus)
	}
	var reserved, needed int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_reserved, quantity_needed FROM build_part_allocations WHERE build_id = $1 AND part_id = $2`,
		buildID, partID,
	).Scan(&reserved, &needed) //nolint:errcheck
	if reserved != 2 || needed != 2 {
		t.Errorf("expected allocation reserved=2, needed=2; got reserved=%d, needed=%d", reserved, needed)
	}

	// No shortfall reached the cart.
	cart, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if items, _ := cart["items"].([]any); len(items) != 0 {
		t.Fatalf("expected empty cart for full-stock build, got %d items", len(items))
	}

	// A ready build is directly checkoutable — no cart convert required.
	checkoutBody, checkoutStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/checkout", teamID, buildID),
		map[string]any{}, token)
	if checkoutStatus != http.StatusOK {
		t.Fatalf("expected 200 on checkout of ready build, got %d: %v", checkoutStatus, checkoutBody)
	}
	if checkoutBody["status"] != "assembled" {
		t.Errorf("expected status=assembled after checkout, got %v", checkoutBody["status"])
	}

	// Stock deducted: 10 - 1*2 = 8.
	var qty int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 8 {
		t.Errorf("expected stock=8 after checkout, got %d", qty)
	}
}

// TestSubmitPartialStockBuild_StaysPending verifies that a build NOT fully
// covered by free stock stays 'pending' (feeding the cart) and carries no
// allocations — leaving ConvertCart's reservation logic to handle it.
func TestSubmitPartialStockBuild_StaysPending(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-partial@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Partial-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	partID := testutil.CreatePart(t, testDB, "global", "PARTIAL-PART-01", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 2) // 2 per board
	testutil.ResolveBOMLineItem(t, testDB, itemID, partID)
	entryID := testutil.CreateInventoryEntry(t, testDB, teamID, partID)
	testutil.SetStockQty(t, testDB, entryID, 3) // need=4 (2 boards × 2), only 3 free

	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 2}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, created)
	}
	buildID := created["id"].(string)

	testutil.WaitForBuildSelections(t, testEnv.Server, teamID, buildID, token)
	submitBody, submitStatus := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/submit", teamID, buildID), nil, token)
	if submitStatus != http.StatusOK {
		t.Fatalf("expected 200 on submit, got %d: %v", submitStatus, submitBody)
	}
	if submitBody["status"] != "pending" {
		t.Fatalf("expected submit to report status=pending, got %v", submitBody["status"])
	}

	// No allocations persisted for a pending build.
	var allocCount int
	testDB.QueryRow(context.Background(),
		`SELECT count(*) FROM build_part_allocations WHERE build_id = $1`, buildID,
	).Scan(&allocCount) //nolint:errcheck
	if allocCount != 0 {
		t.Errorf("expected 0 allocations for pending build, got %d", allocCount)
	}

	// Shortfall of 1 (need 4, free 3) reaches the cart.
	cart, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/cart", teamID), nil, token)
	if items, _ := cart["items"].([]any); len(items) == 0 {
		t.Fatal("expected a shortfall cart item for partially-covered build")
	}
}

func TestGetBuild(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-get@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Bld-PCB-D")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")

	// Create draft build via HTTP.
	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{
			"project_id":      projectID,
			"bom_revision_id": revID,
			"quantity":        1,
		}, token)
	if status != http.StatusCreated {
		t.Fatalf("create expected 201, got %d: %v", status, created)
	}
	buildID := created["id"].(string)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["id"] != buildID {
		t.Errorf("expected id=%s, got %v", buildID, body["id"])
	}
	if body["quantity"].(float64) != 1 {
		t.Errorf("expected quantity=1, got %v", body["quantity"])
	}
	if body["status"].(string) != "draft" {
		t.Errorf("expected status=draft, got %v", body["status"])
	}
}

// TestPatchSubstituteSelection_RevertToPrimary verifies that a null part_id
// removes a line item's stored substitute selection (reverting to the primary),
// and that selecting a non-approved part is rejected with 400.
func TestPatchSubstituteSelection_RevertToPrimary(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-subrevert@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Revert-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")

	// Primary part with no stock; substitute part with plenty → the selection
	// job will auto-pick the substitute.
	primaryID := testutil.CreatePart(t, testDB, "global", "SUBREV-PRIMARY", nil)
	subID := testutil.CreatePart(t, testDB, "global", "SUBREV-SUB", nil)
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 1)
	testutil.ResolveBOMLineItem(t, testDB, itemID, primaryID)

	subEntry := testutil.CreateInventoryEntry(t, testDB, teamID, subID)
	testutil.SetStockQty(t, testDB, subEntry, 10)

	// Approve the substitute via the API.
	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/substitutes", teamID, projectID, revID, itemID),
		map[string]any{"part_id": subID}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201 adding substitute, got %d", status)
	}

	// Create the build → selection job picks the substitute (primary is short).
	created, st := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{"project_id": projectID, "bom_revision_id": revID, "quantity": 1}, token)
	if st != http.StatusCreated {
		t.Fatalf("expected 201 creating build, got %d: %v", st, created)
	}
	buildID := created["id"].(string)

	testutil.WaitForBuildSelections(t, testEnv.Server, teamID, buildID, token)

	build, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
	if sels, _ := build["substitute_selections"].([]any); len(sels) != 1 {
		t.Fatalf("expected substitute auto-selected, got %d selections", len(sels))
	}

	// A non-approved part must be rejected.
	bogusID := testutil.CreatePart(t, testDB, "global", "SUBREV-BOGUS", nil)
	_, badStatus := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/substitute-selections", teamID, buildID),
		map[string]any{"selections": []map[string]any{{"bom_line_item_id": itemID, "part_id": bogusID}}}, token)
	if badStatus != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-approved part, got %d", badStatus)
	}

	// Reverting with a null part_id clears the selection.
	resp, revertStatus := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s/substitute-selections", teamID, buildID),
		map[string]any{"selections": []map[string]any{{"bom_line_item_id": itemID, "part_id": nil}}}, token)
	if revertStatus != http.StatusOK {
		t.Fatalf("expected 200 on revert, got %d: %v", revertStatus, resp)
	}
	if respSels, _ := resp["selections"].([]any); len(respSels) != 0 {
		t.Fatalf("expected no selections in revert response, got %d", len(respSels))
	}

	build2, _ := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
	if sels2, _ := build2["substitute_selections"].([]any); len(sels2) != 0 {
		t.Errorf("expected GetBuild to show no selections after revert, got %d", len(sels2))
	}
}

// Tier 4 (kitting hand-off): GetBuild surfaces the latest non-abandoned kitting
// session so the desktop can restore the kitting panel after reload and hand it off
// to checkout. A build that was never kitted reports kitting_session = null.
func TestGetBuild_ReportsLatestKittingSession(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	ctx := context.Background()

	userID := testutil.CreateUser(t, testDB, "kit-handoff@example.com", "pw", "Kit")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Kit Team")
	token := testutil.CreateSession(t, testDB, userID)
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Kit-PCB")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	buildID := testutil.CreateBuild(t, testDB, projectID, revID, teamID, userID, 5, "ready")

	// No kitting yet → kitting_session is null.
	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("get build: expected 200, got %d: %v", status, body)
	}
	if body["kitting_session"] != nil {
		t.Errorf("un-kitted build should report kitting_session=null, got %v", body["kitting_session"])
	}

	// Seed a completed kitting session for the build.
	testutil.CreateScanSession(t, testDB, userID, teamID)
	var ksID string
	if err := testDB.QueryRow(ctx,
		`INSERT INTO build_kitting_sessions (scan_session_id, build_id, team_id, status, completed_at)
		 VALUES ((SELECT id FROM scan_sessions WHERE user_id = $1 AND team_id = $2 LIMIT 1),
		         $3, $4, 'completed', now())
		 RETURNING id`,
		userID, teamID, buildID, teamID).Scan(&ksID); err != nil {
		t.Fatalf("seed kitting session: %v", err)
	}

	body, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/builds/%s", teamID, buildID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("get build (kitted): expected 200, got %d: %v", status, body)
	}
	ks, ok := body["kitting_session"].(map[string]any)
	if !ok {
		t.Fatalf("expected kitting_session object, got %T: %v", body["kitting_session"], body["kitting_session"])
	}
	if ks["id"] != ksID {
		t.Errorf("kitting_session.id = %v, want %s", ks["id"], ksID)
	}
	if ks["status"] != "completed" {
		t.Errorf("kitting_session.status = %v, want completed", ks["status"])
	}
}
