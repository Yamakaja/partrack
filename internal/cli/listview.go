package cli

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Helpers shared by the list and detail views of the workflow objects — orders,
// builds, the cart, checkouts and projects. They exist so those commands agree
// on how a date, a count footer and a distributor reference are rendered.

// shortDate reduces an RFC3339 timestamp to its date. Every list view here is
// answering "when, roughly", and the time of day only costs columns.
func shortDate(ts string) string {
	if ts == "" {
		return dash
	}
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// shortDatePtr is shortDate for a nullable timestamp.
func shortDatePtr(ts *string) string {
	if ts == nil {
		return dash
	}
	return shortDate(*ts)
}

// countFooter states what was shown against what exists, so a truncated listing
// never reads like the whole set.
func countFooter(shown int, total int64, noun string) string {
	if int64(shown) < total {
		return fmt.Sprintf("%d of %d %ss", shown, total, noun)
	}
	if shown == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", shown, noun)
}

// cloneValues copies a query so a caller can vary one key across several
// requests without mutating the shared base.
func cloneValues(q url.Values) url.Values {
	out := make(url.Values, len(q))
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// sortByCreatedDesc restores newest-first order after rows from several requests
// have been merged. RFC3339 sorts lexically, so no parsing is needed.
func sortByCreatedDesc[T any](items []T, createdAt func(T) string) {
	sort.SliceStable(items, func(i, j int) bool {
		return createdAt(items[i]) > createdAt(items[j])
	})
}

// printFlatMap renders the named keys of a decoded JSON object as key/value
// lines, skipping any the server did not send. Used for the handful of responses
// that are genuinely open-ended — distributor status being the only one so far.
func printFlatMap(m map[string]any, keys []string) {
	var pairs [][2]string
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		pairs = append(pairs, [2]string{
			strings.ToUpper(k[:1]) + strings.ReplaceAll(k[1:], "_", " "),
			fmt.Sprintf("%v", v),
		})
	}
	if len(pairs) == 0 {
		fmt.Fprintln(ap.out, "Nothing reported.")
		return
	}
	writeKV(ap.out, pairs)
}

// distributorRef is the subset of GET /distributors this client needs.
type distributorRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// resolveDistributor turns a distributor name or id into an id. The list is
// short and global, so it is fetched rather than cached.
func (a *app) resolveDistributor(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if uuidRe.MatchString(ref) {
		return strings.ToLower(ref), nil
	}
	client, err := a.requireAuthedClient()
	if err != nil {
		return "", err
	}
	var resp List[distributorRef]
	if err := client.Get(ctx, "/distributors", nil, &resp); err != nil {
		return "", err
	}
	d, err := pickOne(ref, "distributor", resp.Items,
		func(d distributorRef) matchRank { return rankText(d.Name, ref) },
		func(d distributorRef) string { return fmt.Sprintf("%-8s %s", shortID(d.ID), d.Name) })
	if err != nil {
		return "", err
	}
	return d.ID, nil
}
