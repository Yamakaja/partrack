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
