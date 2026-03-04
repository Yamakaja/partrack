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
