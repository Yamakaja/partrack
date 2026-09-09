package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Yamakaja/partrack/internal/testutil"
)

// Pick mode sorts a pile ON a list, ticking rows off. The verdict that Filter
// cannot express — already_checked — is the one worth pinning hardest, since it
// is the whole reason the state is durable.
func TestPickScan_Verdicts(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID := testutil.CreateUser(t, testDB, "pick-verdict@example.com", "pw", "P")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "P Team")
	scanToken := testutil.CreateScanSession(t, testDB, userID, teamID)
	desktopToken := testutil.CreateSession(t, testDB, userID)

	onList := testutil.CreatePart(t, testDB, "global", "PICK-ON", nil)
	testutil.CreatePart(t, testDB, "global", "PICK-OFF", nil) // catalog, not on the list

	body, status := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/collections", map[string]any{"name": "Bench pile"}, scanToken)
	if status != http.StatusCreated {
		t.Fatalf("create list: %d %v", status, body)
	}
	collID := body["id"].(string)

	itemsPath := fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID)
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
		map[string]any{"part_id": onList, "quantity": 20, "note": "front bin"}, desktopToken)
	if status != http.StatusCreated {
		t.Fatalf("add item: %d %v", status, body)
	}
	itemID := body["id"].(string)

	pickPath := fmt.Sprintf("/api/v1/scan/collections/%s/pick", collID)

	// First scan of a listed part ticks it.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"barcode": checkBarcode("P100001", "PICK-ON")}, scanToken)
	if status != http.StatusOK {
		t.Fatalf("pick scan: %d %v", status, body)
	}
	if got := body["status"].(string); got != "checked" {
		t.Fatalf("expected status checked, got %v (%v)", got, body)
	}
	if body["item_id"].(string) != itemID {
		t.Errorf("expected the listed row, got %v", body["item_id"])
	}
	firstCheckedAt, _ := body["checked_at"].(string)
	if firstCheckedAt == "" {
		t.Error("expected checked_at on a tick")
	}
	if q, _ := body["quantity"].(float64); q != 20 {
		t.Errorf("expected the row quantity echoed, got %v", body["quantity"])
	}
	tally := body["tally"].(map[string]any)
	if d, t2 := tally["checked_count"].(float64), tally["item_count"].(float64); d != 1 || t2 != 1 {
		t.Errorf("expected tally 1/1, got %v/%v", d, t2)
	}

	// Second bag of the same part: already ticked, and the stamp does not move.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"barcode": checkBarcode("P100001", "PICK-ON")}, scanToken)
	if got := body["status"].(string); got != "already_checked" {
		t.Fatalf("expected already_checked, got %v (%v)", got, body)
	}
	if got, _ := body["checked_at"].(string); got != firstCheckedAt {
		t.Errorf("already_checked moved checked_at: %v → %v", firstCheckedAt, got)
	}

	// A catalog part this list does not hold: no write, no tally.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"barcode": checkBarcode("P100002", "PICK-OFF")}, scanToken)
	if got := body["status"].(string); got != "not_on_list" {
		t.Fatalf("expected not_on_list, got %v (%v)", got, body)
	}
	if body["tally"] != nil {
		t.Error("not_on_list must not report a tally — nothing was written")
	}

	// Picking never grows the list (that is Collect's job).
	body, _ = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/teams/%s/collections/%s", teamID, collID), nil, desktopToken)
	if got := len(body["items"].([]any)); got != 1 {
		t.Errorf("pick must not add rows, list now has %d", got)
	}

	// Unparseable and unknown barcodes.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"barcode": "not-a-barcode"}, scanToken)
	if got := body["status"].(string); got != "unknown" {
		t.Errorf("expected unknown for garbage, got %v", got)
	}
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"barcode": checkBarcode("P100003", "PICK-NOWHERE")}, scanToken)
	if got := body["status"].(string); got != "unknown" {
		t.Errorf("expected unknown for an uncatalogued part, got %v", got)
	} else if body["reason"].(string) != "not_in_catalog" {
		t.Errorf("expected reason not_in_catalog, got %v", body["reason"])
	}
}

func TestPickScan_ManualTickAndStartFresh(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	userID := testutil.CreateUser(t, testDB, "pick-manual@example.com", "pw", "P")
	teamID := testutil.CreateTeam(t, testDB, userID, "member", "P Team")
	scanToken := testutil.CreateScanSession(t, testDB, userID, teamID)
	desktopToken := testutil.CreateSession(t, testDB, userID)

	body, _ := testutil.Do(t, testEnv.Server, http.MethodPost,
		"/api/v1/scan/collections", map[string]any{"name": "Manual"}, scanToken)
	collID := body["id"].(string)
	itemsPath := fmt.Sprintf("/api/v1/teams/%s/collections/%s/items", teamID, collID)
	partID := testutil.CreatePart(t, testDB, "global", "PICK-MANUAL", nil)
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, itemsPath,
		map[string]any{"part_id": partID}, desktopToken)
	itemID := body["id"].(string)

	pickPath := fmt.Sprintf("/api/v1/scan/collections/%s/pick", collID)

	// Manual tick — the affordance for cut tape and house-labelled bins.
	body, status := testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"item_id": itemID, "checked": true}, scanToken)
	if status != http.StatusOK || body["status"].(string) != "checked" {
		t.Fatalf("manual tick: %d %v", status, body)
	}

	// Undo.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"item_id": itemID, "checked": false}, scanToken)
	if body["status"].(string) != "unchecked" {
		t.Errorf("expected unchecked, got %v", body["status"])
	}
	if body["checked_at"] != nil {
		t.Errorf("expected checked_at cleared, got %v", body["checked_at"])
	}

	// The phone's list view carries the selection state for the outstanding sheet.
	body, _ = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"item_id": itemID, "checked": true}, scanToken)
	body, status = testutil.Do(t, testEnv.Server, http.MethodGet,
		fmt.Sprintf("/api/v1/scan/collections/%s/items", collID), nil, scanToken)
	if status != http.StatusOK {
		t.Fatalf("phone list view: %d %v", status, body)
	}
	if it := body["items"].([]any)[0].(map[string]any); it["checked_at"] == nil {
		t.Error("expected checked_at on the phone list view")
	}

	// "Start fresh" clears the ticks a previous run left behind.
	body, status = testutil.Do(t, testEnv.Server, http.MethodPost,
		fmt.Sprintf("/api/v1/scan/collections/%s/checked", collID),
		map[string]any{"op": "none"}, scanToken)
	if status != http.StatusOK {
		t.Fatalf("start fresh: %d %v", status, body)
	}
	if n := body["checked_count"].(float64); n != 0 {
		t.Errorf("expected checked_count 0 after start fresh, got %v", n)
	}

	// A foreign list is unreachable from another user's scan token.
	otherID := testutil.CreateUser(t, testDB, "pick-manual-other@example.com", "pw", "O")
	testutil.AddMember(t, testDB, teamID, otherID, "member")
	otherScanToken := testutil.CreateScanSession(t, testDB, otherID, teamID)
	if _, status = testutil.Do(t, testEnv.Server, http.MethodPost, pickPath,
		map[string]any{"item_id": itemID, "checked": true}, otherScanToken); status != http.StatusNotFound {
		t.Errorf("pick on a foreign list: expected 404, got %d", status)
	}
}
