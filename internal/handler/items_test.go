package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestUpdateBOMItem(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "item-update@example.com", "pw", "Items")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Items Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-Items")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "U1", 1)
	partID := testutil.CreatePart(t, testDB, "global", "LM7805", nil)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s",
			teamID, projectID, revID, itemID),
		map[string]any{"resolved_part_id": partID}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["resolved_part_id"] != partID {
		t.Errorf("expected resolved_part_id=%s, got %v", partID, body["resolved_part_id"])
	}
}

func TestUpdateBOMItem_NotFound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "item-404@example.com", "pw", "Items")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Items Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-X")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/00000000-0000-0000-0000-000000000000",
			teamID, projectID, revID),
		map[string]any{"resolved_part_id": nil}, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestAddSubstitute(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sub-add@example.com", "pw", "Sub")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Sub Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-Sub")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "U2", 1)
	subPartID := testutil.CreatePart(t, testDB, "global", "LM317T", nil)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/substitutes",
			teamID, projectID, revID, itemID),
		map[string]any{"part_id": subPartID}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["part_id"] != subPartID {
		t.Errorf("expected part_id=%s, got %v", subPartID, body["part_id"])
	}
}

func TestAddSubstitute_Duplicate(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sub-dup@example.com", "pw", "Sub")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Sub Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-Dup")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "U3", 1)
	subPartID := testutil.CreatePart(t, testDB, "global", "NE555-DUP", nil)
	token := testutil.CreateSession(t, testDB, userID)

	path := fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/substitutes",
		teamID, projectID, revID, itemID)
	payload := map[string]any{"part_id": subPartID}

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, path, payload, token)
	if status != http.StatusCreated {
		t.Fatalf("first add expected 201, got %d", status)
	}

	_, status = testutil.Do(t, testEnv.Server, http.MethodPost, path, payload, token)
	if status != http.StatusConflict {
		t.Fatalf("duplicate add expected 409, got %d", status)
	}
}

func TestRemoveSubstitute(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "sub-rm@example.com", "pw", "Sub")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Sub Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-Rm")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1")
	itemID := testutil.CreateBOMLineItem(t, testDB, revID, "U4", 1)
	subPartID := testutil.CreatePart(t, testDB, "global", "BC547-RM", nil)
	token := testutil.CreateSession(t, testDB, userID)

	// Add first.
	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/substitutes",
			teamID, projectID, revID, itemID),
		map[string]any{"part_id": subPartID}, token)
	if status != http.StatusCreated {
		t.Fatalf("add expected 201, got %d", status)
	}

	// Remove.
	_, status = testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s/items/%s/substitutes/%s",
			teamID, projectID, revID, itemID, subPartID),
		nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("remove expected 204, got %d", status)
	}
}
