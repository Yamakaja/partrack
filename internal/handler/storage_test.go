package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

func TestListStorageLocations(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-list@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Create a location directly.
	testDB.Exec(t.Context(), //nolint:errcheck
		`INSERT INTO storage_locations (team_id, label) VALUES ($1, 'Shelf A')`, teamID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID), nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) < 1 {
		t.Errorf("expected at least 1 location, got %d", len(items))
	}
}

func TestCreateStorageLocation(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-create@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID),
		map[string]any{"label": "Bin 1", "description": "Top shelf"}, token)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %v", status, body)
	}
	if body["label"] != "Bin 1" {
		t.Errorf("expected label=Bin 1, got %v", body["label"])
	}
	if body["id"] == nil {
		t.Error("expected id in response")
	}
}

func TestCreateStorageLocation_MissingLabel(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-nolabel@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID),
		map[string]any{}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing label, got %d", status)
	}
}

func TestCreateStorageLocation_DuplicateLabel(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-dup@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	payload := map[string]any{"label": "Unique Bin"}
	testutil.Do(t, testEnv.Server, http.MethodPost, //nolint:errcheck
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID), payload, token)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID), payload, token)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate label, got %d", status)
	}
}

func TestCreateStorageLocation_ViewerForbidden(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	ownerID := testutil.CreateUser(t, testDB, "storage-owner@example.com", "pw", "Owner")
	viewerID := testutil.CreateUser(t, testDB, "storage-viewer@example.com", "pw", "Viewer")
	teamID := testutil.CreateTeam(t, testDB, ownerID, "admin", "Storage Team")
	testutil.AddMember(t, testDB, teamID, viewerID, "viewer")
	viewerToken := testutil.CreateSession(t, testDB, viewerID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID),
		map[string]any{"label": "Forbidden Bin"}, viewerToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d", status)
	}
}

func TestUpdateStorageLocation(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-update@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Create a location.
	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID),
		map[string]any{"label": "Old Label"}, token)
	locID, _ := created["id"].(string)

	body, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations/%s", teamID, locID),
		map[string]any{"label": "New Label"}, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", status, body)
	}
	if body["label"] != "New Label" {
		t.Errorf("expected label=New Label, got %v", body["label"])
	}
}

func TestUpdateStorageLocation_NotFound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-upd404@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodPatch,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations/00000000-0000-0000-0000-000000000000", teamID),
		map[string]any{"label": "Ghost"}, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", status)
	}
}

func TestDeleteStorageLocation(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-del@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID),
		map[string]any{"label": "To Delete"}, token)
	locID, _ := created["id"].(string)

	_, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations/%s", teamID, locID), nil, token)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
}

func TestDeleteStorageLocation_BlockedByInventoryEntry(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "storage-inuse@example.com", "pw", "Storage")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Storage Team")
	token := testutil.CreateSession(t, testDB, userID)

	// Create location via API.
	created, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations", teamID),
		map[string]any{"label": "Occupied Shelf"}, token)
	locID, _ := created["id"].(string)

	// Create a part and inventory entry referencing that location directly in DB.
	partID := testutil.CreatePart(t, testDB, "global", "INUSE-PART", nil)
	testDB.Exec(context.Background(), //nolint:errcheck
		`INSERT INTO inventory_entries (team_id, part_id, storage_location_id)
		 VALUES ($1, $2, $3)`, teamID, partID, locID)

	_, status := testutil.Do(t, testEnv.Server, http.MethodDelete,
		fmt.Sprintf("/api/v1/teams/%s/storage-locations/%s", teamID, locID), nil, token)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when location is in use, got %d", status)
	}
}
