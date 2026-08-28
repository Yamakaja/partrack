package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// uuidRe matches a canonical UUID, the form every partrack id takes.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// hexPrefixRe matches a bare hex run, the shape of an abbreviated id.
var hexPrefixRe = regexp.MustCompile(`^[0-9a-fA-F]{4,}$`)

// minPrefixLen is the shortest id prefix accepted. Four hex digits is enough to
// be unambiguous within a result set while staying easy to retype.
const minPrefixLen = 4

// listTeams fetches the caller's teams. Like every list endpoint, /teams wraps
// its rows in the standard {total, limit, offset, items} envelope rather than
// returning a bare array.
func (a *app) listTeams(ctx context.Context) ([]Team, error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	var resp struct {
		Total int64  `json:"total"`
		Items []Team `json:"items"`
	}
	if err := client.Get(ctx, "/teams", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// teamID resolves the configured team reference (a name or a UUID) to an id,
// caching the answer for the process. Everything team-scoped funnels through
// here, so an unset or unknown team fails once with a useful message.
func (a *app) teamID(ctx context.Context) (string, error) {
	if a.teamIDCache != "" {
		return a.teamIDCache, nil
	}
	teams, err := a.listTeams(ctx)
	if err != nil {
		return "", err
	}
	if len(teams) == 0 {
		return "", exitErrf(ExitNotFound, "you are not a member of any team")
	}

	ref := strings.TrimSpace(a.cfg.Team)
	if ref == "" {
		if len(teams) == 1 {
			a.teamIDCache, a.teamName = teams[0].ID, teams[0].Name
			return a.teamIDCache, nil
		}
		var b strings.Builder
		b.WriteString("no team selected — set one with `pt config set team <name>` or pass --team:")
		for _, t := range teams {
			fmt.Fprintf(&b, "\n  %s (%s)", t.Name, t.Role)
		}
		return "", exitErrf(ExitUsage, "%s", b.String())
	}

	if uuidRe.MatchString(ref) {
		for _, t := range teams {
			if strings.EqualFold(t.ID, ref) {
				a.teamIDCache, a.teamName = t.ID, t.Name
				return a.teamIDCache, nil
			}
		}
		return "", exitErrf(ExitNotFound, "no team with id %s", ref)
	}

	lower := strings.ToLower(ref)
	var matches []Team
	for _, t := range teams {
		if strings.EqualFold(t.Name, ref) {
			a.teamIDCache, a.teamName = t.ID, t.Name
			return a.teamIDCache, nil
		}
		if strings.Contains(strings.ToLower(t.Name), lower) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return "", exitErrf(ExitNotFound, "no team matching %q — list them with `pt teams`", ref)
	case 1:
		a.teamIDCache, a.teamName = matches[0].ID, matches[0].Name
		return a.teamIDCache, nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d teams:", ref, len(matches))
		for _, t := range matches {
			fmt.Fprintf(&b, "\n  %s", t.Name)
		}
		return "", exitErrf(ExitAmbiguous, "%s", b.String())
	}
}

// resolvePart turns a user-supplied part reference into a part id. The order is
// cheapest-and-most-certain first:
//
//	@N          row N of the last result set
//	<uuid>      a full id, used as-is
//	<hexprefix> an id prefix, matched against recently seen parts
//	<mpn>       an exact MPN, via ?mpn=
//	<text>      a substring search, via ?q= — resolved only when it matches one part
//
// Anything matching several parts prints the candidates and exits 5, so a script
// never acts on a part the user did not name.
func (a *app) resolvePart(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", exitErrf(ExitUsage, "empty part reference")
	}

	if strings.HasPrefix(ref, "@") {
		return a.resolveResultRef(ref, kindPart)
	}
	if uuidRe.MatchString(ref) {
		return strings.ToLower(ref), nil
	}
	if hexPrefixRe.MatchString(ref) {
		if id, err := a.resolvePrefix(ref, kindPart); err != nil || id != "" {
			return id, err
		}
		// Not a known prefix — fall through and try it as an MPN, since plenty
		// of real MPNs are all-hex (e.g. "0402", "1206").
	}
	return a.resolveByText(ctx, ref)
}

// resolveResultRef handles the @N form against the saved result set. The kind
// is checked, so `pt orders show @1` right after a `pt search` says what went
// wrong instead of fetching an order by a part's id.
func (a *app) resolveResultRef(ref, kind string) (string, error) {
	n, err := strconv.Atoi(ref[1:])
	if err != nil || n < 1 {
		return "", exitErrf(ExitUsage, "bad reference %q: expected @1, @2, … from the last listing", ref)
	}
	s := a.loadState()
	if len(s.Results) > 0 && s.ResultKind != kind {
		return "", exitErrf(ExitUsage,
			"the last listing was %ss, so %s is not a %s — run `%s` first",
			s.ResultKind, ref, kind, listCommandFor(kind))
	}
	if len(s.Results) == 0 {
		// A search that matched nothing still clears the result set, since @N
		// must never point into a stale one. Saying "run pt search first" to
		// someone who just did is worse than useless, so the two are separated.
		if !s.SavedAt.IsZero() {
			return "", exitErrf(ExitNotFound, "the last listing matched nothing, so %s refers to nothing", ref)
		}
		return "", exitErrf(ExitNotFound, "no saved result set — run `%s` first", listCommandFor(kind))
	}
	if n > len(s.Results) {
		return "", exitErrf(ExitNotFound, "%s is out of range — the last listing returned %d rows", ref, len(s.Results))
	}
	if s.Results[n-1].ID == "" {
		// A listing can hold rows that resolved to nothing — an unresolved BOM
		// line keeps its slot so the numbering matches what was printed.
		label := s.Results[n-1].Label
		if label == "" {
			label = "that row"
		}
		return "", exitErrf(ExitNotFound,
			"%s (%s) did not resolve to a %s, so there is nothing to look up", ref, label, kind)
	}
	return s.Results[n-1].ID, nil
}

// resolvePrefix matches an abbreviated id against recently seen parts. It
// returns an empty id (and no error) when nothing matches, so the caller can
// fall back to treating the input as an MPN.
func (a *app) resolvePrefix(ref, kind string) (string, error) {
	if len(ref) < minPrefixLen {
		return "", nil
	}
	lower := strings.ToLower(ref)
	s := a.loadState()
	var matches []Ref
	for _, p := range s.Recent {
		if p.Kind == kind && strings.HasPrefix(strings.ToLower(p.ID), lower) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0].ID, nil
	default:
		return "", ambiguousIdents(ref, matches)
	}
}

// resolveByText tries an exact MPN match, then a substring search.
func (a *app) resolveByText(ctx context.Context, ref string) (string, error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return "", err
	}
	teamID, err := a.teamID(ctx)
	if err != nil {
		return "", err
	}

	// Exact MPN. The server compares with =, so this is the precise path.
	q := url.Values{}
	q.Set("team_id", teamID)
	q.Set("mpn", ref)
	q.Set("limit", "25")
	var exact PartListResponse
	if err := client.Get(ctx, "/parts", q, &exact); err != nil {
		return "", err
	}
	if len(exact.Items) == 1 {
		a.remember(teamID, partIdent(exact.Items[0]))
		return exact.Items[0].ID, nil
	}
	if len(exact.Items) > 1 {
		return "", ambiguousItems(ref, exact.Items, exact.Total)
	}

	// Substring search over MPN, description, manufacturer and aliases.
	q = url.Values{}
	q.Set("team_id", teamID)
	q.Set("q", ref)
	q.Set("limit", "25")
	var fuzzy PartListResponse
	if err := client.Get(ctx, "/parts", q, &fuzzy); err != nil {
		return "", err
	}
	switch {
	case len(fuzzy.Items) == 0:
		return "", exitErrf(ExitNotFound, "no part matching %q", ref)
	case len(fuzzy.Items) == 1:
		item := fuzzy.Items[0]
		// A substring hit is a guess, so say which part was chosen — on stderr,
		// where it cannot corrupt piped output.
		fmt.Fprintf(os.Stderr, "pt: %q → %s\n", ref, strOr(item.Mpn, item.ID))
		a.remember(teamID, partIdent(item))
		return item.ID, nil
	default:
		return "", ambiguousItems(ref, fuzzy.Items, fuzzy.Total)
	}
}

// ambiguousItems formats the candidate list for a reference that matched more
// than one part.
func ambiguousItems(ref string, items []PartListItem, total int64) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d parts:", ref, total)
	for i, it := range items {
		fmt.Fprintf(&b, "\n  %-8s %-28s %s",
			shortID(it.ID), strOr(it.Mpn, dash), strOr(it.Manufacturer, dash))
		if i == 9 && len(items) > 10 {
			fmt.Fprintf(&b, "\n  … and %d more", len(items)-10)
			break
		}
	}
	b.WriteString("\nnarrow it down, or use an id prefix from the first column")
	return exitErrf(ExitAmbiguous, "%s", b.String())
}

// ambiguousIdents is the same for locally cached identities.
func ambiguousIdents(ref string, idents []Ref) error {
	var b strings.Builder
	fmt.Fprintf(&b, "id prefix %q matches %d known %ss:", ref, len(idents), idents[0].Kind)
	for _, p := range idents {
		fmt.Fprintf(&b, "\n  %-8s %s", shortID(p.ID), p.Label)
	}
	return exitErrf(ExitAmbiguous, "%s", b.String())
}

// listCommandFor names the command that refreshes the @N set for a kind, so an
// addressing failure ends with the fix rather than the diagnosis. It returns ""
// for a kind with no listing command, and callers drop the hint rather than
// naming a command that does not exist.
func listCommandFor(kind string) string {
	switch kind {
	case kindPart:
		return "pt search"
	case kindProject, kindOrder, kindBuild, kindCheckout:
		return "pt " + kind + "s list"
	default:
		return ""
	}
}

// shortID abbreviates a UUID to the prefix length the tool advertises.
func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

// fetchPart loads the full detail for a resolved part and records its identity.
func (a *app) fetchPart(ctx context.Context, partID, teamID string) (*PartDetail, error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("team_id", teamID)
	var detail PartDetail
	if err := client.Get(ctx, "/parts/"+partID, q, &detail); err != nil {
		return nil, err
	}
	a.remember(teamID, detailIdent(&detail))
	return &detail, nil
}

// resolveAndFetch is the common opening move for every part-scoped command.
func (a *app) resolveAndFetch(ctx context.Context, ref string) (*PartDetail, string, error) {
	teamID, err := a.teamID(ctx)
	if err != nil {
		return nil, "", err
	}
	partID, err := a.resolvePart(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	detail, err := a.fetchPart(ctx, partID, teamID)
	if err != nil {
		return nil, "", err
	}
	return detail, teamID, nil
}
