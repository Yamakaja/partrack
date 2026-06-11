package timing

import (
	"sort"
	"sync"
	"time"
)

// Tunables for bounded, low-overhead in-memory aggregation.
const (
	// metricsWindow is how long stats accumulate before the registry rotates and
	// starts a fresh epoch. It bounds staleness so the snapshot reflects recent
	// load; memory is separately bounded by the maps being keyed on finite
	// dimensions (route, sqlc query name) or capped (topNCap) for high-cardinality
	// dimensions (user, project).
	metricsWindow = 1 * time.Hour
	// topNCap bounds the per-user and per-project tables. When full, the
	// lowest-cost entry is evicted on insert, yielding an approximate top-N.
	topNCap = 100
)

// stat is cumulative timing for one bounded-cardinality key (route or query).
type stat struct {
	count int64
	sumMs float64
	maxMs float64
}

func (s *stat) add(ms float64) {
	s.count++
	s.sumMs += ms
	if ms > s.maxMs {
		s.maxMs = ms
	}
}

// dimStat aggregates request cost for one high-cardinality dimension value.
type dimStat struct {
	count   int64
	dbMs    float64
	totalMs float64
}

// Registry is a process-global, in-memory aggregator of request and query
// timings. All maps are bounded — keyed on finite dimensions or capped with
// eviction — and the whole set rotates every metricsWindow, so memory never
// grows without bound. Safe for concurrent use.
type Registry struct {
	mu         sync.Mutex
	epochStart time.Time
	window     time.Duration
	cap        int
	routes     map[string]*stat    // route pattern → handler-time stat
	routeDB    map[string]*stat    // route pattern → db-time stat
	queries    map[string]*stat    // sqlc query name → db-time stat
	users      map[string]*dimStat // user id → cost (capped)
	projects   map[string]*dimStat // project id → cost (capped)
}

// NewRegistry returns an empty Registry with default window and cap.
func NewRegistry() *Registry {
	return &Registry{
		epochStart: time.Now(),
		window:     metricsWindow,
		cap:        topNCap,
		routes:     map[string]*stat{},
		routeDB:    map[string]*stat{},
		queries:    map[string]*stat{},
		users:      map[string]*dimStat{},
		projects:   map[string]*dimStat{},
	}
}

var defaultRegistry = NewRegistry()

// Default returns the process-global registry that the middleware feeds.
func Default() *Registry { return defaultRegistry }

// rotateLocked clears all tables and starts a new epoch when the window has
// elapsed. The caller must hold r.mu.
func (r *Registry) rotateLocked(now time.Time) {
	if now.Sub(r.epochStart) < r.window {
		return
	}
	r.epochStart = now
	r.routes = map[string]*stat{}
	r.routeDB = map[string]*stat{}
	r.queries = map[string]*stat{}
	r.users = map[string]*dimStat{}
	r.projects = map[string]*dimStat{}
}

// recordRequest folds one completed request's timings into the registry. route
// is the chi route pattern (empty → ignored); project is the resolved projectId
// URL param if present.
func (r *Registry) recordRequest(route, project string, c *Collector) {
	if route == "" {
		return
	}
	totalMs := durMs(c.durOf("total"))
	dbMs := durMs(c.durOf("db"))
	userID := c.user()
	queries := c.querySnapshot()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.rotateLocked(time.Now())

	statFor(r.routes, route).add(totalMs)
	if dbMs > 0 {
		statFor(r.routeDB, route).add(dbMs)
	}
	for name, qt := range queries {
		s := statFor(r.queries, name)
		// One request may run a query several times; attribute the summed time
		// but count each execution.
		s.count += int64(qt.Count) - 1
		s.add(durMs(qt.Dur))
	}
	if userID != "" {
		d := dimFor(r.users, userID, r.cap)
		d.count++
		d.dbMs += dbMs
		d.totalMs += totalMs
	}
	if project != "" {
		d := dimFor(r.projects, project, r.cap)
		d.count++
		d.dbMs += dbMs
		d.totalMs += totalMs
	}
}

func statFor(m map[string]*stat, k string) *stat {
	s := m[k]
	if s == nil {
		s = &stat{}
		m[k] = s
	}
	return s
}

// dimFor returns the dimStat for k, inserting it if absent. When the map is at
// capacity it evicts the lowest-cost (by dbMs) entry first, bounding memory.
func dimFor(m map[string]*dimStat, k string, cap int) *dimStat {
	if d, ok := m[k]; ok {
		return d
	}
	if len(m) >= cap {
		var minK string
		var minV float64
		first := true
		for kk, vv := range m {
			if first || vv.dbMs < minV {
				minK, minV, first = kk, vv.dbMs, false
			}
		}
		delete(m, minK)
	}
	d := &dimStat{}
	m[k] = d
	return d
}

func durMs(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// ── Snapshot (read model for the admin endpoint) ────────────────────────────

// Snapshot is an immutable view of the registry at a point in time, with each
// table sorted by total time consumed (descending) so the biggest cost centres
// are first.
type Snapshot struct {
	EpochStart    time.Time    `json:"epoch_start"`
	WindowSeconds float64      `json:"window_seconds"`
	Routes        []StatMetric `json:"routes"`
	Queries       []StatMetric `json:"queries"`
	Users         []DimMetric  `json:"users"`
	Projects      []DimMetric  `json:"projects"`
}

// StatMetric is one route or query row.
type StatMetric struct {
	Name    string  `json:"name"`
	Count   int64   `json:"count"`
	SumMs   float64 `json:"sum_ms"`
	AvgMs   float64 `json:"avg_ms"`
	MaxMs   float64 `json:"max_ms"`
	DBSumMs float64 `json:"db_sum_ms,omitempty"` // routes only: db time within the route
}

// DimMetric is one user or project row.
type DimMetric struct {
	ID      string  `json:"id"`
	Count   int64   `json:"count"`
	DBMs    float64 `json:"db_ms"`
	TotalMs float64 `json:"total_ms"`
}

// Snapshot returns the current aggregates, sorted by cost.
func (r *Registry) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := Snapshot{
		EpochStart:    r.epochStart,
		WindowSeconds: r.window.Seconds(),
	}
	for name, s := range r.routes {
		m := StatMetric{Name: name, Count: s.count, SumMs: s.sumMs, MaxMs: s.maxMs}
		if s.count > 0 {
			m.AvgMs = s.sumMs / float64(s.count)
		}
		if db := r.routeDB[name]; db != nil {
			m.DBSumMs = db.sumMs
		}
		snap.Routes = append(snap.Routes, m)
	}
	for name, s := range r.queries {
		m := StatMetric{Name: name, Count: s.count, SumMs: s.sumMs, MaxMs: s.maxMs}
		if s.count > 0 {
			m.AvgMs = s.sumMs / float64(s.count)
		}
		snap.Queries = append(snap.Queries, m)
	}
	for id, d := range r.users {
		snap.Users = append(snap.Users, DimMetric{ID: id, Count: d.count, DBMs: d.dbMs, TotalMs: d.totalMs})
	}
	for id, d := range r.projects {
		snap.Projects = append(snap.Projects, DimMetric{ID: id, Count: d.count, DBMs: d.dbMs, TotalMs: d.totalMs})
	}

	sort.Slice(snap.Routes, func(i, j int) bool { return snap.Routes[i].SumMs > snap.Routes[j].SumMs })
	sort.Slice(snap.Queries, func(i, j int) bool { return snap.Queries[i].SumMs > snap.Queries[j].SumMs })
	sort.Slice(snap.Users, func(i, j int) bool { return snap.Users[i].DBMs > snap.Users[j].DBMs })
	sort.Slice(snap.Projects, func(i, j int) bool { return snap.Projects[i].DBMs > snap.Projects[j].DBMs })
	return snap
}
