package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State remembers the last result set so `@3` in one command means row 3 of the
// previous listing. It also keeps a bounded roll of recently seen objects, so a
// UUID prefix copied out of an earlier command still resolves.
//
// This is the only client-side state pt keeps. Nothing here is treated as
// authoritative: identity only, never stock, price, status or location.
type State struct {
	URL     string    `json:"url"`
	TeamID  string    `json:"team_id"`
	SavedAt time.Time `json:"saved_at"`

	// ResultKind names what the @N set holds, so `pt orders show @1` after a
	// `pt search` fails with an explanation rather than fetching an order by a
	// part's id.
	ResultKind string `json:"result_kind"`
	Results    []Ref  `json:"results"`
	Recent     []Ref  `json:"recent"`
}

// Object kinds that can be addressed by @N or by id prefix.
const (
	kindPart     = "part"
	kindProject  = "project"
	kindOrder    = "order"
	kindBuild    = "build"
	kindCheckout = "checkout"
)

// Ref is the identity subset needed to address an object again later.
type Ref struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
	// Manufacturer disambiguates parts, whose MPNs collide across makers.
	Manufacturer string `json:"manufacturer,omitempty"`
}

// UnmarshalJSON accepts the pre-kind state file, which stored parts as
// `{id, mpn, manufacturer}` with no kind. Reading those as parts means an
// upgrade does not silently discard the recent roll a user had built up.
func (r *Ref) UnmarshalJSON(data []byte) error {
	var raw struct {
		Kind         string `json:"kind"`
		ID           string `json:"id"`
		Label        string `json:"label"`
		Mpn          string `json:"mpn"`
		Manufacturer string `json:"manufacturer"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Kind = raw.Kind
	if r.Kind == "" {
		r.Kind = kindPart
	}
	r.ID = raw.ID
	r.Label = raw.Label
	if r.Label == "" {
		r.Label = raw.Mpn
	}
	r.Manufacturer = raw.Manufacturer
	return nil
}

// maxRecent bounds the recently-seen roll. Large enough that a UUID prefix from
// a few commands ago still resolves, small enough to stay a trivial file.
const maxRecent = 300

func (a *app) statePath() string {
	return filepath.Join(a.cfg.CacheDir, "state.json")
}

// loadState reads the state file. A missing or foreign-server file yields an
// empty state rather than an error.
func (a *app) loadState() *State {
	var s State
	if err := readJSONFile(a.statePath(), &s); err != nil {
		return &State{}
	}
	if s.URL != a.cfg.URL {
		return &State{}
	}
	if s.ResultKind == "" && len(s.Results) > 0 {
		s.ResultKind = s.Results[0].Kind
	}
	return &s
}

// saveResults records a command's result rows as the new @N set and folds them
// into the recent roll. Failures are non-fatal — losing @N addressing must never
// fail a listing that already printed its answer.
func (a *app) saveResults(teamID, kind string, refs []Ref) {
	for i := range refs {
		if refs[i].Kind == "" {
			refs[i].Kind = kind
		}
	}

	s := a.loadState()
	s.URL = a.cfg.URL
	s.TeamID = teamID
	s.SavedAt = time.Now().UTC()
	s.ResultKind = kind
	s.Results = refs

	// Prepend the new rows, then de-duplicate by id keeping first (newest) wins.
	merged := make([]Ref, 0, len(refs)+len(s.Recent))
	seen := make(map[string]bool, len(refs)+len(s.Recent))
	for _, list := range [][]Ref{refs, s.Recent} {
		for _, p := range list {
			if p.ID == "" || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			merged = append(merged, p)
		}
	}
	if len(merged) > maxRecent {
		merged = merged[:maxRecent]
	}
	s.Recent = merged

	a.writeState(s)
}

// remember folds a single object into the recent roll without disturbing the
// @N set, so `pt show <mpn>` makes that part addressable by UUID prefix after.
func (a *app) remember(teamID string, r Ref) {
	if r.ID == "" {
		return
	}
	s := a.loadState()
	for _, existing := range s.Recent {
		if existing.ID == r.ID {
			return
		}
	}
	s.URL = a.cfg.URL
	if s.TeamID == "" {
		s.TeamID = teamID
	}
	s.Recent = append([]Ref{r}, s.Recent...)
	if len(s.Recent) > maxRecent {
		s.Recent = s.Recent[:maxRecent]
	}
	a.writeState(s)
}

// writeState persists the state file, best-effort. A write failure only
// surfaces under --debug: the command has already produced its answer.
func (a *app) writeState(s *State) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := writeFileAtomic(a.statePath(), data, 0o644); err != nil && gf.debug {
		fmt.Fprintf(os.Stderr, "pt: could not write state: %v\n", err)
	}
}
