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
