package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Reference data — the category tree and the canonical parameter registry — is
// what turns human flags into API query parameters: `--category Resistors` needs
// a numeric id, `--param resistance=10k` needs a parameter id and value type.
//
// Unlike dks, pt does not cache catalog data. The server is yours and has no
// quota, and caching stock or price would defeat the point of asking. Only these
// two slow-moving reference lists are cached, and only to keep flag resolution
// from costing two extra round-trips on every search.
const refDataTTL = 30 * 24 * time.Hour

// refData is the cached bundle, keyed on disk by server URL.
type refData struct {
	FetchedAt  time.Time  `json:"fetched_at"`
	URL        string     `json:"url"`
	Categories []Category `json:"categories"`
	Params     []ParamDef `json:"parameter_definitions"`
}

var refCache *refData

// loadRefData returns the reference bundle, fetching it when absent, stale, or
// when --refresh was given.
func (a *app) loadRefData(ctx context.Context) (*refData, error) {
	if refCache != nil {
		return refCache, nil
	}
	path := a.refDataPath()

	if !gf.refresh {
		var rd refData
		if err := readJSONFile(path, &rd); err == nil &&
			rd.URL == a.cfg.URL && len(rd.Categories) > 0 &&
			time.Since(rd.FetchedAt) < refDataTTL {
			refCache = &rd
			return refCache, nil
		}
	}

	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}

	var catResp struct {
		Categories []Category `json:"categories"`
	}
	if err := client.Get(ctx, "/categories", nil, &catResp); err != nil {
		return nil, err
	}
	var defs []ParamDef
	if err := client.Get(ctx, "/parameter-definitions", nil, &defs); err != nil {
		return nil, err
	}

	rd := &refData{
		FetchedAt:  time.Now().UTC(),
		URL:        a.cfg.URL,
		Categories: catResp.Categories,
		Params:     defs,
	}
	// A cache write failure is not a user-facing error — the data is in hand.
	if data, err := json.MarshalIndent(rd, "", "  "); err == nil {
		if err := writeFileAtomic(path, data, 0o644); err != nil && gf.debug {
			fmt.Fprintf(os.Stderr, "pt: could not write reference cache: %v\n", err)
		}
	}
	refCache = rd
	return refCache, nil
}

func (a *app) refDataPath() string {
	return filepath.Join(a.cfg.CacheDir, "refdata.json")
}

// categoryByID looks up a category in the cached tree.
func (rd *refData) categoryByID(id int32) *Category {
	for i := range rd.Categories {
		if rd.Categories[i].ID == id {
			return &rd.Categories[i]
		}
	}
	return nil
}

// path renders a category's ancestry as "Passive / Resistors / Chip Resistor".
func (rd *refData) path(c *Category) string {
	var parts []string
	seen := map[int32]bool{}
	for cur := c; cur != nil && !seen[cur.ID]; {
		seen[cur.ID] = true
		parts = append([]string{cur.DisplayName}, parts...)
		if cur.ParentID == nil {
			break
		}
		cur = rd.categoryByID(*cur.ParentID)
	}
	return strings.Join(parts, " / ")
}

// resolveCategory turns a user-supplied category reference into a category.
// Accepted, in order: a numeric id, an exact slug, an exact display name
// (case-insensitive), then a unique case-insensitive substring match. An
// ambiguous match lists the candidates and exits 5, so a script never silently
// searches the wrong branch of the tree.
func (rd *refData) resolveCategory(ref string) (*Category, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, exitErrf(ExitUsage, "empty category")
	}
	if n, err := strconv.Atoi(ref); err == nil {
		if c := rd.categoryByID(int32(n)); c != nil {
			return c, nil
		}
		return nil, exitErrf(ExitNotFound, "no category with id %d", n)
	}

	lower := strings.ToLower(ref)
	for i := range rd.Categories {
		if rd.Categories[i].Slug == ref {
			return &rd.Categories[i], nil
		}
	}
	var exact, partial []*Category
	for i := range rd.Categories {
		c := &rd.Categories[i]
		name := strings.ToLower(c.DisplayName)
		switch {
		case name == lower || strings.ToLower(c.Slug) == lower:
			exact = append(exact, c)
		case strings.Contains(name, lower) || strings.Contains(strings.ToLower(c.Slug), lower):
			partial = append(partial, c)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	switch len(candidates) {
	case 0:
		return nil, exitErrf(ExitNotFound,
			"no category matching %q — list them with `pt categories`", ref)
	case 1:
		return candidates[0], nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d categories:", ref, len(candidates))
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].DisplayName < candidates[j].DisplayName
		})
		for _, c := range candidates {
			fmt.Fprintf(&b, "\n  %-5d %s", c.ID, rd.path(c))
		}
		return nil, exitErrf(ExitAmbiguous, "%s", b.String())
	}
}

// effectiveProfile resolves a category's curated class profile — the value
// parameter and the ordered primary parameters an admin set via
// PATCH /admin/categories/{id}/params.
//
// Profiles are curated on the parametric *class* (Resistors, Capacitors), not on
// every leaf, and the server's parameter-schema endpoint reads only the category
// asked for. So a leaf like "Chip Resistor - Surface Mount" reports an empty
// profile even though its parent defines one. The whole tree is cached here, so
// the walk happens client-side; `from` names the category the profile came from,
// which is nil when nothing in the ancestry is curated.
func (rd *refData) effectiveProfile(c *Category) (valueParam string, primary []string, from *Category) {
	seen := map[int32]bool{}
	for cur := c; cur != nil && !seen[cur.ID]; {
		seen[cur.ID] = true
		hasValue := cur.ValueParam != nil && *cur.ValueParam != ""
		if hasValue || len(cur.PrimaryParams) > 0 {
			if hasValue {
				valueParam = *cur.ValueParam
			}
			return valueParam, cur.PrimaryParams, cur
		}
		if cur.ParentID == nil {
			break
		}
		cur = rd.categoryByID(*cur.ParentID)
	}
	return "", nil, nil
}

// rangeParams returns the registry's range-typed definitions. The category
// parameter endpoint filters these out (see GetCategoryParameters), so they never
// appear in a category's observed list even though they are stored and
// filterable — worth naming rather than silently omitting.
func (rd *refData) rangeParams() []ParamDef {
	var out []ParamDef
	for _, p := range rd.Params {
		if p.ValueType == "range" {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DisplayName < out[j].DisplayName })
	return out
}

// paramBySlug looks up a canonical parameter definition by slug, then by
// display name, both case-insensitively.
func (rd *refData) paramBySlug(ref string) *ParamDef {
	lower := strings.ToLower(strings.TrimSpace(ref))
	// Slugs are the canonical spelling, so they win outright.
	for i := range rd.Params {
		if strings.ToLower(rd.Params[i].Slug) == lower {
			return &rd.Params[i]
		}
	}
	// Display names are how they appear in `pt show`, so accept those too.
	normalized := strings.ReplaceAll(lower, "_", " ")
	for i := range rd.Params {
		if strings.ToLower(rd.Params[i].DisplayName) == normalized {
			return &rd.Params[i]
		}
	}
	return nil
}

// children returns a category's direct children, in sort order.
func (rd *refData) children(parentID *int32) []*Category {
	var out []*Category
	for i := range rd.Categories {
		c := &rd.Categories[i]
		switch {
		case parentID == nil && c.ParentID == nil:
			out = append(out, c)
		case parentID != nil && c.ParentID != nil && *c.ParentID == *parentID:
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].DisplayName < out[j].DisplayName
	})
	return out
}
