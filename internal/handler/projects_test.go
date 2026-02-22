package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListProjects_Empty(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "proj-list@example.com", "pw", "Proj")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Proj Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
	if body["total"].(float64) != 0 {
		t.Errorf("expected total=0, got %v", body["total"])
	}
}

func TestCreateProject(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "proj-create@example.com", "pw", "Proj")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Proj Team")
	token := testutil.CreateSession(t, testDB, userID)

	desc := "Power supply board"
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects", teamID),
		map[string]any{"name": "PSU Board", "description": desc}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["name"] != "PSU Board" {
		t.Errorf("expected name=PSU Board, got %v", body["name"])
	}
	if body["status"] != "active" {
		t.Errorf("expected status=active, got %v", body["status"])
	}
	if body["id"] == "" {
		t.Error("expected non-empty id")
	}
}

func TestCreateProject_MissingName(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "proj-noname@example.com", "pw", "Proj")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Proj Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/projects", teamID),
		map[string]any{"description": "no name"}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestGetProject(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "proj-get@example.com", "pw", "Proj")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Proj Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Radar Board")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s", teamID, projectID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["name"] != "Radar Board" {
		t.Errorf("expected name=Radar Board, got %v", body["name"])
	}
	if body["id"] != projectID {
		t.Errorf("expected id=%s, got %v", projectID, body["id"])
	}
}

func TestGetProject_NotFound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "proj-404@example.com", "pw", "Proj")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Proj Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/projects/00000000-0000-0000-0000-000000000000", teamID), nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestUpdateProject_Name(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "proj-update@example.com", "pw", "Proj")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Proj Team")
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Old Name")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/projects/%s", teamID, projectID),
		map[string]any{"name": "New Name"}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["name"] != "New Name" {
		t.Errorf("expected name=New Name, got %v", body["name"])
	}
}
