package mfrmatch

import (
	"context"
	"strings"
	"sync"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgxpool"
)

// canonical is the resolved identity a normalized spelling maps to.
type canonical struct {
	id            string // manufacturer UUID
	canonicalName string // preferred display spelling
	normalizedKey string // Normalize(canonicalName); the match key
}

// Alias is one alias → canonical mapping, used to seed a Resolver from a non-DB
// source (tests, or callers that source equivalences elsewhere).
type Alias struct {
	AliasNormalized string
	ManufacturerID  string
	CanonicalName   string
	NormalizedName  string
}

// Resolver maps manufacturer-name spellings to canonical identities using an
// in-memory alias table refreshed from the database. All methods are safe for
// concurrent use. A nil or empty table means Normalize-only behaviour.
type Resolver struct {
	db      *pgxpool.Pool
	mu      sync.RWMutex
	aliases map[string]canonical // alias_normalized → canonical identity
}

// NewResolver returns a Resolver backed by db. Call Reload once before use (it is
// empty, i.e. Normalize-only, until then).
func NewResolver(db *pgxpool.Pool) *Resolver {
	return &Resolver{db: db, aliases: make(map[string]canonical)}
}

// NewStaticResolver returns a Resolver backed by a fixed alias list and no database;
// Reload is a no-op. Intended for tests and non-DB callers.
func NewStaticResolver(aliases []Alias) *Resolver {
	r := &Resolver{aliases: make(map[string]canonical, len(aliases))}
	for _, a := range aliases {
		r.aliases[a.AliasNormalized] = canonical{
			id:            a.ManufacturerID,
			canonicalName: a.CanonicalName,
			normalizedKey: a.NormalizedName,
		}
	}
	return r
}

// Reload rebuilds the alias table from the database and atomically swaps it in.
// Safe to call concurrently with lookups. A no-op for a static (db == nil) resolver.
// Called once at startup and again after admin mutations.
func (r *Resolver) Reload(ctx context.Context) error {
	if r.db == nil {
		return nil
	}
	rows, err := dbgen.New(r.db).ListManufacturerAliases(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]canonical, len(rows))
	for _, row := range rows {
		m[row.AliasNormalized] = canonical{
			id:            row.ManufacturerID.String(),
			canonicalName: row.CanonicalName,
			normalizedKey: row.NormalizedName,
		}
	}
	r.mu.Lock()
	r.aliases = m
	r.mu.Unlock()
	return nil
}

// lookup returns the canonical identity for a name and whether it is a known alias.
func (r *Resolver) lookup(name string) (canonical, bool) {
	norm := Normalize(name)
	if norm == "" {
		return canonical{}, false
	}
	r.mu.RLock()
	c, ok := r.aliases[norm]
	r.mu.RUnlock()
	return c, ok
}

// CanonicalKey returns the match key for a manufacturer name: the normalized name of
// the canonical manufacturer the spelling maps to, or its plain normalized form when
// the spelling is unknown. Two names with the same non-empty CanonicalKey are the
// same manufacturer for matching and dedup.
func (r *Resolver) CanonicalKey(name string) string {
	if c, ok := r.lookup(name); ok {
		return c.normalizedKey
	}
	return Normalize(name)
}

// CanonicalName returns the canonical display spelling and true when name is a known
// alias; otherwise it returns name unchanged and false. Callers persist the canonical
// spelling on parts so the (manufacturer, mpn) uniqueness index collapses distributor
// spelling variants to a single part.
func (r *Resolver) CanonicalName(name string) (string, bool) {
	if c, ok := r.lookup(name); ok {
		return c.canonicalName, true
	}
	return name, false
}

// Matches reports whether got and want refer to the same manufacturer. An empty want
// ("manufacturer unknown") matches anything, so a caller that cannot determine the
// target manufacturer falls back to MPN-only matching rather than rejecting
// everything. Alias-aware superset of plain normalized equality.
func (r *Resolver) Matches(got, want string) bool {
	if strings.TrimSpace(want) == "" {
		return true
	}
	kg, kw := r.CanonicalKey(got), r.CanonicalKey(want)
	return kg != "" && kg == kw
}
