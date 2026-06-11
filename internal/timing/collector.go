package timing

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type contextKey struct{}

// Span is a single named timing measurement.
type Span struct {
	Name  string
	Desc  string
	start time.Time
	dur   time.Duration
	done  bool
}

// Stop records the elapsed time since the Span was started. No-op if called more than once.
func (s *Span) Stop() {
	if !s.done {
		s.dur = time.Since(s.start)
		s.done = true
	}
}

// Collector accumulates Server-Timing spans for a single HTTP request.
// It is safe for concurrent use.
type Collector struct {
	mu    sync.Mutex
	spans []*Span
	// Per-sqlc-query timing within this request, fed by DBTracer. Kept separate
	// from spans so it never bloats the Server-Timing header; it exists only to
	// be folded into the global Registry at request end.
	queries     map[string]time.Duration
	queryCounts map[string]int
	userID      string // authenticated user id, set by the auth middleware
}

// AddQuery accumulates a single query's duration under its sqlc name for this
// request. An empty name is bucketed under "(unnamed)".
func (c *Collector) AddQuery(name string, dur time.Duration) {
	if name == "" {
		name = "(unnamed)"
	}
	c.mu.Lock()
	if c.queries == nil {
		c.queries = make(map[string]time.Duration)
		c.queryCounts = make(map[string]int)
	}
	c.queries[name] += dur
	c.queryCounts[name]++
	c.mu.Unlock()
}

// SetUser records the authenticated user id for per-user attribution.
func (c *Collector) SetUser(id string) {
	c.mu.Lock()
	c.userID = id
	c.mu.Unlock()
}

func (c *Collector) user() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userID
}

// durOf returns the recorded (or best-effort in-progress) duration of the named
// span, or 0 if absent.
func (c *Collector) durOf(name string) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.spans {
		if s.Name != name {
			continue
		}
		if s.done {
			return s.dur
		}
		if !s.start.IsZero() {
			return time.Since(s.start)
		}
	}
	return 0
}

// QueryTiming is the per-request rollup for one query name.
type QueryTiming struct {
	Dur   time.Duration
	Count int
}

// querySnapshot copies this request's per-query timings for hand-off to the
// Registry (taken under lock to decouple from the registry's lock).
func (c *Collector) querySnapshot() map[string]QueryTiming {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queries) == 0 {
		return nil
	}
	out := make(map[string]QueryTiming, len(c.queries))
	for k, d := range c.queries {
		out[k] = QueryTiming{Dur: d, Count: c.queryCounts[k]}
	}
	return out
}

// Start records the beginning of a named span and returns it.
// The caller must call Stop() on the returned Span when the work finishes.
// desc is optional — pass an empty string to omit it from the header.
func (c *Collector) Start(name, desc string) *Span {
	s := &Span{Name: name, Desc: desc, start: time.Now()}
	c.mu.Lock()
	c.spans = append(c.spans, s)
	c.mu.Unlock()
	return s
}

// Add records a pre-measured duration directly, accumulating into an existing
// span with the same name or creating a new completed one. Used by DBTracer to
// accumulate multiple query durations into a single "db" metric.
func (c *Collector) Add(name, desc string, dur time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.spans {
		if s.Name == name {
			s.dur += dur
			return
		}
	}
	c.spans = append(c.spans, &Span{Name: name, Desc: desc, dur: dur, done: true})
}

// Header serialises all spans into the Server-Timing header value.
// Duration is in milliseconds (float, 2 decimal places) as required by the spec.
// Spans that were started but never stopped are included with best-effort elapsed time.
func (c *Collector) Header() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var b strings.Builder
	first := true
	for _, s := range c.spans {
		dur := s.dur
		if !s.done {
			if s.start.IsZero() {
				continue
			}
			dur = time.Since(s.start)
		}
		if !first {
			b.WriteString(", ")
		}
		first = false
		fmt.Fprintf(&b, "%s;dur=%.2f", s.Name, float64(dur)/float64(time.Millisecond))
		if s.Desc != "" {
			fmt.Fprintf(&b, ";desc=%q", s.Desc)
		}
	}
	return b.String()
}

// FromContext retrieves the Collector from a request context.
// Returns nil if the timing middleware has not run (safe to call without nil check on Collector methods).
func FromContext(ctx context.Context) *Collector {
	c, _ := ctx.Value(contextKey{}).(*Collector)
	return c
}

func withCollector(ctx context.Context, c *Collector) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}
