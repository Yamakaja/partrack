// Package bomresolve is the single definition of how a BOM line finds its Part.
//
// The tier ladder — SKU, exact MPN, prior revision, parametric match, fuzzy MPN,
// and finally a distributor lookup — used to live inside the import job's write
// loop, which meant the only way to learn what an import would do was to run one.
// A preview written separately would have been a second implementation of the
// same decision, and the two would have drifted the moment either changed;
// predicting an import wrongly is worse than not predicting it at all.
//
// So the ladder is here, as a pure decision: it reads the catalog and returns
// what it concluded, and never writes. The import job applies the decision; the
// resolve-preview handler renders it. Both walk the same rungs in the same order.
//
// The tiers are conservative by construction. Ambiguity is never resolved by
// picking: several matching parts leave the line for a human, and a line the
// catalog cannot satisfy is left unresolved rather than having a part invented
// for it (generic-part creation is an explicit user action — doc/bom-import.md
// §7.2).
package bomresolve

import (
	"context"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/partmatch"
	"github.com/jackc/pgx/v5/pgtype"
)

// Resolution states, as persisted in bom_line_items.resolution_state.
const (
	StateAutoSKU           = "auto_sku"
	StateAutoMPN           = "auto_mpn"
	StateAutoMPNFuzzy      = "auto_mpn_fuzzy"
	StateAutoPriorRevision = "auto_prior_revision"
	StateAutoGeneric       = "auto_generic"
	StateAmbiguous         = "ambiguous"
	StateImporting         = "importing"
	// StateUnresolved is the empty state: no tier hit and there is nothing to
	// look up at a distributor either. The line waits for a human.
	StateUnresolved = ""
)

// Line is one BOM line as the tiers see it — the identity columns, the raw
// value/footprint used for line-signature reuse, and the structured reading of
// the row.
type Line struct {
	ReferenceDesignators string
	Mpn                  *string
	Manufacturer         *string
	Sku                  *string
	Value                *string
	Footprint            *string

	// ResolvedDistributorID is what makes a SKU resolvable. Without it the SKU
	// tier cannot run at all, which is why a BOM with SKUs and no distributor
	// column resolves none of them.
	ResolvedDistributorID pgtype.UUID

	// Interpretation is the confirmed reading of the row. A zero value simply
	// skips the parametric tier.
	Interpretation genspec.Interpretation
}

// Scope is the catalog and project the line resolves against.
type Scope struct {
	TeamID pgtype.UUID

	// ProjectID enables the prior-revision tier. An invalid id skips it, which
	// is what a preview with no project selected wants.
	ProjectID pgtype.UUID

	// CurrentRevisionID is the revision being imported, excluded from
	// prior-revision lookups. Invalid means "exclude nothing" — a preview has no
	// revision of its own and must still see what earlier ones resolved.
	CurrentRevisionID pgtype.UUID
}

// Decision is what the ladder concluded about one line. It is advice, not an
// action: nothing has been written.
type Decision struct {
	// State is the resolution_state the import would record.
	State string
	// PartID is the resolved part; valid only for the resolving states.
	PartID pgtype.UUID
	// Tier names the rung that decided, for explaining the outcome.
	Tier string

	// Candidates are the catalog parts the deciding tier considered — one on a
	// clean resolve, several on an ambiguity. Every tier that queries the catalog
	// fills this, so a caller can always show what the choice was between rather
	// than reporting a bare "ambiguous" and making the user go and look.
	Candidates []Candidate

	// OriginState and OriginRevisionID carry the provenance of a prior-revision
	// reuse: the decision it ultimately descends from, collapsed to one hop.
	OriginState      *string
	OriginRevisionID pgtype.UUID
}

// Candidate is a catalog part a tier considered. The fields are the union of
// what the tiers can supply: the parametric tier knows a part's stock and how
// close its value is to the line's nominal, the fuzzy MPN tier knows only the
// identity. Absent fields are absent, not defaulted.
type Candidate struct {
	ID             pgtype.UUID
	Mpn            *string
	Manufacturer   *string
	Package        *string
	Description    *string
	Scope          string
	QuantityOnHand *int32
	ValueSI        *float64
}

// Resolved reports whether the decision links the line to a part.
func (d Decision) Resolved() bool { return d.PartID.Valid && d.State != StateAmbiguous }

// Resolve walks the tier ladder for one line and reports what it found.
//
// Query errors are treated as "this tier found nothing" rather than failing the
// line, matching the import job: a BOM with one unlucky lookup should still
// import, with the affected line surfacing for review.
func Resolve(ctx context.Context, q *dbgen.Queries, scope Scope, line Line) Decision {
	var d Decision

	hasMPN := line.Mpn != nil && *line.Mpn != ""
	hasMfr := line.Manufacturer != nil && *line.Manufacturer != ""
	hasSKU := line.Sku != nil && *line.Sku != ""

	// 1. SKU — only with a distributor to resolve it against.
	if hasSKU && line.ResolvedDistributorID.Valid {
		if listing, err := q.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
			Sku:           *line.Sku,
			DistributorID: line.ResolvedDistributorID,
		}); err == nil {
			d.PartID, d.State, d.Tier = listing.PartID, StateAutoSKU, "sku"
		}
	}

	// 2a. MPN + manufacturer, preferred: the same MPN across two manufacturers
	// is not an ambiguity worth raising when the BOM says which one it means.
	if d.State == StateUnresolved && hasMPN && hasMfr {
		if matched, err := q.ListPartsByMPNAndManufacturer(ctx, dbgen.ListPartsByMPNAndManufacturerParams{
			Mpn:          line.Mpn,
			Manufacturer: line.Manufacturer,
			TeamID:       scope.TeamID,
		}); err == nil {
			if len(matched) > 0 {
				d.Candidates = candidatesFromMPNAndManufacturer(matched)
				if len(matched) == 1 {
					d.PartID, d.State, d.Tier = matched[0].ID, StateAutoMPN, "mpn_manufacturer"
				} else {
					d.State, d.Tier = StateAmbiguous, "mpn_manufacturer"
				}
			}
		}
	}

	// 2b. MPN alone, when there was no manufacturer or it matched nothing.
	if d.State == StateUnresolved && hasMPN {
		if matched, err := q.ListPartsByMPN(ctx, dbgen.ListPartsByMPNParams{
			Mpn:    line.Mpn,
			TeamID: scope.TeamID,
		}); err == nil {
			if len(matched) > 0 {
				d.Candidates = candidatesFromMPN(matched)
				if len(matched) == 1 {
					d.PartID, d.State, d.Tier = matched[0].ID, StateAutoMPN, "mpn"
				} else {
					d.State, d.Tier = StateAmbiguous, "mpn"
				}
			}
		}
	}

	// 3. Prior revision. This also runs on an ambiguous result and can settle it:
	// a project that already chose between those candidates has answered the
	// question, and re-asking every revision would be the friction the reuse
	// design exists to remove.
	if scope.ProjectID.Valid && (d.State == StateUnresolved || d.State == StateAmbiguous) {
		if prior, origin, originRev, tier := priorRevisionPart(ctx, q, scope, line, hasMPN, hasMfr, hasSKU); prior.Valid {
			d.PartID, d.State, d.Tier = prior, StateAutoPriorRevision, tier
			d.OriginState, d.OriginRevisionID = origin, originRev
		}
	}

	// 4. Parametric match-only: an existing catalog part satisfying the
	// interpretation's constraints. Never creates one.
	if d.State == StateUnresolved {
		if matches, err := partmatch.Find(ctx, q, scope.TeamID, line.Interpretation); err == nil && len(matches) > 0 {
			d.Candidates = candidatesFromParametric(matches)
			if len(matches) == 1 {
				d.PartID, d.State, d.Tier = matches[0].ID, StateAutoGeneric, "parametric"
			} else {
				d.State, d.Tier = StateAmbiguous, "parametric"
			}
		}
	}

	// 5. Fuzzy local MPN: the catalog often holds a packaged variant of the BOM's
	// base part (BOM "AZ431A" → catalog "AZ431AN-ATRG1"). A single hit resolves,
	// but stays flagged — a substring match is a weaker claim than an exact one,
	// so the review queue still shows it for a one-click confirm.
	if d.State == StateUnresolved && hasMPN {
		if fuzzy, err := q.FindPartsByMPNFuzzy(ctx, dbgen.FindPartsByMPNFuzzyParams{
			Search: line.Mpn,
			TeamID: scope.TeamID,
		}); err == nil && len(fuzzy) > 0 {
			d.Candidates = candidatesFromFuzzy(fuzzy)
			if len(fuzzy) == 1 {
				d.PartID, d.State, d.Tier = fuzzy[0].ID, StateAutoMPNFuzzy, "mpn_fuzzy"
			} else {
				d.State, d.Tier = StateAmbiguous, "mpn_fuzzy"
			}
		}
	}

	// 6. Not in the library. With an identifier, the distributor resolve pipeline
	// can go and find it; without one there is nothing to look up.
	if d.State == StateUnresolved && (hasMPN || (hasSKU && line.ResolvedDistributorID.Valid)) {
		d.State, d.Tier = StateImporting, "distributor"
	}

	return d
}

// priorRevisionPart looks for an earlier revision of the same project that
// already resolved this line, by MPN, then SKU, then the line's own signature.
// The signature tier is what carries a manual resolution of a generic line
// forward: it has no MPN or SKU to key on, but the same designators with the
// same value and footprint are the same line.
func priorRevisionPart(ctx context.Context, q *dbgen.Queries, scope Scope, line Line, hasMPN, hasMfr, hasSKU bool) (pgtype.UUID, *string, pgtype.UUID, string) {
	if hasMPN {
		if hasMfr {
			if r, err := q.GetPriorRevisionPartByMPNAndManufacturer(ctx, dbgen.GetPriorRevisionPartByMPNAndManufacturerParams{
				ProjectID:         scope.ProjectID,
				CurrentRevisionID: scope.CurrentRevisionID,
				Mpn:               line.Mpn,
				Manufacturer:      line.Manufacturer,
			}); err == nil {
				origin, rev := collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
				return r.ResolvedPartID, origin, rev, "prior_revision_mpn"
			}
		}
		if r, err := q.GetPriorRevisionPartByMPN(ctx, dbgen.GetPriorRevisionPartByMPNParams{
			ProjectID:         scope.ProjectID,
			CurrentRevisionID: scope.CurrentRevisionID,
			Mpn:               line.Mpn,
		}); err == nil {
			origin, rev := collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
			return r.ResolvedPartID, origin, rev, "prior_revision_mpn"
		}
	}

	if hasSKU && line.ResolvedDistributorID.Valid {
		if r, err := q.GetPriorRevisionPartBySKU(ctx, dbgen.GetPriorRevisionPartBySKUParams{
			ProjectID:         scope.ProjectID,
			CurrentRevisionID: scope.CurrentRevisionID,
			Sku:               line.Sku,
			DistributorID:     line.ResolvedDistributorID,
		}); err == nil {
			origin, rev := collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
			return r.ResolvedPartID, origin, rev, "prior_revision_sku"
		}
	}

	if r, err := q.GetPriorRevisionPartByLineSignature(ctx, dbgen.GetPriorRevisionPartByLineSignatureParams{
		ProjectID:            scope.ProjectID,
		CurrentRevisionID:    scope.CurrentRevisionID,
		ReferenceDesignators: line.ReferenceDesignators,
		Value:                line.Value,
		Footprint:            line.Footprint,
	}); err == nil {
		origin, rev := collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
		return r.ResolvedPartID, origin, rev, "prior_revision_signature"
	}

	var none pgtype.UUID
	return none, nil, pgtype.UUID{}, ""
}

// collapseOrigin folds a prior-revision reuse into a single hop. If the line
// being reused had itself inherited its part, we adopt its origin rather than
// pointing at it — otherwise a project that re-imports an unchanged BOM builds a
// chain of "matched to last revision" that says nothing about the decision that
// actually chose the part.
func collapseOrigin(srcRevID pgtype.UUID, srcState, srcOriginState *string, srcOriginRevID pgtype.UUID) (*string, pgtype.UUID) {
	if srcState != nil && *srcState == StateAutoPriorRevision && srcOriginState != nil {
		return srcOriginState, srcOriginRevID
	}
	return srcState, srcRevID
}

// The conversions below exist because sqlc emits a distinct row type per query.
// Folding them into one Candidate is what lets a caller treat "what was this
// line choosing between?" as one question rather than four.

func candidatesFromParametric(rows []dbgen.FindGenericPartCandidatesRow) []Candidate {
	out := make([]Candidate, len(rows))
	for i, r := range rows {
		out[i] = Candidate{
			ID: r.ID, Mpn: r.Mpn, Manufacturer: r.Manufacturer,
			Package: r.CanonicalPackage, Description: r.Description,
			Scope: string(r.Scope), QuantityOnHand: r.QuantityOnHand, ValueSI: r.ValueSi,
		}
	}
	return out
}

func candidatesFromMPN(rows []dbgen.ListPartsByMPNRow) []Candidate {
	out := make([]Candidate, len(rows))
	for i, r := range rows {
		out[i] = Candidate{
			ID: r.ID, Mpn: r.Mpn, Manufacturer: r.Manufacturer,
			Package: r.Package, Description: r.Description,
		}
	}
	return out
}

func candidatesFromMPNAndManufacturer(rows []dbgen.ListPartsByMPNAndManufacturerRow) []Candidate {
	out := make([]Candidate, len(rows))
	for i, r := range rows {
		out[i] = Candidate{
			ID: r.ID, Mpn: r.Mpn, Manufacturer: r.Manufacturer,
			Package: r.Package, Description: r.Description,
		}
	}
	return out
}

func candidatesFromFuzzy(rows []dbgen.FindPartsByMPNFuzzyRow) []Candidate {
	out := make([]Candidate, len(rows))
	for i, r := range rows {
		out[i] = Candidate{ID: r.ID, Mpn: r.Mpn, Manufacturer: r.Manufacturer}
	}
	return out
}
