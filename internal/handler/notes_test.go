package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// note GETs the team's note for a part and returns the decoded response + status.
func getNote(t *testing.T, teamID, partID, token string) (map[string]any, int) {
	t.Helper()
	return testutil.Do(t, testEnv.Server, http.MethodGet,
		"/api/v1/teams/"+teamID+"/parts/"+partID+"/note", nil, token)
}

func putNote(t *testing.T, teamID, partID, token, body string) (map[string]any, int) {
	t.Helper()
	return testutil.Do(t, testEnv.Server, http.MethodPut,
		"/api/v1/teams/"+teamID+"/parts/"+partID+"/note", map[string]any{"body": body}, token)
}

func TestPartNoteEmptyThenSave(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "note@test.local", "pw", "Note User")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Note Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "GENERIC-001", nil)

	// No note yet → 200 with empty body (not 404).
	got, status := getNote(t, teamID, partID, token)
	if status != http.StatusOK {
		t.Fatalf("GET empty note status = %d, want 200 (%v)", status, got)
	}
	if got["body"] != "" {
		t.Errorf("empty note body = %q, want \"\"", got["body"])
	}

	// Save, then read it back.
	const body = "## Notes\n\nUse the 3V3 rail."
	saved, status := putNote(t, teamID, partID, token, body)
	if status != http.StatusOK {
		t.Fatalf("PUT note status = %d, want 200 (%v)", status, saved)
	}
	if saved["body"] != body {
		t.Errorf("saved body = %q, want %q", saved["body"], body)
	}
	if saved["updated_at"] == nil {
		t.Errorf("expected updated_at on save")
	}

	reread, _ := getNote(t, teamID, partID, token)
	if reread["body"] != body {
		t.Errorf("reread body = %q, want %q", reread["body"], body)
	}
}

func TestPartNoteManifestResolves(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "note2@test.local", "pw", "Note User 2")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Note Team 2")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "STM32F407VGT6", nil)
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Power Supply")

	// Upload an attachment to embed.
	resp := uploadAttachment(t, teamID, token, makePNG(t, 4, 5))
	var up map[string]any
	decodeJSON(t, resp, &up)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", resp.StatusCode)
	}
	attID, _ := up["id"].(string)

	// A body referencing a part chip, a project chip, and the uploaded image.
	body := fmt.Sprintf(
		"MCU :part[STM32F407VGT6]{#%s} used in :project[Power Supply]{#%s}\n\n![cap](attachment:%s)\n",
		partID, projectID, attID,
	)
	saved, status := putNote(t, teamID, partID, token, body)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200 (%v)", status, saved)
	}

	manifest, ok := saved["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("manifest missing/!object: %v", saved["manifest"])
	}
	objects, _ := manifest["objects"].(map[string]any)

	// Part reference resolves to a normalized object entry.
	pEntry, ok := objects[partID].(map[string]any)
	if !ok {
		t.Fatalf("part %s not in objects: %v", partID, objects)
	}
	if pEntry["kind"] != "part" || pEntry["label"] != "STM32F407VGT6" {
		t.Errorf("part entry = %v, want kind=part label=STM32F407VGT6", pEntry)
	}
	if href, _ := pEntry["href"].(string); !strings.Contains(href, "/parts/"+partID) {
		t.Errorf("part href = %v, want .../parts/%s", pEntry["href"], partID)
	}

	// Project reference resolves too.
	prEntry, ok := objects[projectID].(map[string]any)
	if !ok {
		t.Fatalf("project %s not in objects: %v", projectID, objects)
	}
	if prEntry["kind"] != "project" || prEntry["label"] != "Power Supply" {
		t.Errorf("project entry = %v, want kind=project label='Power Supply'", prEntry)
	}
	if href, _ := prEntry["href"].(string); !strings.Contains(href, "/projects/"+projectID) {
		t.Errorf("project href = %v, want .../projects/%s", prEntry["href"], projectID)
	}

	// Attachment reference resolves to a blob URL.
	atts, _ := manifest["attachments"].(map[string]any)
	aEntry, ok := atts[attID].(map[string]any)
	if !ok {
		t.Fatalf("attachment %s not in manifest: %v", attID, atts)
	}
	if url, _ := aEntry["url"].(string); !strings.HasPrefix(url, "/api/v1/blobs/") {
		t.Errorf("manifest attachment url = %v, want /api/v1/blobs/ prefix", aEntry["url"])
	}
}

func TestProjectNoteEmptyThenSave(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "pnote@test.local", "pw", "PN User")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "PN Team")
	token := testutil.CreateSession(t, testDB, userID)
	projectID := testutil.CreateProject(t, testDB, teamID, userID, "Widget")

	path := "/api/v1/teams/" + teamID + "/projects/" + projectID + "/note"

	got, status := testutil.Do(t, testEnv.Server, http.MethodGet, path, nil, token)
	if status != http.StatusOK {
		t.Fatalf("GET project note status = %d, want 200 (%v)", status, got)
	}
	if got["body"] != "" {
		t.Errorf("empty project note body = %q, want \"\"", got["body"])
	}

	const body = "Schematic on the shared drive."
	saved, status := testutil.Do(t, testEnv.Server, http.MethodPut, path, map[string]any{"body": body}, token)
	if status != http.StatusOK {
		t.Fatalf("PUT project note status = %d, want 200 (%v)", status, saved)
	}
	if saved["body"] != body {
		t.Errorf("saved project note body = %q, want %q", saved["body"], body)
	}

	reread, _ := testutil.Do(t, testEnv.Server, http.MethodGet, path, nil, token)
	if reread["body"] != body {
		t.Errorf("reread project note body = %q, want %q", reread["body"], body)
	}
}

func TestPartFlagSetAndReflected(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "flag@test.local", "pw", "Flag User")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "Flag Team")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "GENERIC-FLAG", nil)

	flagPath := "/api/v1/teams/" + teamID + "/parts/" + partID + "/flag"

	// Default flag is 'none' on a fresh note.
	note, _ := getNote(t, teamID, partID, token)
	if note["flag"] != "none" {
		t.Errorf("default flag = %v, want none", note["flag"])
	}

	// Set banned.
	res, status := testutil.Do(t, testEnv.Server, http.MethodPut, flagPath, map[string]any{"flag": "banned"}, token)
	if status != http.StatusOK {
		t.Fatalf("PUT flag status = %d, want 200 (%v)", status, res)
	}
	if res["flag"] != "banned" {
		t.Errorf("set flag = %v, want banned", res["flag"])
	}

	// Reflected on the note GET, without clobbering the (empty) body.
	note2, _ := getNote(t, teamID, partID, token)
	if note2["flag"] != "banned" {
		t.Errorf("note flag after set = %v, want banned", note2["flag"])
	}

	// Invalid flag rejected.
	_, status = testutil.Do(t, testEnv.Server, http.MethodPut, flagPath, map[string]any{"flag": "bogus"}, token)
	if status != http.StatusBadRequest {
		t.Errorf("invalid flag status = %d, want 400", status)
	}
}

func TestPartNotePutRequiresMember(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "note3@test.local", "pw", "Note User 3")
	teamID := testutil.CreateTeam(t, testDB, userID, "viewer", "Note Team 3")
	token := testutil.CreateSession(t, testDB, userID)
	partID := testutil.CreatePart(t, testDB, "global", "GENERIC-002", nil)

	// Viewer can read.
	if _, status := getNote(t, teamID, partID, token); status != http.StatusOK {
		t.Fatalf("viewer GET status = %d, want 200", status)
	}
	// But not write.
	if _, status := putNote(t, teamID, partID, token, "nope"); status != http.StatusForbidden {
		t.Errorf("viewer PUT status = %d, want 403", status)
	}
}
