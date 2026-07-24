package objectref

import "strings"

// Canonical human handles for the entities an activity row can point at.
//
// A row's subject_label is a *snapshot* taken at write time, so these run on the
// write path and the stored text stays audit-correct even if the entity is
// renamed later. They are the single definition of "how do we identify an X to a
// user" — the same handles belong on detail pages and list rows, so a reader
// recognises the thing in the feed as the thing they clicked.
//
// A missing label is not fatal: the frontend falls back to a generic "the order"
// / "the build", which is exactly the unhelpful text these exist to eliminate.

// OrderLabel identifies an order by distributor plus the most specific reference
// the team has given it. Orders carry no per-team sequence number, so the last
// resort is the short id — the same handle the order detail page uses as its
// title, which keeps the feed and that page cross-referenceable.
//
//	DigiKey #PO-1042     purchase order number
//	DigiKey #WEB-88213   the distributor's own reference
//	DigiKey 0f3a9c21     neither recorded
func OrderLabel(distributor string, purchaseOrderNumber, externalReference *string, orderID string) string {
	distributor = strings.TrimSpace(distributor)

	ref := strings.TrimSpace(derefStr(purchaseOrderNumber))
	if ref == "" {
		ref = strings.TrimSpace(derefStr(externalReference))
	}
	if ref != "" {
		// Don't double a '#' the user already typed into the PO field.
		return strings.TrimSpace(distributor + " #" + strings.TrimPrefix(ref, "#"))
	}

	if short := shortID(orderID); short != "" {
		return strings.TrimSpace(distributor + " " + short)
	}
	return distributor
}

// RevisionLabel identifies a BOM revision in the context of its project. A build
// is identified the same way — a build is an assembly of exactly one revision —
// so both flow through here and read consistently in the log. Builds have no
// number of their own, so two builds of one revision share a label; the link
// still resolves to the specific build.
//
//	Blinky rev B
//	Blinky v1.2   ("rev"/"v" already on the label is not doubled)
func RevisionLabel(projectName, revisionLabel string) string {
	projectName = strings.TrimSpace(projectName)
	revisionLabel = strings.TrimSpace(revisionLabel)

	if revisionLabel == "" {
		return projectName
	}
	// Revision labels come from the BOM file and are inconsistent: "B", "v1.2",
	// "Rev C". Only prefix "rev" when the label doesn't already say so.
	lower := strings.ToLower(revisionLabel)
	if !strings.HasPrefix(lower, "rev") && !strings.HasPrefix(lower, "v") {
		revisionLabel = "rev " + revisionLabel
	}
	if projectName == "" {
		return revisionLabel
	}
	return projectName + " " + revisionLabel
}

// PartLabel identifies a part by MPN, falling back to its short description for
// generic/parametric parts that have no MPN.
func PartLabel(mpn, shortDescription *string) string {
	if s := strings.TrimSpace(derefStr(mpn)); s != "" {
		return s
	}
	return strings.TrimSpace(derefStr(shortDescription))
}

// shortID returns the first group of a UUID ("0f3a9c21-…" → "0f3a9c21"),
// matching the 8-char handle the order detail page shows.
func shortID(id string) string {
	if i := strings.IndexByte(id, '-'); i > 0 {
		return id[:i]
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
