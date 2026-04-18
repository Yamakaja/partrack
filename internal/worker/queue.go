// Package worker provides a channel-based background job queue and a
// per-team SSE (Server-Sent Events) hub for pushing job progress to
// connected frontend clients.
package worker

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Yamakaja/partrack/internal/distributor"
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
}

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
	}
	for i := 0; i < workers; i++ {
		go q.run()
	}
	return q
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

// run is the worker goroutine loop.
func (q *Queue) run() {
	for job := range q.jobs {
		q.process(job)
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

func (q *Queue) publishProgress(job Job, eventType string, data any) {
	b, _ := json.Marshal(data)
	q.Publish(Event{Type: eventType, Data: b, JobID: job.ID, TeamID: job.TeamID})
}

func (q *Queue) handleUnknown(job Job) {
	q.publishError(job, fmt.Sprintf("unknown job type: %s", job.Type))
}
