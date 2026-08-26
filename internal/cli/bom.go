package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Yamakaja/partrack/internal/apitypes"
	"github.com/Yamakaja/partrack/internal/bomfile"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/spf13/cobra"
)

// bomCmd groups everything that operates on a BOM file. `check` is the only
// member today; importing a checked file is the next command to land here, which
// is why the mapping and item-building live in internal/bomfile rather than in
// this file — `check` already produces the exact payload an import would post.
var bomCmd = &cobra.Command{
	Use:   "bom",
	Short: "Work with BOM files",
	Long: `Commands that operate on a BOM file before it reaches partrack.

  pt bom check my-board.csv`,
}

var bomCheckFlags struct {
	delimiter    string
	comment      string
	noHeader     bool
	skip         int
	mappings     []string
	distributor  string
	problemsOnly bool
	limit        int
	resolve      bool
	project      string
}

var bomCheckCmd = &cobra.Command{
	Use:   "check <file>",
	Short: "Show how partrack will read a BOM file, before importing it",
	Long: `Parse a BOM the way the import wizard does and print what partrack will
make of it: which column became which role, which rows survive, and how each
line is interpreted — route, category, package and the parameters extracted from
its value.

This runs entirely locally. It needs no server, no team and no login, and it
imports nothing; it is a rehearsal of the parse so mistakes are found while they
are still cheap to fix.

Two things it is deliberately loud about, because the wizard does them silently:
rows dropped for want of a designator or a usable quantity, and SKUs with no
distributor to resolve them against.

  pt bom check board.csv
  pt bom check board.csv --problems
  pt bom check board.csv --map "Comment=value" --map "Designator=designators"
  pt bom check board.csv --delimiter ';' --skip 3
  pt bom check board.csv --format json | jq '.rows[] | select(.route == "unknown")'

Add --resolve to ask the server what the import would actually resolve each line
to. That part needs a login and a team, and consults your catalog:

  pt bom check board.csv --resolve
  pt bom check board.csv --resolve --project "Rev C Mainboard"`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBOMCheck(cmd.Context(), args[0])
	},
}

// bomRow is one line of the file, paired with what the parser made of it. The
// raw inputs travel alongside the reading so the two can be compared at a
// glance — that comparison is the entire point of the command.
type bomRow struct {
	Line      int    `json:"line"`
	Refdes    string `json:"refdes"`
	Quantity  int32  `json:"quantity"`
	Value     string `json:"value,omitempty"`
	Footprint string `json:"footprint,omitempty"`
	MPN       string `json:"mpn,omitempty"`
	SKU       string `json:"sku,omitempty"`

	genspec.Interpretation
}

// buildBOMRows runs the parser over every importable item, pairing each reading
// with the raw cells it came from.
//
// The dereference here matters more than it looks: strOr(_, "") and not str().
// str renders an em-dash for an absent value, which is right for a table and
// catastrophic as parser input — an em-dash in the MPN field is a non-empty
// identity hint, so genspec routes the row to identity resolution and never
// looks at the value at all.
func buildBOMRows(build bomfile.Build) []bomRow {
	rows := make([]bomRow, len(build.Items))
	for i, it := range build.Items {
		value, footprint := strOr(it.Value, ""), strOr(it.Footprint, "")
		mpn, sku := strOr(it.MPN, ""), strOr(it.SKU, "")
		rows[i] = bomRow{
			Line:      build.ItemLines[i],
			Refdes:    it.ReferenceDesignators,
			Quantity:  it.Quantity,
			Value:     value,
			Footprint: footprint,
			MPN:       mpn,
			SKU:       sku,
			Interpretation: genspec.Interpret(genspec.Input{
				Refdes:    it.ReferenceDesignators,
				Footprint: footprint,
				Value:     value,
				MPN:       mpn,
				SKU:       sku,
			}),
		}
	}
	return rows
}

// attention reports whether a row is one the review gate would put at the top:
// nothing usable was extracted, the parse left doubt, or the designator and the
// footprint disagree about what the part is.
func (r bomRow) attention() bool {
	return r.Route == genspec.RouteUnknown || r.Escalate || len(r.Flags) > 0
}

func runBOMCheck(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return exitErrf(ExitNotFound, "%v", err)
	}
	defer f.Close() //nolint:errcheck

	opts := bomfile.ParseOptions{NoHeader: bomCheckFlags.noHeader, SkipLines: bomCheckFlags.skip}
	if opts.Delimiter, err = singleRune("--delimiter", bomCheckFlags.delimiter); err != nil {
		return err
	}
	if opts.Comment, err = singleRune("--comment", bomCheckFlags.comment); err != nil {
		return err
	}

	sheet, err := bomfile.Parse(f, opts)
	if err != nil {
		return exitErrf(ExitUsage, "%v", err)
	}

	roles := bomfile.AutoMapHeaders(sheet.Headers)
	if roles, err = applyMappings(sheet, roles, bomCheckFlags.mappings); err != nil {
		return err
	}

	// The name only: offline there is no id to resolve it to, and a stand-in id
	// would put a resolution that never happened into the import payload.
	buildOpts := bomfile.BuildOptions{
		SingleDistributorName: strings.TrimSpace(bomCheckFlags.distributor),
	}

	build := bomfile.BuildItems(sheet, roles, buildOpts)

	rows := buildBOMRows(build)

	// The forecast is a separate, server-side question: everything above is a
	// parse, this is what the catalog makes of it.
	var forecast *apitypes.BOMResolvePreview
	var projectName string
	if bomCheckFlags.resolve {
		if len(build.Items) == 0 {
			return exitErrf(ExitUsage, "no importable rows to resolve")
		}
		var err error
		if forecast, projectName, err = resolveForecast(ctx, build.Items, bomCheckFlags.project); err != nil {
			return err
		}
	}

	if ap.format == FormatJSON {
		return writeJSON(ap.out, map[string]any{
			"file":        path,
			"delimiter":   string(sheet.Delimiter),
			"sniffed":     sheet.Sniffed,
			"columns":     columnMapJSON(sheet, roles),
			"rows_parsed": len(sheet.Rows),
			"imported":    len(build.Items),
			"dropped":     build.Dropped,
			"findings":    build.Findings,
			"routes":      routeCounts(rows),
			"rows":        selectRows(rows),
			// The payload an import would post, so a caller can diff it or keep it.
			"items": build.Items,
			// Absent unless --resolve was given; never an empty object, so a
			// caller can tell "not asked" from "asked and nothing resolves".
			"forecast": forecast,
		})
	}
	printBOMCheck(path, sheet, roles, build, rows)
	if forecast != nil {
		printForecast(forecast, projectName, rows)
	}
	return nil
}

func printBOMCheck(path string, sheet *bomfile.Sheet, roles []bomfile.ColumnRole, build bomfile.Build, rows []bomRow) {
	delim := string(sheet.Delimiter)
	if sheet.Delimiter == '\t' {
		delim = "tab"
	}
	if sheet.Sniffed {
		delim += " (sniffed)"
	}
	writeKV(ap.out, [][2]string{
		{"File", path},
		{"Delimiter", delim},
		{"Rows", fmt.Sprintf("%d parsed · %d importable · %d dropped", len(sheet.Rows), len(build.Items), build.Dropped)},
	})

	writeSection(ap.out, "COLUMNS")
	colRows := make([][]string, 0, len(sheet.Headers))
	for i, h := range sheet.Headers {
		role := bomfile.RoleIgnore
		if i < len(roles) {
			role = roles[i]
		}
		shown := string(role)
		if role == bomfile.RoleIgnore {
			shown = dash
		}
		colRows = append(colRows, []string{strconv.Itoa(i + 1), h, shown})
	}
	writeTable(ap.out, []string{"#", "HEADER", "ROLE"}, colRows)

	if len(build.Findings) > 0 {
		writeSection(ap.out, "FINDINGS")
		fRows := make([][]string, 0, len(build.Findings))
		for _, f := range build.Findings {
			where := dash
			if f.Line > 0 {
				where = "line " + strconv.Itoa(f.Line)
			}
			fRows = append(fRows, []string{string(f.Severity), where, f.Message})
		}
		writeTable(ap.out, []string{"SEVERITY", "WHERE", "WHAT"}, fRows)
	}

	writeSection(ap.out, "HOW PARTRACK READS IT")
	counts := routeCounts(rows)
	rRows := make([][]string, 0, 4)
	for _, route := range []string{"identity", "parametric", "ignore", "unknown"} {
		if n := counts[route]; n > 0 {
			rRows = append(rRows, []string{route, strconv.Itoa(n), routeMeaning(route)})
		}
	}
	writeTable(ap.out, []string{"ROUTE", "ROWS", "MEANING"}, rRows)

	shown := selectRows(rows)
	if len(shown) > 0 {
		writeSection(ap.out, "ROWS")
		dRows := make([][]string, 0, len(shown))
		for _, r := range shown {
			dRows = append(dRows, []string{
				strconv.Itoa(r.Line),
				r.Refdes,
				firstNonEmpty(r.Value, r.MPN, r.SKU, dash),
				truncate(firstNonEmpty(r.Footprint, dash), 24),
				string(r.Route),
				strOrDash(r.Category),
				strOrDash(r.PackageValue()),
				formatParams(r),
				rowMarkers(r),
			})
		}
		writeTable(ap.out, []string{"LINE", "REFDES", "VALUE", "FOOTPRINT", "ROUTE", "CATEGORY", "PKG", "PARAMS", ""}, dRows)
	}

	printBOMLegend(rows, shown)
}

// printBOMLegend explains the markers and states the limits of the check, so a
// clean run is not mistaken for a guarantee that every line will resolve.
func printBOMLegend(all, shown []bomRow) {
	var attention, escalate int
	for _, r := range all {
		if r.attention() {
			attention++
		}
		if r.Escalate {
			escalate++
		}
	}
	fmt.Fprintln(ap.out)
	if bomCheckFlags.problemsOnly && len(shown) < len(all) {
		fmt.Fprintf(ap.out, "Showing %d of %d rows; drop --problems for the rest.\n", len(shown), len(all))
	}
	if attention > 0 {
		noun := "rows"
		if attention == 1 {
			noun = "row"
		}
		fmt.Fprintf(ap.out, "! marks the %d %s the review gate will put first.\n", attention, noun)
	}
	if escalate > 0 {
		noun := "rows whose values"
		if escalate == 1 {
			noun = "row whose value"
		}
		fmt.Fprintf(ap.out,
			"~ marks %d %s did not parse cleanly. If the server has an LLM\n"+
				"  configured, it re-reads these during import; otherwise they need a hand.\n", escalate, noun)
	}
	fmt.Fprint(ap.out,
		"\nThis is the parse only. Whether a line resolves to a part also depends on\n"+
			"your catalog, which this command does not consult.\n")
}

// selectRows applies --problems and --limit, and orders what remains
// attention-first — the same ordering the review gate uses, so the rows worth
// arguing with are the ones on screen.
func selectRows(rows []bomRow) []bomRow {
	out := make([]bomRow, 0, len(rows))
	for _, r := range rows {
		if bomCheckFlags.problemsOnly && !r.attention() {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].attention() != out[j].attention() {
			return out[i].attention()
		}
		return out[i].Line < out[j].Line
	})
	if bomCheckFlags.limit > 0 && len(out) > bomCheckFlags.limit {
		out = out[:bomCheckFlags.limit]
	}
	return out
}

func routeCounts(rows []bomRow) map[string]int {
	counts := map[string]int{}
	for _, r := range rows {
		counts[string(r.Route)]++
	}
	return counts
}

func routeMeaning(route string) string {
	switch route {
	case "identity":
		return "resolved by MPN or SKU against a specific part"
	case "parametric":
		return "matched against the catalog by category, package and value"
	case "ignore":
		return "not procurable (test point, mounting hole, DNP)"
	case "unknown":
		return "nothing reliable extracted — will need review"
	}
	return ""
}

// paramUnits gives the display unit for the secondary parameters genspec
// recognises. The primary magnitude takes its unit from the class profile
// instead; these are the ones a bare number would otherwise misrepresent — a
// voltage rating of "16" reads very differently from "16 V".
var paramUnits = map[string]string{
	"tolerance":      "%",
	"voltage_rating": "V",
	"current_rating": "A",
	"power_rating":   "W",
	"frequency":      "Hz",
}

// formatParams renders the extracted constraints the way a datasheet would, so
// a misparse is visible without decoding SI floats. The package parameter is
// omitted: it has its own column.
func formatParams(r bomRow) string {
	var parts []string
	for _, p := range r.Params {
		if p.Slug == genspec.PackageSlug {
			continue
		}
		if p.ValueSI != nil {
			unit := paramUnits[p.Slug]
			if prof, ok := genspec.ProfileForClass(r.Class); ok && p.Slug == prof.ValueParam {
				unit = prof.UnitSymbol
			}
			parts = append(parts, p.Slug+"="+paramparse.FormatEngineering(*p.ValueSI, unit))
			continue
		}
		parts = append(parts, p.Slug+"="+p.Raw)
	}
	if len(parts) == 0 {
		return dash
	}
	return strings.Join(parts, " ")
}

// rowMarkers flags a row for attention and, when the parser left tokens it could
// not attribute, names them — residue is usually the clue to what was misread.
func rowMarkers(r bomRow) string {
	var m []string
	if r.attention() {
		m = append(m, "!")
	}
	if r.Escalate {
		m = append(m, "~")
	}
	m = append(m, r.Flags...)
	if len(r.Residue) > 0 {
		m = append(m, "residue:"+strings.Join(r.Residue, ","))
	}
	if len(m) == 0 {
		return ""
	}
	return strings.Join(m, " ")
}

// applyMappings overrides the auto-detected roles from --map. A column may be
// named by header text or by 1-based position, so a file with duplicate or empty
// headers is still addressable.
func applyMappings(sheet *bomfile.Sheet, roles []bomfile.ColumnRole, specs []string) ([]bomfile.ColumnRole, error) {
	for _, spec := range splitParamSpecs(specs) {
		col, roleName, ok := strings.Cut(spec, "=")
		if !ok {
			return nil, exitErrf(ExitUsage, "bad --map %q: expected <column>=<role>", spec)
		}
		col, roleName = strings.TrimSpace(col), strings.TrimSpace(roleName)

		role := bomfile.ColumnRole(strings.ToLower(roleName))
		if !validRole(role) {
			return nil, exitErrf(ExitUsage, "unknown role %q in --map %q; roles are %s",
				roleName, spec, strings.Join(roleNames(), ", "))
		}

		idx := -1
		if n, err := strconv.Atoi(col); err == nil {
			if n < 1 || n > len(sheet.Headers) {
				return nil, exitErrf(ExitUsage, "--map %q: column %d is out of range (the file has %d)", spec, n, len(sheet.Headers))
			}
			idx = n - 1
		} else {
			for i, h := range sheet.Headers {
				if strings.EqualFold(strings.TrimSpace(h), col) {
					idx = i
					break
				}
			}
		}
		if idx < 0 {
			return nil, exitErrf(ExitNotFound, "--map %q: no column named %q (headers: %s)",
				spec, col, strings.Join(sheet.Headers, ", "))
		}
		roles[idx] = role
	}
	return roles, nil
}

func validRole(r bomfile.ColumnRole) bool {
	for _, known := range bomfile.AllRoles {
		if r == known {
			return true
		}
	}
	return false
}

func roleNames() []string {
	out := make([]string, len(bomfile.AllRoles))
	for i, r := range bomfile.AllRoles {
		out[i] = string(r)
	}
	return out
}

func columnMapJSON(sheet *bomfile.Sheet, roles []bomfile.ColumnRole) []map[string]any {
	out := make([]map[string]any, 0, len(sheet.Headers))
	for i, h := range sheet.Headers {
		role := bomfile.RoleIgnore
		if i < len(roles) {
			role = roles[i]
		}
		out = append(out, map[string]any{"index": i + 1, "header": h, "role": string(role)})
	}
	return out
}

// singleRune reads a one-character flag, accepting "tab" for the character that
// cannot be typed into a shell argument.
func singleRune(flag, s string) (rune, error) {
	switch {
	case s == "":
		return 0, nil
	case strings.EqualFold(s, "tab") || s == `\t`:
		return '\t', nil
	}
	r := []rune(s)
	if len(r) != 1 {
		return 0, exitErrf(ExitUsage, "%s takes a single character, got %q", flag, s)
	}
	return r[0], nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func strOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return dash
	}
	return s
}

func init() {
	f := bomCheckCmd.Flags()
	f.StringVar(&bomCheckFlags.delimiter, "delimiter", "", "field delimiter, or \"tab\" (default: sniff)")
	f.StringVar(&bomCheckFlags.comment, "comment", "", "skip lines starting with this character")
	f.BoolVar(&bomCheckFlags.noHeader, "no-header", false, "the first row is data, not column names")
	f.IntVar(&bomCheckFlags.skip, "skip", 0, "drop N lines from the top before parsing")
	f.StringArrayVar(&bomCheckFlags.mappings, "map", nil, "override a column's role: <header|number>=<role>, repeatable")
	f.StringVar(&bomCheckFlags.distributor, "distributor", "", "assume this distributor for SKU rows with no distributor column")
	f.BoolVar(&bomCheckFlags.problemsOnly, "problems", false, "list only the rows that need attention")
	f.IntVar(&bomCheckFlags.limit, "limit", 0, "maximum rows to list (0 for all)")
	f.BoolVar(&bomCheckFlags.resolve, "resolve", false, "also ask the server what each line would resolve to")
	f.StringVar(&bomCheckFlags.project, "project", "", "project to resolve against, by name or id (enables prior-revision reuse)")

	bomCmd.AddCommand(bomCheckCmd)
}
