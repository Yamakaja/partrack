package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListRevisions_Empty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "rev-list@example.com", "pw", "Rev")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Rev Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-A")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestImportRevision(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "rev-import@example.com", "pw", "Rev")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Rev Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-B")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{
			"label":      "v1.0",
			"set_active": true,
			"items": []any{
				map[string]any{"reference_designators": "R1,R2", "quantity": 2},
				map[string]any{"reference_designators": "C1", "quantity": 1, "raw_description": "100nF"},
			},
		}, token)
	if status != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", status, body)
	}
	if body["bom_revision_id"] == "" || body["bom_revision_id"] == nil {
		t.Error("expected non-empty bom_revision_id")
	}
	if body["job_id"] == "" || body["job_id"] == nil {
		t.Error("expected non-empty job_id")
	}
}

func TestImportRevision_MissingLabel(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "rev-nolabel@example.com", "pw", "Rev")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Rev Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-C")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions", teamID, projectID),
		map[string]any{"set_active": false}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestGetRevision(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "rev-get@example.com", "pw", "Rev")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Rev Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-D")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v2.0")
	testutil.CreateBOMLineItem(t, testDB, revID, "U1", 1)
	testutil.CreateBOMLineItem(t, testDB, revID, "R1,R2", 2)
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s", teamID, projectID, revID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["label"] != "v2.0" {
		t.Errorf("expected label=v2.0, got %v", body["label"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestUpdateRevision_Notes(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "rev-update@example.com", "pw", "Rev")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Rev Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "PCB-E")
	revID := testutil.CreateRevision(t, testDB, projectID, userID, "v1.0")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s/revisions/%s", teamID, projectID, revID),
		map[string]any{"notes": "Initial production release"}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["notes"] != "Initial production release" {
		t.Errorf("expected notes updated, got %v", body["notes"])
	}
}
