// Package activity records a unified, append-only stream of user actions —
// the "Recent Activity" feed and the substrate for a future admin audit log.
//
// It is a *complement* to domain ledgers (e.g. stock_adjustments): a row is
// written only when no domain source-of-truth already captures the change with
// actor + timestamp. Rows are self-contained for display (snapshot labels) and
// carry a structured payload the frontend renders via a declarative template.
//
// Two write lanes:
//   - Record:     buffered + batch-inserted (feed/bulk traffic; a few seconds'
//     delay is acceptable, BOM imports can burst).
//   - RecordSync: a single synchronous insert that returns the new row id — for
//     security/admin events that must not be lost, and for scan.part_not_found
//     which needs its id echoed back to the phone for later resolution.
package activity

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PublishFunc delivers the live-feed SSE event. Injected so this package does not
// depend on the worker package (which imports this one for job-side recording).
// targetUserID empty ⇒ broadcast to the team; otherwise deliver only to that user.
type PublishFunc func(eventType, teamID, targetUserID string, data []byte)

// Feed is the visibility tier of an activity row.
type Feed string

const (
	FeedTeam     Feed = "team"     // broadcast to the team feed (purchaser+ toggle)
	FeedPersonal Feed = "personal" // the acting user's own feed only
	FeedAudit    Feed = "audit"    // recorded, shown in no feed (admin/security)
)

// Source is the channel an action came through.
type Source string

const (
	SourceWeb     Source = "web"
	SourceScanner Source = "scanner"
	SourceJob     Source = "job"
	SourceSystem  Source = "system"
)

// Ref is a typed reference to an entity, with a snapshot label captured at write
// time (durable, audit-correct). The frontend renders label as link text and the
// type+id as the navigation target.
type Ref struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Label  string `json:"label"`
	Parent *Ref   `json:"parent,omitempty"` // optional context, e.g. a revision's project
}

// Event is a single activity to record. TeamID/ActorID/Source/Action/Feed are
// required; the rest are optional and shape the rendered sentence.
type Event struct {
	TeamID     pgtype.UUID
	ActorID    pgtype.UUID
	ActorLabel string
	Source     Source
	Action     string // verb.noun, e.g. "part.created"
	Feed       Feed
	Subject    *Ref
	Refs       map[string]Ref
	Values     map[string]any
	Diff       map[string][2]any
	Summary    string    // plain-text fallback / audit export
	At         time.Time // event time; zero ⇒ now
}

const (
	batchMaxRows  = 100
	batchInterval = 2 * time.Second
	batchBuffer   = 1024
)

// Recorder owns the batcher goroutine and the DB/SSE handles.
type Recorder struct {
	db      *pgxpool.Pool
	queries *dbgen.Queries
	pub     PublishFunc
	ch      chan dbgen.InsertActivityRowsParams
	done    chan struct{}
	wg      sync.WaitGroup
}

// New constructs a Recorder. Call Start to launch the batcher.
func New(db *pgxpool.Pool, pub PublishFunc) *Recorder {
	return &Recorder{
		db:      db,
		queries: dbgen.New(db),
		pub:     pub,
		ch:      make(chan dbgen.InsertActivityRowsParams, batchBuffer),
		done:    make(chan struct{}),
	}
}

// Start launches the batch-flush goroutine. Stop (via ctx cancel) drains it.
func (r *Recorder) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.run(ctx)
}

// Stop signals the batcher to drain and waits for it.
func (r *Recorder) Stop() {
	close(r.done)
	r.wg.Wait()
}

func (r *Recorder) run(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()
	buf := make([]dbgen.InsertActivityRowsParams, 0, batchMaxRows)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		if _, err := r.queries.InsertActivityRows(context.Background(), buf); err != nil {
			// CopyFrom is all-or-nothing, so a single unusable row — most
			// plausibly one whose team was deleted while it sat in this buffer —
			// would otherwise discard everyone else's rows in the same batch.
			// Retry individually so the damage is limited to the bad row.
			slog.Warn("activity: batch insert failed, retrying rows individually",
				"rows", len(buf), "err", err)
			for _, p := range buf {
				if _, err := r.queries.CreateActivity(context.Background(), dbgen.CreateActivityParams(p)); err != nil {
					slog.Warn("activity: row insert failed", "action", p.Action, "err", err)
				}
			}
		}
		buf = buf[:0]
	}

	for {
		select {
		case p := <-r.ch:
			buf = append(buf, p)
			if len(buf) >= batchMaxRows {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-r.done:
			// Drain whatever is queued, then flush and exit.
			for {
				select {
				case p := <-r.ch:
					buf = append(buf, p)
				default:
					flush()
					return
				}
			}
		case <-ctx.Done():
			flush()
			return
		}
	}
}

// Record enqueues an event for batched insertion and publishes the live-feed SSE.
// Non-blocking: if the buffer is full it falls back to a synchronous insert so
// nothing is silently dropped.
func (r *Recorder) Record(e Event) {
	if r == nil {
		return
	}
	params := e.toBatchParams()
	select {
	case r.ch <- params:
	default:
		if _, err := r.queries.CreateActivity(context.Background(), e.toCreateParams()); err != nil {
			slog.Warn("activity: overflow sync insert failed", "action", e.Action, "err", err)
		}
	}
	r.publish(e)
}

// RecordSync inserts a single row synchronously and returns its id. Used for
// security/admin events and scan.part_not_found.
func (r *Recorder) RecordSync(ctx context.Context, e Event) (pgtype.UUID, error) {
	if r == nil {
		return pgtype.UUID{}, nil
	}
	id, err := r.queries.CreateActivity(ctx, e.toCreateParams())
	if err != nil {
		slog.Warn("activity: sync insert failed", "action", e.Action, "err", err)
		return pgtype.UUID{}, err
	}
	r.publish(e)
	return id, nil
}

// publish emits the activity_changed SSE so open feeds refresh. Personal events
// target only the actor; team events broadcast; audit events emit nothing.
func (r *Recorder) publish(e Event) {
	if r.pub == nil || e.Feed == FeedAudit || !e.TeamID.Valid {
		return
	}
	data, _ := json.Marshal(map[string]string{"action": e.Action, "feed": string(e.Feed)})
	target := ""
	if e.Feed == FeedPersonal {
		target = e.ActorID.String()
	}
	r.pub(string("activity_changed"), e.TeamID.String(), target, data)
}

func (e Event) toCreateParams() dbgen.CreateActivityParams {
	st, sid, sl := e.subjectCols()
	return dbgen.CreateActivityParams{
		TeamID:       e.TeamID,
		ActorUserID:  e.ActorID,
		ActorLabel:   ptr(e.ActorLabel),
		Source:       string(e.Source),
		Action:       e.Action,
		Feed:         string(e.Feed),
		SubjectType:  st,
		SubjectID:    sid,
		SubjectLabel: sl,
		Summary:      ptr(e.Summary),
		Payload:      e.payloadJSON(),
		CreatedAt:    e.createdAt(),
	}
}

func (e Event) toBatchParams() dbgen.InsertActivityRowsParams {
	p := e.toCreateParams()
	return dbgen.InsertActivityRowsParams(p)
}

func (e Event) createdAt() pgtype.Timestamptz {
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	var ts pgtype.Timestamptz
	_ = ts.Scan(at)
	return ts
}

func (e Event) subjectCols() (*string, pgtype.UUID, *string) {
	if e.Subject == nil {
		return nil, pgtype.UUID{}, nil
	}
	var id pgtype.UUID
	_ = id.Scan(e.Subject.ID)
	return ptr(e.Subject.Type), id, ptr(e.Subject.Label)
}

// payloadJSON marshals the non-empty structured parts into the JSONB column.
func (e Event) payloadJSON() []byte {
	m := map[string]any{}
	if len(e.Refs) > 0 {
		m["refs"] = e.Refs
	}
	if len(e.Values) > 0 {
		m["values"] = e.Values
	}
	if len(e.Diff) > 0 {
		m["diff"] = e.Diff
	}
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

// ptr returns a pointer to s, or nil if s is empty.
func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
