// Package partmatch implements the parametric (generic) part match-only tier: it
// finds existing catalog parts that satisfy a genspec interpretation by category +
// canonical package + primary value (within a tolerance band). It is the single
// definition of "what a generic BOM line matches", shared by the BOM import job
// (auto-resolve on a unique match) and the review-gate candidate picker (surface
// all matches). It never creates a part.
package partmatch

import (
	"context"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/jackc/pgx/v5/pgtype"
)

// Find returns all active catalog parts (global + team) matching the
// interpretation's category subtree, canonical package, and primary-value
// magnitude within the tolerance band, ordered by closeness to nominal. It returns
// no candidates (and no error) when the interpretation is not a value-bearing
// parametric reading — no class profile, no value parameter, or no nominal value.
//
// Band: ±1% by default; for a tolerance-bearing line ±(0.1·T) where T is the
// tolerance in percent (a 10% line widens to ±1%, a 1% line to ±0.1%).
func Find(ctx context.Context, q *dbgen.Queries, teamID pgtype.UUID, interp genspec.Interpretation) ([]dbgen.FindGenericPartCandidatesRow, error) {
	if interp.Route != genspec.RouteParametric || interp.Category == "" || interp.PackageValue() == "" {
		return nil, nil
	}
	prof, ok := genspec.ProfileForClass(interp.Class)
	if !ok || prof.ValueParam == "" {
		return nil, nil
	}

	var nominal, tol *float64
	for _, p := range interp.Params {
		switch p.Slug {
		case prof.ValueParam:
			nominal = p.ValueSI
		case "tolerance":
			tol = p.ValueSI
		}
	}
	if nominal == nil {
		return nil, nil
	}

	cat, err := q.GetCategoryBySlug(ctx, interp.Category)
	if err != nil {
		return nil, err
	}
	paramDef, err := q.GetParameterBySlug(ctx, prof.ValueParam)
	if err != nil {
		return nil, err
	}

	eps := 0.01
	if tol != nil && *tol > 0 {
		eps = 0.001 * (*tol)
	}
	lo := *nominal * (1 - eps)
	hi := *nominal * (1 + eps)
	if lo > hi {
		lo, hi = hi, lo
	}

	// The stored package is raw (possibly user-edited); canonicalise to match
	// parts.canonical_package, falling back to the raw form.
	pkg := genspec.NormalizePackage(interp.PackageValue())
	if pkg == "" {
		pkg = interp.PackageValue()
	}
	pid := paramDef.ID
	return q.FindGenericPartCandidates(ctx, dbgen.FindGenericPartCandidatesParams{
		TeamID:           teamID,
		CategoryID:       cat.ID,
		CanonicalPackage: &pkg,
		ParameterID:      &pid,
		MinVal:           &lo,
		MaxVal:           &hi,
		Nominal:          *nominal,
	})
}
