package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestCreateTeam(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "teamcreate@example.com", "pw", "Creator")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/teams",
		map[string]any{"name": "Avengers"}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["name"] != "Avengers" {
		t.Errorf("expected name=Avengers, got %v", body["name"])
	}

	// Creator must appear as admin of the new team.
	meBody, _ := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/auth/me", nil, token)
	memberships, _ := meBody["memberships"].([]any)
	found := false
	for _, m := range memberships {
		mm := m.(map[string]any)
		if mm["team_id"] == body["id"] && mm["role"] == "admin" {
			found = true
		}
	}
	if !found {
		t.Error("creator should be admin of the new team")
	}
}

func TestCreateTeam_MissingName(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "teamnoname@example.com", "pw", "NoName")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost, "/api/v1/teams",
		map[string]any{}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestListTeams_OnlyMemberTeams(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	u1 := testutil.CreateUser(t, testDB, "teamlist-u1@example.com", "pw", "U1")
	u2 := testutil.CreateUser(t, testDB, "teamlist-u2@example.com", "pw", "U2")
	tok1 := testutil.CreateSession(t, testDB, u1)
	tok2 := testutil.CreateSession(t, testDB, u2)

	// u1 is in team A; u2 is in team B.
	testutil.CreateTeam(t, testDB, u1, "admin", "Team A")
	testutil.CreateTeam(t, testDB, u2, "admin", "Team B")

	body1, _ := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/teams", nil, tok1)
	body2, _ := testutil.Do(t, testEnv.Server, http.MethodGet, "/api/v1/teams", nil, tok2)

	items1, _ := body1["items"].([]any)
	items2, _ := body2["items"].([]any)

	if len(items1) != 1 {
		t.Errorf("u1 should see 1 team, got %d", len(items1))
	}
	if len(items2) != 1 {
		t.Errorf("u2 should see 1 team, got %d", len(items2))
	}
	if items1[0].(map[string]any)["name"] != "Team A" {
		t.Errorf("u1 should see Team A, got %v", items1[0].(map[string]any)["name"])
	}
}

func TestGetTeam_Member(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "teamget@example.com", "pw", "GetTeam")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Get Team")

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["id"] != teamID {
		t.Errorf("wrong team ID in response: %v", body["id"])
	}
}

func TestGetTeam_NonMember(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	ownerID := testutil.CreateUser(t, testDB, "teamowner@example.com", "pw", "Owner")
	outsiderID := testutil.CreateUser(t, testDB, "teamoutsider@example.com", "pw", "Outsider")
	outsiderToken := testutil.CreateSession(t, testDB, outsiderID)
	teamID := testutil.CreateTeam(t, testDB, ownerID, "admin", "Private Team")

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s", teamID), nil, outsiderToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

func TestUpdateTeam_AdminAllowed(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "teamupdate@example.com", "pw", "UpdateAdmin")
	token := testutil.CreateSession(t, testDB, userID)
	teamID := testutil.CreateTeam(t, testDB, userID, "admin", "Old Name")

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodPatch, fmt.Sprintf("/api/v1/teams/%s", teamID),
		map[string]any{"name": "New Name"}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["name"] != "New Name" {
		t.Errorf("expected name=New Name, got %v", body["name"])
	}
}

func TestUpdateTeam_MemberForbidden(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	ownerID := testutil.CreateUser(t, testDB, "teamupdateowner@example.com", "pw", "Owner")
	memberID := testutil.CreateUser(t, testDB, "teamupdatemember@example.com", "pw", "Member")
	teamID := testutil.CreateTeam(t, testDB, ownerID, "admin", "Stable Team")
	testutil.AddMember(t, testDB, teamID, memberID, "member")
	memberToken := testutil.CreateSession(t, testDB, memberID)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodPatch, fmt.Sprintf("/api/v1/teams/%s", teamID),
		map[string]any{"name": "Hack"}, memberToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}
