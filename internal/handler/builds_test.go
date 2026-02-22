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
		map[string]any{"bom_revision_id": revID, "boards_built": 1}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	deductions, _ := body["deductions"].([]any)
	if len(deductions) != 0 {
		t.Errorf("expected 0 deductions (no resolved items), got %d", len(deductions))
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
		map[string]any{"bom_revision_id": revID, "boards_built": 3}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	deductions, _ := body["deductions"].([]any)
	if len(deductions) != 1 {
		t.Fatalf("expected 1 deduction, got %d", len(deductions))
	}
	d := deductions[0].(map[string]any)
	// 2 per board × 3 boards = 6 deducted (negative delta).
	if d["delta"].(float64) != -6 {
		t.Errorf("expected delta=-6, got %v", d["delta"])
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

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{
			"project_id":      projectID,
			"bom_revision_id": revID,
			"boards_built":    2,
		}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["boards_built"].(float64) != 2 {
		t.Errorf("expected boards_built=2, got %v", body["boards_built"])
	}
	if body["deduction_count"].(float64) != 1 {
		t.Errorf("expected deduction_count=1, got %v", body["deduction_count"])
	}

	// Stock should be 10 - 1*2 = 8.
	var qty int32
	testDB.QueryRow(context.Background(),
		`SELECT quantity_on_hand FROM inventory_entries WHERE id = $1`, entryID,
	).Scan(&qty) //nolint:errcheck
	if qty != 8 {
		t.Errorf("expected stock=8 after build, got %d", qty)
	}
}

func TestGetBuild(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bld-get@example.com", "pw", "Bld")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Bld Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Bld-PCB-D")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")

	// Create build via HTTP.
	created, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/builds", teamID),
		map[string]any{
			"project_id":      projectID,
			"bom_revision_id": revID,
			"boards_built":    1,
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
	if body["boards_built"].(float64) != 1 {
		t.Errorf("expected boards_built=1, got %v", body["boards_built"])
	}
}
