// Package worker provides a channel-based background job queue and a
// per-team SSE (Server-Sent Events) hub for pushing job progress to
// connected frontend clients.
package worker

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sync"
)

// JobType identifies the kind of background work to perform.
type JobType string

const (
	JobBOMImport      JobType = "bom_import"
	JobPartEnrich     JobType = "part_enrich"
	JobListingRefresh JobType = "listing_refresh"
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
	Type    string          `json:"event"`
	Data    json.RawMessage `json:"data"`
	JobID   string          `json:"job_id,omitempty"`
	TeamID  string          `json:"-"` // routing only; not serialised
}

// Queue is a channel-based job dispatcher with an integrated SSE hub.
// Jobs are processed by a pool of worker goroutines. Progress events are
// fanned out to all active SSE subscribers for the relevant team.
type Queue struct {
	jobs        chan Job
	subscribers map[string][]chan Event // keyed by team ID
	mu          sync.RWMutex
}

// New creates a Queue and starts n worker goroutines.
func New(workers int) *Queue {
	q := &Queue{
		jobs:        make(chan Job, 64),
		subscribers: make(map[string][]chan Event),
	}
	for i := 0; i < workers; i++ {
		go q.run()
	}
	return q
}

// Enqueue submits a job and returns its ID.
func (q *Queue) Enqueue(jobType JobType, teamID string, payload any) string {
	id := newUUID()
	q.jobs <- Job{ID: id, Type: jobType, TeamID: teamID, Payload: payload}
	return id
}

// newUUID returns a random UUID v4 string.
func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Subscribe registers a channel to receive SSE events for a team.
// The returned cancel function must be called when the SSE connection closes.
func (q *Queue) Subscribe(teamID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	q.mu.Lock()
	q.subscribers[teamID] = append(q.subscribers[teamID], ch)
	q.mu.Unlock()

	cancel := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		subs := q.subscribers[teamID]
		for i, s := range subs {
			if s == ch {
				q.subscribers[teamID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}
	return ch, cancel
}

// Publish sends an event to all SSE subscribers of the event's team.
func (q *Queue) Publish(ev Event) {
	q.mu.RLock()
	subs := q.subscribers[ev.TeamID]
	q.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
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
// Handlers are stubs until implemented; they publish a terminal error event.
func (q *Queue) process(job Job) {
	var handler func(Job)
	switch job.Type {
	case JobBOMImport:
		handler = q.handleBOMImport
	case JobPartEnrich:
		handler = q.handlePartEnrich
	case JobListingRefresh:
		handler = q.handleListingRefresh
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

func (q *Queue) handleBOMImport(job Job) {
	// TODO: implement BOM import enrichment pipeline
	q.publishError(job, fmt.Sprintf("handler %s not yet implemented", job.Type))
}

func (q *Queue) handlePartEnrich(job Job) {
	// TODO: implement part enrichment from distributor APIs
	q.publishError(job, fmt.Sprintf("handler %s not yet implemented", job.Type))
}

func (q *Queue) handleListingRefresh(job Job) {
	// TODO: implement price/availability refresh
	q.publishError(job, fmt.Sprintf("handler %s not yet implemented", job.Type))
}

func (q *Queue) handleUnknown(job Job) {
	q.publishError(job, fmt.Sprintf("unknown job type: %s", job.Type))
}
