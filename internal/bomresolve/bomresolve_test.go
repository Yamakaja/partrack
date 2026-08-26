package bomresolve

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func uuid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[0], u.Valid = b, true
	return u
}

func str(s string) *string { return &s }

// A prior-revision reuse records where the decision actually came from, not the
// line it copied. Without collapsing, a project that re-imports an unchanged BOM
// builds a chain of "matched to last revision" pointing at another "matched to
// last revision", which says nothing about what chose the part.
func TestCollapseOriginFoldsChainedReuse(t *testing.T) {
	revA, revB := uuid(1), uuid(2)

	// The source line was itself a reuse: adopt its origin, not the source.
	gotState, gotRev := collapseOrigin(revB, str(StateAutoPriorRevision), str(StateAutoMPN), revA)
	if gotState == nil || *gotState != StateAutoMPN {
		t.Errorf("state = %v, want the origin's state (auto_mpn)", gotState)
	}
	if gotRev != revA {
		t.Errorf("revision = %v, want the origin revision", gotRev)
	}

	// The source line made its own decision: it *is* the origin.
	gotState, gotRev = collapseOrigin(revB, str(StateAutoSKU), nil, pgtype.UUID{})
	if gotState == nil || *gotState != StateAutoSKU {
		t.Errorf("state = %v, want auto_sku", gotState)
	}
	if gotRev != revB {
		t.Errorf("revision = %v, want the source revision", gotRev)
	}

	// A reuse with no recorded origin has nothing to adopt, so it stands as its own.
	gotState, gotRev = collapseOrigin(revB, str(StateAutoPriorRevision), nil, pgtype.UUID{})
	if gotState == nil || *gotState != StateAutoPriorRevision {
		t.Errorf("state = %v, want auto_prior_revision", gotState)
	}
	if gotRev != revB {
		t.Errorf("revision = %v, want the source revision", gotRev)
	}
}

// Resolved separates "this line has a part" from "this line has candidates".
// An ambiguous decision carries a part id in neither sense, and treating it as
// resolved would auto-pick on the user's behalf — the one thing the tiers
// deliberately never do.
func TestResolvedExcludesAmbiguity(t *testing.T) {
	tests := []struct {
		name string
		d    Decision
		want bool
	}{
		{"resolved by mpn", Decision{State: StateAutoMPN, PartID: uuid(1)}, true},
		{"resolved parametrically", Decision{State: StateAutoGeneric, PartID: uuid(1)}, true},
		{"ambiguous with a part id set", Decision{State: StateAmbiguous, PartID: uuid(1)}, false},
		{"ambiguous", Decision{State: StateAmbiguous}, false},
		{"awaiting a distributor", Decision{State: StateImporting}, false},
		{"nothing", Decision{}, false},
	}
	for _, tc := range tests {
		if got := tc.d.Resolved(); got != tc.want {
			t.Errorf("%s: Resolved() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
