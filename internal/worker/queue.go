// Package worker provides a channel-based background job queue and a
// per-team SSE (Server-Sent Events) hub for pushing job progress to
// connected frontend clients.
package worker

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobType identifies the kind of background work to perform.
type JobType string

const (
	JobBOMImport                 JobType = "bom_import"
	JobPartEnrich                JobType = "part_enrich"
	JobListingRefresh            JobType = "listing_refresh"
	JobOrderImport               JobType = "order_import"
	JobOrderLink                 JobType = "order_link"
	JobBuildSubstituteSelections JobType = "build_substitute_selections"
	JobRetryUnresolved           JobType = "retry_unresolved"
	JobPartResolve               JobType = "part_resolve"
)

// Job is a unit of work submitted to the Queue.
type Job struct {
	ID      string
	Type    JobType
	TeamID  string
	Payload any
}

// Event is a progress notification sent to SSE subscribers.
type Event struct {
	Type         string          `json:"event"`
	Data         json.RawMessage `json:"data"`
	JobID        string          `json:"job_id,omitempty"`
	TeamID       string          `json:"-"` // routing only; not serialised
	UserID       string          `json:"-"` // skip delivery to this user's channel; empty = send to all
	TargetUserID string          `json:"-"` // deliver ONLY to this user; takes precedence over UserID
}

// subscription pairs an SSE channel with the user who owns the connection.
type subscription struct {
	userID string
	ch     chan Event
}

// Queue is a channel-based job dispatcher with an integrated SSE hub.
// Jobs are processed by a pool of worker goroutines. Progress events are
// fanned out to all active SSE subscribers for the relevant team.
type Queue struct {
	jobs           chan Job
	subscribers    map[string][]subscription // keyed by team ID
	mu             sync.RWMutex
	db             *pgxpool.Pool
	distributors   distributor.Registry
	cartInvalidate func(teamID string)
	activity       *activity.Recorder
	quit           chan struct{}  // closed by Shutdown to signal goroutines to stop
	wg             sync.WaitGroup // tracks worker + cleanup goroutines for drain
}

// SetActivity wires the activity recorder so jobs can emit rollup events
// (e.g. bom.imported). Set once at startup, before jobs are processed.
func (q *Queue) SetActivity(rec *activity.Recorder) { q.activity = rec }

// SetCartInvalidate registers a callback that is called whenever a background
// job updates distributor listing data. The callback should invalidate any
// cached cart resolution for the given team.
func (q *Queue) SetCartInvalidate(fn func(teamID string)) {
	q.mu.Lock()
	q.cartInvalidate = fn
	q.mu.Unlock()
}

// invalidateCart calls the registered cart invalidation callback, if any.
func (q *Queue) invalidateCart(teamID string) {
	q.mu.RLock()
	fn := q.cartInvalidate
	q.mu.RUnlock()
	if fn != nil && teamID != "" {
		fn(teamID)
	}
}

// New creates a Queue and starts n worker goroutines.
func New(workers int, db *pgxpool.Pool, reg distributor.Registry) *Queue {
	q := &Queue{
		jobs:         make(chan Job, 64),
		subscribers:  make(map[string][]subscription),
		db:           db,
		distributors: reg,
		quit:         make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.run()
	}
	q.wg.Add(1)
	go q.runCleanup()
	return q
}

// Shutdown stops accepting cleanup-driven work and waits for in-flight jobs to
// finish, bounded by ctx. The jobs channel is deliberately never closed: the
// cleanup goroutine enqueues recovery jobs, so closing would risk a send-on-
// closed-channel panic. Instead workers observe quit, drain the buffer, and exit.
// Returns ctx.Err() if the deadline elapses before all goroutines stop.
func (q *Queue) Shutdown(ctx context.Context) error {
	close(q.quit)
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stalePendingCutoff is how long a resolve item must have been pending before
// the recovery sweep considers it orphaned (no running job). Must comfortably
// exceed the worst-case job duration: enrichmentTimeout (60s) × distributors + overhead.
const stalePendingCutoff = 5 * time.Minute

// runCleanup periodically deletes committed and failed part_resolve_items older
// than 30 days, and re-enqueues any pending items that have been waiting longer
// than stalePendingCutoff (orphaned by a server restart or a job panic).
func (q *Queue) runCleanup() {
	defer q.wg.Done()

	// Run recovery immediately on startup, then every 5 minutes.
	q.recoverStalePendingItems()

	cleanupTicker := time.NewTicker(24 * time.Hour)
	recoveryTicker := time.NewTicker(stalePendingCutoff)
	defer cleanupTicker.Stop()
	defer recoveryTicker.Stop()

	for {
		select {
		case <-cleanupTicker.C:
			if err := dbgen.New(q.db).DeleteOldPartResolveItems(context.Background()); err != nil {
				slog.Warn("part resolve cleanup failed", "err", err)
			}
		case <-recoveryTicker.C:
			q.recoverStalePendingItems()
		case <-q.quit:
			return
		}
	}
}

// recoverStalePendingItems finds pending resolve items that have no running job
// (older than stalePendingCutoff) and re-enqueues them.
func (q *Queue) recoverStalePendingItems() {
	ctx := context.Background()
	cutoff := pgtype.Timestamptz{}
	cutoff.Scan(time.Now().Add(-stalePendingCutoff)) //nolint:errcheck

	items, err := dbgen.New(q.db).GetStalePendingResolveItems(ctx, cutoff)
	if err != nil {
		slog.Warn("part resolve recovery: query failed", "err", err)
		return
	}
	for _, item := range items {
		slog.Info("part resolve recovery: re-enqueuing orphaned pending item", "item_id", item.ID.String())
		q.Enqueue(JobPartResolve, item.TeamID.String(), map[string]string{
			"resolve_item_id": item.ID.String(),
			"team_id":         item.TeamID.String(),
			"user_id":         item.CreatedBy.String(),
		})
	}
	if len(items) > 0 {
		slog.Info("part resolve recovery: re-enqueued orphaned items", "count", len(items))
	}
}

// newUUID returns a random UUID v4 string.
func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Enqueue submits a job and returns its ID.
func (q *Queue) Enqueue(jobType JobType, teamID string, payload any) string {
	id := newUUID()
	q.jobs <- Job{ID: id, Type: jobType, TeamID: teamID, Payload: payload}
	return id
}

// Subscribe registers a channel to receive SSE events for a team.
// userID identifies the connected user so that Publish can skip sending
// entity-change events back to the user who caused them.
// The returned cancel function must be called when the SSE connection closes.
func (q *Queue) Subscribe(teamID, userID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	sub := subscription{userID: userID, ch: ch}
	q.mu.Lock()
	q.subscribers[teamID] = append(q.subscribers[teamID], sub)
	q.mu.Unlock()

	cancel := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		subs := q.subscribers[teamID]
		for i, s := range subs {
			if s.ch == ch {
				q.subscribers[teamID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}
	return ch, cancel
}

// Publish sends an event to SSE subscribers of the event's team.
//
// Routing rules (evaluated in order):
//  1. If ev.TargetUserID is set, deliver ONLY to subscribers with that userID.
//  2. Otherwise, if ev.UserID is set, skip the subscriber with that userID
//     (the acting user's own mutations are handled by client-side cache invalidation).
//  3. Otherwise, deliver to all team subscribers.
func (q *Queue) Publish(ev Event) {
	q.mu.RLock()
	subs := q.subscribers[ev.TeamID]
	q.mu.RUnlock()
	for _, sub := range subs {
		if ev.TargetUserID != "" {
			if sub.userID != ev.TargetUserID {
				continue
			}
		} else if ev.UserID != "" && sub.userID == ev.UserID {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			// drop if subscriber is too slow
		}
	}
}

// run is the worker goroutine loop. On quit it drains any buffered jobs and
// exits, so Shutdown can wait for genuinely in-flight work to settle.
func (q *Queue) run() {
	defer q.wg.Done()
	for {
		select {
		case job := <-q.jobs:
			q.process(job)
		case <-q.quit:
			for {
				select {
				case job := <-q.jobs:
					q.process(job)
				default:
					return
				}
			}
		}
	}
}

// process dispatches a job to the appropriate handler.
func (q *Queue) process(job Job) {
	var handler func(Job)
	switch job.Type {
	case JobBOMImport:
		handler = q.handleBOMImport
	case JobPartEnrich:
		handler = q.handlePartEnrich
	case JobListingRefresh:
		handler = q.handleListingRefresh
	case JobOrderImport:
		handler = q.handleOrderImport
	case JobOrderLink:
		handler = q.handleOrderLink
	case JobBuildSubstituteSelections:
		handler = q.handleBuildSubstituteSelections
	case JobRetryUnresolved:
		handler = q.handleRetryUnresolved
	case JobPartResolve:
		handler = q.handlePartResolve
	default:
		handler = q.handleUnknown
	}
	handler(job)
}

func (q *Queue) publishError(job Job, msg string) {
	data, _ := json.Marshal(map[string]any{
		"job_id": job.ID,
		"status": "error",
		"error":  msg,
	})
	q.Publish(Event{
		Type:   string(job.Type),
		Data:   data,
		JobID:  job.ID,
		TeamID: job.TeamID,
	})
}

// HasSubscriber reports whether the given user has at least one active SSE
// subscription on the given team. Used by scan endpoints to detect whether the
// desktop is connected before delivering a focus event.
func (q *Queue) HasSubscriber(teamID, userID string) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	for _, sub := range q.subscribers[teamID] {
		if sub.userID == userID {
			return true
		}
	}
	return false
}

func (q *Queue) publishProgress(job Job, eventType string, data any) {
	b, _ := json.Marshal(data)
	q.Publish(Event{Type: eventType, Data: b, JobID: job.ID, TeamID: job.TeamID})
}

// CreateAndEnqueueResolveItem creates a part_resolve_item linked to the given
// bulk scan item and enqueues JobPartResolve. Called synchronously from
// AddBulkScanItem when the DB lookup yields no match.
func (q *Queue) CreateAndEnqueueResolveItem(
	ctx context.Context,
	teamID pgtype.UUID,
	userID pgtype.UUID,
	scanItemID pgtype.UUID,
	inputFields []byte,
) (pgtype.UUID, error) {
	queries := dbgen.New(q.db)
	item, err := queries.CreatePartResolveItem(ctx, dbgen.CreatePartResolveItemParams{
		TeamID:      teamID,
		SourceType:  "bulk_scan_item",
		SourceID:    pgtype.UUID{Bytes: scanItemID.Bytes, Valid: true},
		InputFields: inputFields,
		CreatedBy:   userID,
	})
	if err != nil {
		return pgtype.UUID{}, err
	}
	if err := queries.SetBulkScanItemResolveID(ctx, dbgen.SetBulkScanItemResolveIDParams{
		ID:            scanItemID,
		ResolveItemID: item.ID,
	}); err != nil {
		return pgtype.UUID{}, err
	}
	q.Enqueue(JobPartResolve, teamID.String(), map[string]string{
		"resolve_item_id": item.ID.String(),
		"team_id":         teamID.String(),
		"user_id":         userID.String(),
	})
	return item.ID, nil
}

func (q *Queue) handleUnknown(job Job) {
	q.publishError(job, fmt.Sprintf("unknown job type: %s", job.Type))
}
