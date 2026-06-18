// Package partmatch implements the parametric (generic) part match-only tier: it
// finds existing catalog parts that satisfy a genspec interpretation. It is the
// single definition of "what a generic BOM line matches", shared by the BOM import
// job (auto-resolve on a unique match) and the review-gate candidate picker (surface
// all matches). It never creates a part.
//
// Three matching modes, picked from the interpretation's shape:
//   - value-band: a package plus a primary magnitude (R/C/L) — exact package, value
//     within a tolerance band, ordered by closeness to nominal.
//   - package-only: a package but no magnitude (a commodity such as an LED or
//     ferrite bead) — category subtree + package alone.
//   - signature: no package but a set of discriminating numeric parameters (pin
//     headers: positions/rows/pitch) — every provided parameter matched within a
//     tight epsilon.
package partmatch

import (
	"context"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/jackc/pgx/v5/pgtype"
)

// Find returns all active catalog parts (global + team) that satisfy the
// interpretation, using the matching mode implied by its shape (see package doc).
// It returns no candidates (and no error) when the interpretation is not a
// parametric reading, carries no category, or carries nothing to match on (neither
// a package nor any numeric parameter).
//
// Band (value-band mode): ±1% by default; for a tolerance-bearing line ±(0.1·T)
// where T is the tolerance in percent (a 10% line widens to ±1%, a 1% line to ±0.1%).
func Find(ctx context.Context, q *dbgen.Queries, teamID pgtype.UUID, interp genspec.Interpretation) ([]dbgen.FindGenericPartCandidatesRow, error) {
	if interp.Route != genspec.RouteParametric || interp.Category == "" {
		return nil, nil
	}

	cat, err := q.GetCategoryBySlug(ctx, interp.Category)
	if err != nil {
		return nil, err
	}

	// The class may define a primary value parameter (resistance, capacitance, …).
	// Find the interpretation's nominal for it, if both exist.
	var valueParam string
	if prof, ok := genspec.ProfileForClass(interp.Class); ok {
		valueParam = prof.ValueParam
	}
	var nominal, tol *float64
	if valueParam != "" {
		for _, p := range interp.Params {
			switch p.Slug {
			case valueParam:
				nominal = p.ValueSI
			case "tolerance":
				tol = p.ValueSI
			}
		}
	}

	// The stored package is raw (possibly user-edited); canonicalise to match
	// parts.canonical_package, falling back to the raw form.
	pkg := genspec.NormalizePackage(interp.PackageValue())
	if pkg == "" {
		pkg = interp.PackageValue()
	}

	// No package: match on the interpretation's discriminating numeric parameters
	// (pin headers and the like). Falls back to no match when there are none.
	if pkg == "" {
		return findBySignature(ctx, q, teamID, cat.ID, interp)
	}

	args := dbgen.FindGenericPartCandidatesParams{
		TeamID:           teamID,
		CategoryID:       cat.ID,
		CanonicalPackage: &pkg,
	}

	// Value-bearing line: constrain to the nominal within a tolerance band. A line
	// with no nominal falls through to a package-only match (parameter_id NULL).
	if nominal != nil {
		paramDef, err := q.GetParameterBySlug(ctx, valueParam)
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
		pid := paramDef.ID
		args.ParameterID = &pid
		args.MinVal = &lo
		args.MaxVal = &hi
		args.Nominal = nominal
	}

	return q.FindGenericPartCandidates(ctx, args)
}

// findBySignature matches a package-less generic line on the set of numeric
// parameters it carries (e.g. a pin header's positions/rows/pitch). Every such
// parameter must match a catalog part within the query's epsilon. Param slugs that
// aren't known parameter definitions are skipped; with no usable constraints there
// is nothing to match on, so it returns no candidates.
func findBySignature(ctx context.Context, q *dbgen.Queries, teamID pgtype.UUID, categoryID int32, interp genspec.Interpretation) ([]dbgen.FindGenericPartCandidatesRow, error) {
	var pids []int32
	var vals []float64
	for _, p := range interp.Params {
		if p.Slug == genspec.PackageSlug || p.ValueSI == nil {
			continue
		}
		def, err := q.GetParameterBySlug(ctx, p.Slug)
		if err != nil {
			continue // unknown parameter slug — can't constrain on it
		}
		pids = append(pids, def.ID)
		vals = append(vals, *p.ValueSI)
	}
	if len(pids) == 0 {
		return nil, nil
	}

	rows, err := q.FindGenericPartCandidatesBySignature(ctx, dbgen.FindGenericPartCandidatesBySignatureParams{
		TeamID:       teamID,
		CategoryID:   categoryID,
		ParameterIds: pids,
		Values:       vals,
	})
	if err != nil {
		return nil, err
	}

	// Adapt to the shared candidate row (value_si is not meaningful in this mode).
	out := make([]dbgen.FindGenericPartCandidatesRow, len(rows))
	for i, r := range rows {
		out[i] = dbgen.FindGenericPartCandidatesRow{
			ID:               r.ID,
			Mpn:              r.Mpn,
			Manufacturer:     r.Manufacturer,
			CanonicalPackage: r.CanonicalPackage,
			Description:      r.Description,
			Scope:            r.Scope,
			QuantityOnHand:   r.QuantityOnHand,
		}
	}
	return out, nil
}
