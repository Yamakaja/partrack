package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var searchFlags struct {
	mpn        string
	category   string
	pkg        string
	params     []string
	tolerance  float64
	stock      []string
	lifecycle  []string
	scope      string
	location   string
	fields     []string
	showParams []string
	withParams bool
	limit      int
	offset     int
	all        bool
	sortBy     string
	wide       bool
}

// pageLimit is the server's maximum page size; --all walks in these steps.
const pageLimit = 200

// allCap bounds --all so a bare `pt search --all` on a large catalog cannot walk
// forever. Anything beyond this is reported rather than silently dropped.
const allCap = 2000

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search the parts catalog and your inventory",
	Long: `Search parts by text, identity, category, package, stock state and parameters.

The free-text query matches MPN, description, manufacturer and aliases as a
substring — it is not value-aware, so a parametric search belongs in --param:

  pt search --category Resistors --param resistance=10k --param tolerance=1%
  pt search --category Capacitors --param capacitance=100n..1u --package 0603
  pt search STM32 --stock in
  pt search --stock low --fields mpn,stock,min,loc --sort stock

Rows are numbered so the next command can address them as @1, @2, ….`,
	Args: usageArgs(cobra.ArbitraryArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSearch(cmd.Context(), strings.Join(args, " "))
	},
}

func runSearch(ctx context.Context, query string) error {
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("team_id", teamID)
	if query != "" {
		q.Set("q", query)
	}
	if searchFlags.mpn != "" {
		q.Set("mpn", searchFlags.mpn)
	}
	if searchFlags.pkg != "" {
		q.Set("package", searchFlags.pkg)
	}
	if searchFlags.scope != "" {
		if searchFlags.scope != "global" && searchFlags.scope != "team" {
			return exitErrf(ExitUsage, "--scope must be global or team")
		}
		q.Set("scope", searchFlags.scope)
	}
	if v := normalizeStockStatus(searchFlags.stock); v != "" {
		q.Set("stock_status", v)
	}
	if v := normalizeLifecycle(searchFlags.lifecycle); v != "" {
		q.Set("lifecycle_status", v)
	}

	// Category resolution also decides whether parameter columns are available:
	// the server only attaches them when a category filter is present.
	var category *Category
	var filters []*paramFilter
	if searchFlags.category != "" || len(searchFlags.params) > 0 {
		rd, err := ap.loadRefData(ctx)
		if err != nil {
			return err
		}
		if searchFlags.category != "" {
			category, err = rd.resolveCategory(searchFlags.category)
			if err != nil {
				return err
			}
			q.Set("category_id", strconv.FormatInt(int64(category.ID), 10))
		}
		for _, spec := range splitParamSpecs(searchFlags.params) {
			f, err := parseParamSpec(spec, rd, searchFlags.tolerance)
			if err != nil {
				return err
			}
			f.apply(q)
			if warn := f.rangeCaveat(); warn != "" {
				fmt.Fprintf(os.Stderr, "pt: warning: %s\n", warn)
			}
			filters = append(filters, f)
		}
	}

	if searchFlags.location != "" {
		locID, _, err := ap.resolveLocation(ctx, teamID, searchFlags.location)
		if err != nil {
			return err
		}
		q.Set("storage_location_id", locID)
	}

	wantParams := searchFlags.withParams || len(searchFlags.showParams) > 0
	if wantParams && category == nil {
		return exitErrf(ExitUsage,
			"--params needs a --category (the server only returns parameter values for a category filter)")
	}

	limit := searchFlags.limit
	if limit <= 0 {
		limit = 50
	}
	if searchFlags.all {
		limit = pageLimit
	}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(searchFlags.offset))

	var resp PartListResponse
	if err := client.Get(ctx, "/parts", q, &resp); err != nil {
		return err
	}
	items := resp.Items

	if searchFlags.all {
		offset := searchFlags.offset + len(items)
		for int64(len(items)) < resp.Total && len(items) < allCap {
			q.Set("offset", strconv.Itoa(offset))
			var page PartListResponse
			if err := client.Get(ctx, "/parts", q, &page); err != nil {
				return err
			}
			if len(page.Items) == 0 {
				break
			}
			items = append(items, page.Items...)
			offset += len(page.Items)
		}
		if int64(len(items)) < resp.Total {
			fmt.Fprintf(os.Stderr, "pt: stopped at %d of %d matches (--all cap)\n", len(items), resp.Total)
		}
	}

	cols, err := resolveFields(pickFields(searchFlags.fields))
	if err != nil {
		return err
	}
	var paramCols []string
	if wantParams {
		paramCols = paramColumns(items, splitList(searchFlags.showParams))
	}

	if searchFlags.sortBy != "" {
		if err := sortItems(items, searchFlags.sortBy); err != nil {
			return err
		}
		if int64(len(items)) < resp.Total {
			fmt.Fprintf(os.Stderr,
				"pt: sorted the %d fetched rows of %d matches — add --all to sort the whole result set\n",
				len(items), resp.Total)
		}
	}

	// Record the result set before rendering, so @N works even if the caller
	// pipes stdout into something that closes early.
	idents := make([]PartIdent, len(items))
	for i, it := range items {
		idents[i] = partIdent(it)
	}
	ap.saveResults(teamID, idents)

	if ap.format == FormatJSON {
		rows := make([]map[string]any, len(items))
		for i, it := range items {
			row := jsonRow(it, cols, paramCols)
			row["n"] = i + 1
			row["uuid"] = it.ID
			rows[i] = row
		}
		return writeJSON(ap.out, map[string]any{
			"total":   resp.Total,
			"fetched": len(items),
			"items":   rows,
		})
	}

	if len(items) == 0 {
		fmt.Fprintln(ap.out, "No parts matched.")
		return nil
	}

	descWidth := 42
	if searchFlags.wide {
		descWidth = 0
	}
	rows := make([][]string, len(items))
	for i, it := range items {
		rows[i] = numberedRow(i+1, rowFor(it, cols, paramCols, descWidth))
	}
	writeTable(ap.out, numberedHeader(cols, paramCols), rows)
	fmt.Fprintln(ap.out, searchFooter(len(items), resp.Total, category, filters))
	return nil
}

// searchFooter summarises what was shown and, crucially, what a bare --param
// value actually became — a band, not an exact match.
func searchFooter(shown int, total int64, category *Category, filters []*paramFilter) string {
	var b strings.Builder
	if int64(shown) < total {
		fmt.Fprintf(&b, "%d of %d parts", shown, total)
	} else {
		fmt.Fprintf(&b, "%d part%s", shown, plural(shown))
	}
	if ap.teamName != "" {
		fmt.Fprintf(&b, " · team %s", ap.teamName)
	}
	if category != nil {
		fmt.Fprintf(&b, " · %s", category.DisplayName)
	}
	for _, f := range filters {
		fmt.Fprintf(&b, " · %s", f.describe())
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// pickFields returns the requested field list, or the default set.
func pickFields(flag []string) []string {
	if fields := splitList(flag); len(fields) > 0 {
		return fields
	}
	return defaultPartFields
}

// splitParamSpecs splits repeated --param flags. Unlike other list flags it does
// not split on commas, because enum filters legitimately contain them
// (mounting=SMD,THT is one filter with two values).
func splitParamSpecs(vals []string) []string {
	var out []string
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// normalizeStockStatus maps friendly names onto the server's stock_status set.
func normalizeStockStatus(vals []string) string {
	var out []string
	for _, v := range splitList(vals) {
		switch strings.ToLower(v) {
		case "in", "in_stock", "in-stock":
			out = append(out, "in_stock")
		case "low", "low_stock", "low-stock":
			out = append(out, "low_stock")
		case "out", "out_of_stock", "out-of-stock", "empty":
			out = append(out, "out_of_stock")
		case "untracked", "not_tracked", "not-tracked":
			out = append(out, "not_tracked")
		default:
			out = append(out, v)
		}
	}
	return strings.Join(out, ",")
}

// normalizeLifecycle maps friendly names onto the lifecycle enum.
func normalizeLifecycle(vals []string) string {
	var out []string
	for _, v := range splitList(vals) {
		switch strings.ToLower(v) {
		case "nla", "no-longer-available":
			out = append(out, "no_longer_available")
		default:
			out = append(out, strings.ToLower(strings.ReplaceAll(v, "-", "_")))
		}
	}
	return strings.Join(out, ",")
}

// sortItems orders the fetched rows client-side. The parts endpoint always
// sorts by MPN, so any other order has to happen here.
func sortItems(items []PartListItem, spec string) error {
	key, dir, _ := strings.Cut(spec, ":")
	descending := strings.EqualFold(dir, "desc")
	col, ok := lookupColumn(strings.ToLower(strings.TrimSpace(key)))
	if !ok {
		return exitErrf(ExitUsage, "unknown sort field %q — available: %s", key, availableFields())
	}

	// Numeric columns must not sort lexically: 9 belongs after 100.
	numeric := map[string]func(PartListItem) *int32{
		"stock":   func(p PartListItem) *int32 { return p.QuantityOnHand },
		"min":     func(p PartListItem) *int32 { return p.MinimumStockQuantity },
		"reorder": func(p PartListItem) *int32 { return p.ReorderQuantity },
	}
	if get, isNum := numeric[col.Key]; isNum {
		sort.SliceStable(items, func(i, j int) bool {
			a, b := get(items[i]), get(items[j])
			switch {
			case a == nil && b == nil:
				return false
			case a == nil: // absent values sort last regardless of direction
				return false
			case b == nil:
				return true
			}
			if descending {
				return *a > *b
			}
			return *a < *b
		})
		return nil
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := col.Value(items[i]), col.Value(items[j])
		if descending {
			return a > b
		}
		return a < b
	})
	return nil
}

func init() {
	f := searchCmd.Flags()
	f.StringVar(&searchFlags.mpn, "mpn", "", "exact MPN match")
	f.StringVarP(&searchFlags.category, "category", "c", "", "category by name, slug or id")
	f.StringVarP(&searchFlags.pkg, "package", "p", "", "package / footprint (exact)")
	f.StringArrayVar(&searchFlags.params, "param", nil,
		"parameter filter, repeatable: slug=10k | slug=9k..11k | slug='>=10k' | slug=A,B")
	f.Float64Var(&searchFlags.tolerance, "param-tolerance", defaultParamTolerancePct,
		"percent band applied to a bare --param value (0 for exact)")
	f.StringSliceVar(&searchFlags.stock, "stock", nil, "stock state: in, low, out, untracked")
	f.StringSliceVar(&searchFlags.lifecycle, "lifecycle", nil,
		"lifecycle: active, nrnd, obsolete, deprecated, nla")
	f.StringVar(&searchFlags.scope, "scope", "", "part scope: global or team")
	f.StringVarP(&searchFlags.location, "location", "l", "", "storage location by label or id (includes descendants)")
	f.StringSliceVar(&searchFlags.fields, "fields", nil,
		"columns to show, in order (default "+strings.Join(defaultPartFields, ",")+")")
	f.StringSliceVar(&searchFlags.showParams, "params", nil,
		"add parameter columns; bare adds all, or name a subset (needs --category)")
	f.IntVar(&searchFlags.limit, "limit", 50, "maximum rows to fetch (server caps a page at 200)")
	f.IntVar(&searchFlags.offset, "offset", 0, "row offset to start from")
	f.BoolVar(&searchFlags.all, "all", false, "fetch every matching page")
	f.StringVar(&searchFlags.sortBy, "sort", "", "sort fetched rows by a field, e.g. stock:desc")
	f.BoolVar(&searchFlags.wide, "wide", false, "do not truncate the description column")

	// `--params` with no value means "every parameter in the result set".
	f.Lookup("params").NoOptDefVal = " "
	searchCmd.PreRun = func(cmd *cobra.Command, args []string) {
		if cmd.Flags().Changed("params") {
			searchFlags.withParams = true
			searchFlags.showParams = splitList(searchFlags.showParams)
		}
	}
}
