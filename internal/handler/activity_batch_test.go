package handler_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	"github.com/Yamakaja/partrack/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
)

// Activity rows are batch-inserted with CopyFrom, which is all-or-nothing. A row
// whose team no longer exists — deleted while the row sat in the buffer — must
// not take everyone else's rows down with it.
func TestActivityBatch_OneBadRowDoesNotDiscardTheBatch(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "act-batch@example.com", "pw", "Batch")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "Batch Team")
	token := testutil.CreateSession(t, testDB, userID)

	var goodTeam, ghostTeam, actor pgtype.UUID
	if err := goodTeam.Scan(teamID); err != nil {
		t.Fatalf("scan team id: %v", err)
	}
	if err := actor.Scan(userID); err != nil {
		t.Fatalf("scan user id: %v", err)
	}
	// A team that does not exist: violates activity_log_team_id_fkey.
	if err := ghostTeam.Scan("00000000-0000-0000-0000-0000000000ff"); err != nil {
		t.Fatalf("scan ghost id: %v", err)
	}

	rec := testEnv.Handler.Activity
	rec.Record(activity.Event{
		TeamID: ghostTeam, ActorID: actor, Source: activity.SourceWeb,
		Action: "project.created", Feed: activity.FeedTeam,
		Subject: &activity.Ref{Type: "project", ID: "11111111-1111-1111-1111-111111111111", Label: "Ghost"},
	})
	rec.Record(activity.Event{
		TeamID: goodTeam, ActorID: actor, Source: activity.SourceWeb,
		Action: "project.created", Feed: activity.FeedTeam,
		Subject: &activity.Ref{Type: "project", ID: "22222222-2222-2222-2222-222222222222", Label: "Survivor"},
	})

	// The batcher flushes on a timer; poll for the surviving row.
	deadline := time.Now().Add(15 * time.Second)
	for {
		body, status := testutil.Do(t, testEnv.Server, http.MethodGet,
			fmt.Sprintf("/api/v1/teams/%s/activity-log?scope=team", teamID), nil, token)
		if status == http.StatusOK {
			if items, _ := body["items"].([]any); len(items) > 0 {
				row, _ := items[0].(map[string]any)
				subject, _ := row["subject"].(map[string]any)
				if subject["label"] != "Survivor" {
					t.Fatalf("unexpected surviving row: %v", row)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the valid row was discarded along with the unusable one")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
