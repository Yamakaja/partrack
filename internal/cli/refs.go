package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Addressing objects other than parts.
//
// Every team-scoped object resolves the same way: @N against the last listing,
// a full UUID, a hex id prefix from the recent roll, then — for the kinds that
// have something human to match on — a text search. Anything that matches more
// than one object lists the candidates and exits 5, so a script never acts on an
// object the user did not name.

// matchRank ranks a candidate against a reference. An exact match wins outright;
// substring hits are only consulted when nothing matched exactly.
type matchRank int

const (
	matchNone matchRank = iota
	matchPartial
	matchExact
)

// rankText is the common text comparison: equal beats contains, both
// case-insensitively, and an empty candidate never matches.
func rankText(candidate, ref string) matchRank {
	if candidate == "" {
		return matchNone
	}
	c, r := strings.ToLower(candidate), strings.ToLower(ref)
	switch {
	case c == r:
		return matchExact
	case strings.Contains(c, r):
		return matchPartial
	default:
		return matchNone
	}
}

// pickOne applies the house resolution rule to a candidate set. describe renders
// one row of the ambiguity list, which always leads with a short id so the user
// can retry with something unambiguous.
func pickOne[T any](ref, kind string, items []T, rank func(T) matchRank, describe func(T) string) (T, error) {
	var zero T
	var exact, partial []T
	for _, it := range items {
		switch rank(it) {
		case matchExact:
			exact = append(exact, it)
		case matchPartial:
			partial = append(partial, it)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}

	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		if cmd := listCommandFor(kind); cmd != "" {
			return zero, exitErrf(ExitNotFound,
				"no %s matching %q — list them with `%s`", kind, ref, cmd)
		}
		return zero, exitErrf(ExitNotFound, "no %s matching %q", kind, ref)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d %ss:", ref, len(candidates), kind)
		for i, c := range candidates {
			fmt.Fprintf(&b, "\n  %s", describe(c))
			if i == 9 && len(candidates) > 10 {
				fmt.Fprintf(&b, "\n  … and %d more", len(candidates)-10)
				break
			}
		}
		b.WriteString("\nnarrow it down, or use an id prefix from the first column")
		return zero, exitErrf(ExitAmbiguous, "%s", b.String())
	}
}

// resolveRef runs the addressing ladder that precedes any text search. byText is
// consulted only when the reference is none of the three forms that identify an
// object outright; passing nil declares that the kind has nothing to search by.
func (a *app) resolveRef(ctx context.Context, kind, ref string,
	byText func(context.Context, string) (string, error),
) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", exitErrf(ExitUsage, "empty %s reference", kind)
	}
	if strings.HasPrefix(ref, "@") {
		return a.resolveResultRef(ref, kind)
	}
	if uuidRe.MatchString(ref) {
		return strings.ToLower(ref), nil
	}
	if hexPrefixRe.MatchString(ref) {
		if id, err := a.resolvePrefix(ref, kind); err != nil || id != "" {
			return id, err
		}
		// Not a known prefix — fall through, since a reference can legitimately
		// look like hex (a revision label, say) without being an id.
	}
	if byText == nil {
		return "", exitErrf(ExitNotFound,
			"%q is not a %s id — %ss have no name to search by, so address one as @N from `%s` or by an id prefix",
			ref, kind, kind, listCommandFor(kind))
	}
	return byText(ctx, ref)
}

// --- Projects ------------------------------------------------------------------

// listProjects fetches the team's projects. The endpoint pages, but a team's
// project list is small enough to resolve against in one request.
func (a *app) listProjects(ctx context.Context, teamID string) ([]ProjectListItem, error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("limit", "200")
	var list List[ProjectListItem]
	if err := client.Get(ctx, "/teams/"+teamID+"/projects", q, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// resolveProjectRef turns a project reference into the list row, which carries
// the active revision id every BOM command needs next.
func (a *app) resolveProjectRef(ctx context.Context, teamID, ref string) (*ProjectListItem, error) {
	projects, err := a.listProjects(ctx, teamID)
	if err != nil {
		return nil, err
	}
	byID := func(id string) (*ProjectListItem, error) {
		for i := range projects {
			if strings.EqualFold(projects[i].ID, id) {
				a.remember(teamID, projectRef(projects[i]))
				return &projects[i], nil
			}
		}
		return nil, exitErrf(ExitNotFound, "no project with id %s", shortID(id))
	}

	id, err := a.resolveRef(ctx, kindProject, ref, func(context.Context, string) (string, error) {
		p, err := pickOne(ref, kindProject, projects,
			func(p ProjectListItem) matchRank {
				// An id that is not UUID-shaped never reached the ladder's uuid
				// branch, so match it here rather than only by name.
				if strings.EqualFold(p.ID, ref) {
					return matchExact
				}
				return rankText(p.Name, ref)
			},
			func(p ProjectListItem) string {
				return fmt.Sprintf("%-8s %-30s %s", shortID(p.ID), p.Name, p.Status)
			})
		if err != nil {
			return "", err
		}
		return p.ID, nil
	})
	if err != nil {
		return nil, err
	}
	return byID(id)
}

func projectRef(p ProjectListItem) Ref {
	return Ref{Kind: kindProject, ID: p.ID, Label: p.Name}
}

// --- Orders --------------------------------------------------------------------

func (a *app) resolveOrderRef(ctx context.Context, teamID, ref string) (string, error) {
	return a.resolveRef(ctx, kindOrder, ref, func(ctx context.Context, ref string) (string, error) {
		orders, err := a.fetchOrders(ctx, teamID, url.Values{"limit": {"200"}})
		if err != nil {
			return "", err
		}
		o, err := pickOne(ref, kindOrder, orders.Items,
			func(o OrderListItem) matchRank {
				if strings.EqualFold(o.ID, ref) {
					return matchExact
				}
				// The distributor name is the coarsest handle and must not beat a
				// reference the user actually typed off an order.
				if r := rankText(strOr(o.ExternalReference, ""), ref); r != matchNone {
					return r
				}
				if rankText(o.DistributorName, ref) != matchNone {
					return matchPartial
				}
				return matchNone
			},
			func(o OrderListItem) string {
				return fmt.Sprintf("%-8s %-16s %-12s %s", shortID(o.ID),
					o.DistributorName, o.Status, strOr(o.ExternalReference, dash))
			})
		if err != nil {
			return "", err
		}
		a.remember(teamID, orderRef(o))
		return o.ID, nil
	})
}

func orderRef(o OrderListItem) Ref {
	return Ref{Kind: kindOrder, ID: o.ID, Label: strOr(o.ExternalReference, o.DistributorName)}
}

// --- Builds --------------------------------------------------------------------

func (a *app) resolveBuildRef(ctx context.Context, teamID, ref string) (string, error) {
	return a.resolveRef(ctx, kindBuild, ref, func(ctx context.Context, ref string) (string, error) {
		builds, err := a.fetchBuilds(ctx, teamID, url.Values{"limit": {"200"}})
		if err != nil {
			return "", err
		}
		b, err := pickOne(ref, kindBuild, builds.Items,
			func(b BuildListItem) matchRank {
				if strings.EqualFold(b.ID, ref) {
					return matchExact
				}
				if r := rankText(b.ProjectName, ref); r != matchNone {
					return r
				}
				return rankText(b.BomRevisionLabel, ref)
			},
			func(b BuildListItem) string {
				return fmt.Sprintf("%-8s %-24s %-10s ×%-5d %s", shortID(b.ID),
					b.ProjectName, b.BomRevisionLabel, b.Quantity, b.Status)
			})
		if err != nil {
			return "", err
		}
		a.remember(teamID, buildRef(b))
		return b.ID, nil
	})
}

func buildRef(b BuildListItem) Ref {
	return Ref{Kind: kindBuild, ID: b.ID, Label: b.ProjectName + " " + b.BomRevisionLabel}
}

// --- Checkouts -----------------------------------------------------------------

// resolveCheckoutRef takes no text form: a checkout has a timestamp and a reason,
// neither of which identifies one. @N and id prefixes are the whole vocabulary.
func (a *app) resolveCheckoutRef(ctx context.Context, ref string) (string, error) {
	return a.resolveRef(ctx, kindCheckout, ref, nil)
}

func checkoutRef(c CheckoutListItem) Ref {
	return Ref{Kind: kindCheckout, ID: c.ID, Label: c.PerformedAt}
}
