package timing

import (
	"testing"
	"time"
)

func TestQueryName(t *testing.T) {
	cases := map[string]string{
		"-- name: GetOrder :one\nSELECT * FROM orders": "GetOrder",
		"-- name: ListParts :many\nSELECT 1":           "ListParts",
		"-- name: DoThing :exec":                       "DoThing",
		"  -- name: Trimmed :one\nSELECT 1":            "Trimmed",
		"SELECT 1":                                     "",
		"BEGIN":                                        "",
		"":                                             "",
	}
	for sql, want := range cases {
		if got := queryName(sql); got != want {
			t.Errorf("queryName(%q) = %q, want %q", sql, got, want)
		}
	}
}

// newCollector builds a Collector pre-loaded with total/db spans and queries,
// as the middleware+tracer would have at request end.
func newCollector(totalMs, dbMs float64, user string, queries map[string]int) *Collector {
	c := &Collector{}
	c.Add("total", "", time.Duration(totalMs*float64(time.Millisecond)))
	c.Add("db", "", time.Duration(dbMs*float64(time.Millisecond)))
	if user != "" {
		c.SetUser(user)
	}
	for name, n := range queries {
		for i := 0; i < n; i++ {
			c.AddQuery(name, time.Millisecond)
		}
	}
	return c
}

func TestRegistryAggregation(t *testing.T) {
	r := NewRegistry()
	r.recordRequest("/projects/{projectId}", "p1", newCollector(20, 10, "u1", map[string]int{"GetOrder": 1, "ListParts": 2}))
	r.recordRequest("/projects/{projectId}", "p1", newCollector(30, 20, "u1", map[string]int{"GetOrder": 1}))
	r.recordRequest("/health", "", newCollector(1, 0, "u2", nil))

	snap := r.Snapshot()

	// Routes sorted by total time desc: the project route (50ms) before /health (1ms).
	if len(snap.Routes) != 2 || snap.Routes[0].Name != "/projects/{projectId}" {
		t.Fatalf("routes = %+v", snap.Routes)
	}
	if snap.Routes[0].Count != 2 || snap.Routes[0].SumMs != 50 || snap.Routes[0].DBSumMs != 30 {
		t.Errorf("project route stat = %+v", snap.Routes[0])
	}

	// Queries: GetOrder ran twice (once per request), ListParts twice in one request.
	byName := map[string]StatMetric{}
	for _, q := range snap.Queries {
		byName[q.Name] = q
	}
	if got := byName["GetOrder"].Count; got != 2 {
		t.Errorf("GetOrder count = %d, want 2", got)
	}
	if got := byName["ListParts"].Count; got != 2 {
		t.Errorf("ListParts count = %d, want 2", got)
	}

	// Users sorted by db time desc: u1 (30ms) before u2 (0ms).
	if len(snap.Users) != 2 || snap.Users[0].ID != "u1" || snap.Users[0].DBMs != 30 {
		t.Errorf("users = %+v", snap.Users)
	}
	// Only p1 has a project attribution (/health request had none).
	if len(snap.Projects) != 1 || snap.Projects[0].ID != "p1" || snap.Projects[0].Count != 2 {
		t.Errorf("projects = %+v", snap.Projects)
	}
}

func TestRegistryEmptyRouteIgnored(t *testing.T) {
	r := NewRegistry()
	r.recordRequest("", "", newCollector(5, 5, "u1", map[string]int{"X": 1}))
	snap := r.Snapshot()
	if len(snap.Routes) != 0 || len(snap.Queries) != 0 || len(snap.Users) != 0 {
		t.Errorf("unrouted request should be ignored, got %+v", snap)
	}
}

func TestDimForEviction(t *testing.T) {
	m := map[string]*dimStat{}
	// Fill beyond cap; lowest-dbMs entries should be evicted.
	for i := 0; i < 5; i++ {
		d := dimFor(m, string(rune('a'+i)), 3)
		d.dbMs = float64(i) // a=0, b=1, c=2, d=3, e=4
	}
	if len(m) != 3 {
		t.Fatalf("len = %d, want 3 (capped)", len(m))
	}
	// The two smallest (a=0, b=1) should have been evicted.
	if _, ok := m["a"]; ok {
		t.Errorf("expected 'a' (lowest cost) to be evicted")
	}
	if _, ok := m["e"]; !ok {
		t.Errorf("expected 'e' (highest cost) to be retained")
	}
}
