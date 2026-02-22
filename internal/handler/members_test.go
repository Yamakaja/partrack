package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListMembers_AdminCanList(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "membadmin@example.com", "pw", "Admin")
	viewerID := testutil.CreateUser(t, testDB, "membviewer@example.com", "pw", "Viewer")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Test Team")
	testutil.AddMember(t, testDB, teamID, viewerID, "viewer")
	adminToken := testutil.CreateSession(t, testDB, adminID)

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s/members", teamID), nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Errorf("expected 2 members, got %d", len(items))
	}
}

func TestListMembers_MemberForbidden(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "memblistadmin@example.com", "pw", "Admin")
	memberID := testutil.CreateUser(t, testDB, "memblistmember@example.com", "pw", "Member")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Test Team")
	testutil.AddMember(t, testDB, teamID, memberID, "member")
	memberToken := testutil.CreateSession(t, testDB, memberID)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s/members", teamID), nil, memberToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

func TestAddMember(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "addmembadmin@example.com", "pw", "Admin")
	newID := testutil.CreateUser(t, testDB, "addmembnew@example.com", "pw", "New")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Team")
	adminToken := testutil.CreateSession(t, testDB, adminID)

	body, status := testutil.Do(t, testEnv.Server,
		http.MethodPost, fmt.Sprintf("/api/v1/teams/%s/members", teamID),
		map[string]any{"user_id": newID, "role": "viewer"}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["role"] != "viewer" {
		t.Errorf("expected role=viewer, got %v", body["role"])
	}
}

func TestAddMember_NonAdminForbidden(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "addmembadmin2@example.com", "pw", "Admin")
	memberID := testutil.CreateUser(t, testDB, "addmembmember@example.com", "pw", "Member")
	newID := testutil.CreateUser(t, testDB, "addmembnew2@example.com", "pw", "New")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Team")
	testutil.AddMember(t, testDB, teamID, memberID, "member")
	memberToken := testutil.CreateSession(t, testDB, memberID)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodPost, fmt.Sprintf("/api/v1/teams/%s/members", teamID),
		map[string]any{"user_id": newID, "role": "viewer"}, memberToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
}

// TestUpdateMemberRole_CacheEviction verifies that after a role upgrade the
// previously cached (lower) role is no longer used. The test promotes a viewer
// to member and then hits a member-only endpoint that would have returned 403
// with the stale cached role.
func TestUpdateMemberRole_CacheEviction(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "rolecache-admin@example.com", "pw", "Admin")
	viewerID := testutil.CreateUser(t, testDB, "rolecache-viewer@example.com", "pw", "Viewer")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Team")
	testutil.AddMember(t, testDB, teamID, viewerID, "viewer")
	adminToken := testutil.CreateSession(t, testDB, adminID)
	viewerToken := testutil.CreateSession(t, testDB, viewerID)

	// Warm the cache: viewer hits a viewer-accessible endpoint.
	testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s", teamID), nil, viewerToken)

	// Admin promotes viewer to member.
	_, status := testutil.Do(t, testEnv.Server,
		http.MethodPatch, fmt.Sprintf("/api/v1/teams/%s/members/%s", teamID, viewerID),
		map[string]any{"role": "member"}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200 on role update, got %d", status)
	}

	// Viewer (now member) hits a member-only endpoint — must succeed.
	// POST /inventory requires member role.
	partID := testutil.CreatePart(t, testDB, "global", "TEST-PART", nil)
	_, status = testutil.Do(t, testEnv.Server,
		http.MethodPost, fmt.Sprintf("/api/v1/teams/%s/inventory", teamID),
		map[string]any{"part_id": partID}, viewerToken)
	if status == http.StatusForbidden {
		t.Fatal("role promotion was not reflected: still getting 403 (stale cache not evicted)")
	}
	// Accept 201 (success) or any non-403 non-401 status.
	if status == http.StatusUnauthorized {
		t.Fatalf("unexpected 401 after role promotion")
	}
}

func TestRemoveMember(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	adminID := testutil.CreateUser(t, testDB, "remmembadmin@example.com", "pw", "Admin")
	memberID := testutil.CreateUser(t, testDB, "remmembmember@example.com", "pw", "Member")
	teamID := testutil.CreateTeam(t, testDB, adminID, "admin", "Team")
	testutil.AddMember(t, testDB, teamID, memberID, "viewer")
	adminToken := testutil.CreateSession(t, testDB, adminID)
	memberToken := testutil.CreateSession(t, testDB, memberID)

	_, status := testutil.Do(t, testEnv.Server,
		http.MethodDelete, fmt.Sprintf("/api/v1/teams/%s/members/%s", teamID, memberID),
		nil, adminToken)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// Removed member must lose access to the team.
	_, status = testutil.Do(t, testEnv.Server,
		http.MethodGet, fmt.Sprintf("/api/v1/teams/%s", teamID), nil, memberToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 after removal, got %d", status)
	}
}
