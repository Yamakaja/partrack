package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State remembers the last result set so `@3` in one command means row 3 of the
// previous `pt search`. It also keeps a bounded roll of recently seen parts, so
// a UUID prefix copied out of an earlier command still resolves.
//
// This is the only client-side state pt keeps about parts. Nothing here is
// treated as authoritative: identity only, never stock, price or location.
type State struct {
	URL     string      `json:"url"`
	TeamID  string      `json:"team_id"`
	SavedAt time.Time   `json:"saved_at"`
	Results []PartIdent `json:"results"`
	Recent  []PartIdent `json:"recent"`
}

// PartIdent is the identity subset needed to address a part again later.
type PartIdent struct {
	ID           string `json:"id"`
	Mpn          string `json:"mpn"`
	Manufacturer string `json:"manufacturer"`
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
	return &s
}

// saveResults records a command's result rows as the new @N set and folds them
// into the recent roll. Failures are non-fatal — losing @N addressing must never
// fail a search that already printed its answer.
func (a *app) saveResults(teamID string, idents []PartIdent) {
	s := a.loadState()
	s.URL = a.cfg.URL
	s.TeamID = teamID
	s.SavedAt = time.Now().UTC()
	s.Results = idents

	// Prepend the new rows, then de-duplicate by id keeping first (newest) wins.
	merged := make([]PartIdent, 0, len(idents)+len(s.Recent))
	seen := make(map[string]bool, len(idents)+len(s.Recent))
	for _, list := range [][]PartIdent{idents, s.Recent} {
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

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := writeFileAtomic(a.statePath(), data, 0o644); err != nil && gf.debug {
		fmt.Fprintf(os.Stderr, "pt: could not write state: %v\n", err)
	}
}

// rememberPart folds a single part into the recent roll without disturbing the
// @N set, so `pt show <mpn>` makes that part addressable by UUID prefix after.
func (a *app) rememberPart(teamID string, p PartIdent) {
	if p.ID == "" {
		return
	}
	s := a.loadState()
	for _, r := range s.Recent {
		if r.ID == p.ID {
			return
		}
	}
	s.URL = a.cfg.URL
	if s.TeamID == "" {
		s.TeamID = teamID
	}
	s.Recent = append([]PartIdent{p}, s.Recent...)
	if len(s.Recent) > maxRecent {
		s.Recent = s.Recent[:maxRecent]
	}
	if data, err := json.MarshalIndent(s, "", "  "); err == nil {
		writeFileAtomic(a.statePath(), data, 0o644) //nolint:errcheck // best-effort
	}
}
