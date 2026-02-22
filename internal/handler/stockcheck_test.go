package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestStockCheck_MissingBuildQty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-nobuildqty@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-A")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/stock-check", teamID, projectID), nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestStockCheck_NoActiveBOM(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-nobom@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-B")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/stock-check?build_qty=1", teamID, projectID), nil, token)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", status)
	}
}

func TestStockCheck_OkAndShortfall(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-full@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Set up project + revision with two items.
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-Full")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	testutil.SetActiveBOM(t, testDB, projectID, revID)

	partA := testutil.CreatePart(t, testDB, "global", "SC-PART-A", nil) // sufficient stock
	partB := testutil.CreatePart(t, testDB, "global", "SC-PART-B", nil) // no stock (shortfall)

	itemA := testutil.CreateBOMLineItem(t, testDB, revID, "R1", 1)
	itemB := testutil.CreateBOMLineItem(t, testDB, revID, "C1", 1)
	testutil.ResolveBOMLineItem(t, testDB, itemA, partA)
	testutil.ResolveBOMLineItem(t, testDB, itemB, partB)

	// Part A: stock=10 (ok for build_qty=1, requires 1).
	entryA := testutil.CreateInventoryEntry(t, testDB, teamID, partA)
	testutil.SetStockQty(t, testDB, entryA, 10)
	// Part B: no inventory entry → shortfall.

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/stock-check?build_qty=1", teamID, projectID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}

	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Index by reference_designators.
	statusByRef := make(map[string]string)
	for _, it := range items {
		m := it.(map[string]any)
		statusByRef[m["reference_designators"].(string)] = m["status"].(string)
	}
	if statusByRef["R1"] != "ok" {
		t.Errorf("expected R1=ok, got %s", statusByRef["R1"])
	}
	if statusByRef["C1"] != "shortfall" {
		t.Errorf("expected C1=shortfall, got %s", statusByRef["C1"])
	}
}

func TestStockCheck_Unresolved(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sc-unresolved@example.com", "pw", "SC")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "SC Team")
	token := testutil.CreateSession(t, testDB, userID)

	projectID := testutil.CreateProject(t, testDB, teamID, userID, "SC-Unresolved")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	testutil.SetActiveBOM(t, testDB, projectID, revID)
	testutil.CreateBOMLineItem(t, testDB, revID, "U1", 1) // not resolved

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/stock-check?build_qty=1", teamID, projectID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0].(map[string]any)
	if it["status"] != "unresolved" {
		t.Errorf("expected status=unresolved, got %v", it["status"])
	}
}
