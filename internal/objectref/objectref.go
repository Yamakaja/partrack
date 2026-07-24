// Package objectref defines how partrack's linkable objects are identified and
// displayed, in one place.
//
// The same question — "what do we call this order?" — is asked by the activity
// log, note link chips, the command palette, and every table that references
// something it doesn't own. Answering it independently in each of those is how
// the app ended up calling one order "the order", "#PO-1042" and
// "DigiKey #PO-1042" on three adjacent screens. This package is the single
// answer; callers format through it rather than assembling strings themselves.
//
// Identity is deliberately split in two:
//
//   - Label is what makes the object recognisable and reads inside a sentence
//     ("Cancelled Blinky rev B"). It never repeats context the sentence already
//     supplies.
//   - Qualifier is the short detail a dense row adds beside the label to tell
//     two similar objects apart ("×10"). Prose omits it; cells render it.
package objectref

// Kind is a linkable object type. These strings are a wire contract: they are
// the remark directive names in note markdown (`:order[label]{#uuid}`), the
// activity_log.subject_type values, and the query parameters of the batch
// resolve endpoint. Don't rename one without migrating all three.
type Kind string

const (
	KindPart     Kind = "part"
	KindProject  Kind = "project"
	KindRevision Kind = "revision"
	KindOrder    Kind = "order"
	KindBuild    Kind = "build"
	KindStorage  Kind = "storage"
)

// Kinds is every resolvable kind, in the order they read best in a mixed list.
var Kinds = []Kind{KindPart, KindProject, KindRevision, KindOrder, KindBuild, KindStorage}

// Valid reports whether s names a resolvable kind.
func Valid(s string) bool {
	for _, k := range Kinds {
		if string(k) == s {
			return true
		}
	}
	return false
}

// Summary is a render-ready reference to one object: enough to draw an inline
// ref or a table cell without knowing the object's type. The server computes it
// so every client renders the same identity.
type Summary struct {
	Kind      Kind   `json:"kind"`
	ID        string `json:"id"`
	Label     string `json:"label"`
	Qualifier string `json:"qualifier,omitempty"`
	Status    string `json:"status,omitempty"`
	Href      string `json:"href,omitempty"`
}

// Href is the frontend route for an object, or "" when the kind has no page.
// A revision lives under its project, so parentID must be the project id;
// without it the revision is unlinkable.
func Href(kind Kind, teamID, id, parentID string) string {
	base := "/teams/" + teamID
	switch kind {
	case KindPart:
		return base + "/parts/" + id
	case KindProject:
		return base + "/projects/" + id
	case KindRevision:
		if parentID == "" {
			return ""
		}
		return base + "/projects/" + parentID + "/revisions/" + id
	case KindOrder:
		return base + "/orders/" + id
	case KindBuild:
		return base + "/builds/" + id
	case KindStorage:
		return base + "/storage/" + id
	}
	return ""
}
