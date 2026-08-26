// Package bomfile turns a BOM file into the payload partrack's import endpoint
// accepts, and reports what it had to assume along the way.
//
// It is the Go counterpart of Stage 1 of the import flow (doc/bom-import.md §4),
// which until now lived only in the import wizard
// (web/src/lib/components/bom/bom-import-utils.ts): parse the delimited text,
// map each column to a role, and fold the cells into items. The output type is
// apitypes.BOMImportItem — the same declaration the handler decodes — so what is
// checked here is literally what gets posted.
//
// The wizard silently drops rows it cannot use: its final filter discards any
// row with no reference designators or a non-positive quantity, and a blank or
// malformed quantity cell therefore removes the line from the BOM with nothing
// said. Reproducing that behaviour is required (an import must be predicted, not
// improved on), but staying silent about it is not — so every drop, every
// coerced quantity, and every structural doubt comes back as a Finding.
//
// The package is deliberately free of DB, network, and genspec dependencies:
// parsing a file and deciding what its columns mean needs none of them.
package bomfile

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/Yamakaja/partrack/internal/apitypes"
)

// ColumnRole is what a spreadsheet column means to the importer. The roles are
// resolution-strategy selectors: mapping a column to `sku` asks for identity
// resolution against a distributor, mapping `value` + `footprint` asks for a
// parametric read. They mirror ColumnRole in bom-import-utils.ts.
type ColumnRole string

const (
	RoleIgnore       ColumnRole = "ignore"
	RoleDesignators  ColumnRole = "designators"
	RoleQuantity     ColumnRole = "quantity"
	RoleMPN          ColumnRole = "mpn"
	RoleDescription  ColumnRole = "description"
	RoleManufacturer ColumnRole = "manufacturer"
	RoleName         ColumnRole = "name"
	RoleSKU          ColumnRole = "sku"
	RoleDistributor  ColumnRole = "distributor"
	RoleValue        ColumnRole = "value"
	RoleFootprint    ColumnRole = "footprint"
)

// AllRoles lists every assignable role, for flag validation and help text.
var AllRoles = []ColumnRole{
	RoleIgnore, RoleDesignators, RoleQuantity, RoleMPN, RoleDescription,
	RoleManufacturer, RoleName, RoleSKU, RoleDistributor, RoleValue, RoleFootprint,
}

// autoMap recognises common header spellings, keyed by lowercased header text.
// It is a port of AUTO_MAP in bom-import-utils.ts and must stay in step with it:
// the wizard and the CLI disagreeing about what a column means would defeat the
// point of checking a BOM before importing it.
var autoMap = map[string]ColumnRole{
	"ref":                        RoleDesignators,
	"reference":                  RoleDesignators,
	"refdes":                     RoleDesignators,
	"designator":                 RoleDesignators,
	"designators":                RoleDesignators,
	"refs":                       RoleDesignators,
	"qty":                        RoleQuantity,
	"quantity":                   RoleQuantity,
	"count":                      RoleQuantity,
	"mpn":                        RoleMPN,
	"manufacturer part number":   RoleMPN,
	"manufacturer part number 1": RoleMPN,
	"part number":                RoleMPN,
	"partnumber":                 RoleMPN,
	"part_number":                RoleMPN,
	"description":                RoleDescription,
	"desc":                       RoleDescription,
	// The EDA "Value" field is the parametric value (10k, 100nF X7R), distinct
	// from a free-text description.
	"value":                  RoleValue,
	"val":                    RoleValue,
	"footprint":              RoleFootprint,
	"footprint name":         RoleFootprint,
	"package":                RoleFootprint,
	"package / case":         RoleFootprint,
	"manufacturer":           RoleManufacturer,
	"manufacturer 1":         RoleManufacturer,
	"mfr":                    RoleManufacturer,
	"mfg":                    RoleManufacturer,
	"manufacturer name":      RoleManufacturer,
	"name":                   RoleName,
	"sku":                    RoleSKU,
	"distributor sku":        RoleSKU,
	"supplier sku":           RoleSKU,
	"distributor":            RoleDistributor,
	"distributor name":       RoleDistributor,
	"vendor":                 RoleDistributor,
	"supplier":               RoleDistributor,
	"supplier 1":             RoleDistributor,
	"supplier part number 1": RoleSKU,
}

// AutoMapHeader returns the role a header name auto-maps to, or RoleIgnore.
func AutoMapHeader(header string) ColumnRole {
	if r, ok := autoMap[strings.ToLower(strings.TrimSpace(header))]; ok {
		return r
	}
	return RoleIgnore
}

// AutoMapHeaders maps a whole header row, as the wizard does on load.
func AutoMapHeaders(headers []string) []ColumnRole {
	roles := make([]ColumnRole, len(headers))
	for i, h := range headers {
		roles[i] = AutoMapHeader(h)
	}
	return roles
}

// ParseOptions mirrors the wizard's parse step. The zero value sniffs the
// delimiter, treats the first row as a header, and has no comment character —
// the same defaults the dialog opens with.
type ParseOptions struct {
	// Delimiter is the field separator; zero means sniff it from the content.
	Delimiter rune
	// Comment, when non-zero, skips lines whose first character is it.
	Comment rune
	// NoHeader treats the first row as data instead of column names.
	NoHeader bool
	// SkipLines drops N lines from the top before parsing, for files that open
	// with a title block.
	SkipLines int
}

// Sheet is a parsed delimited file. Line numbers are 1-based positions in the
// original file, so a finding can point at something the user can actually go
// and look at.
type Sheet struct {
	Headers   []string
	Rows      [][]string
	RowLines  []int // source line of each row in Rows
	Delimiter rune
	Sniffed   bool // Delimiter was guessed rather than given
}

// candidateDelimiters are the separators worth guessing between, in the order
// papaparse tries them.
var candidateDelimiters = []rune{',', '\t', '|', ';', '\x1e', '\x1f'}

// Parse reads a delimited BOM file.
//
// Quoting is handled leniently (csv.LazyQuotes): BOMs exported from EDA tools
// carry stray quotes inside descriptions ("1/8\" spacer"), and rejecting the
// file outright is a worse answer than reading it the way the importer will.
// Ragged rows are kept for the same reason — a short row is a finding, not a
// parse error.
func Parse(r io.Reader, opts ParseOptions) (*Sheet, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(raw), "\ufeff") // Excel writes a BOM byte-order mark

	skipped := 0
	if opts.SkipLines > 0 {
		lines := strings.Split(text, "\n")
		if opts.SkipLines >= len(lines) {
			return nil, fmt.Errorf("--skip %d removes the whole file (%d lines)", opts.SkipLines, len(lines))
		}
		skipped = opts.SkipLines
		text = strings.Join(lines[opts.SkipLines:], "\n")
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("the file has no content to parse")
	}

	delim, sniffed := opts.Delimiter, false
	if delim == 0 {
		delim, sniffed = sniffDelimiter(text, opts.Comment), true
	}

	records, lines, err := readAll(text, delim, opts.Comment)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("no rows parsed — check the delimiter and comment options")
	}

	sheet := &Sheet{Delimiter: delim, Sniffed: sniffed}
	for i := range lines {
		lines[i] += skipped
	}
	if opts.NoHeader {
		sheet.Rows, sheet.RowLines = records, lines
		// Positional names, so --map can still address columns.
		sheet.Headers = make([]string, widestRow(records))
		for i := range sheet.Headers {
			sheet.Headers[i] = fmt.Sprintf("column %d", i+1)
		}
		return sheet, nil
	}
	sheet.Headers = records[0]
	sheet.Rows, sheet.RowLines = records[1:], lines[1:]
	return sheet, nil
}

// readAll parses every record, recording the source line of each. Records are
// read one at a time so FieldPos can report the true line — a quoted field
// spanning several lines would otherwise put every later number out by that
// much.
func readAll(text string, delim, comment rune) ([][]string, []int, error) {
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = delim
	cr.Comment = comment
	cr.LazyQuotes = true
	cr.FieldsPerRecord = -1 // ragged rows are a finding, not a failure
	cr.ReuseRecord = false

	var records [][]string
	var lines []int
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", len(lines)+1, err)
		}
		line := len(lines) + 1
		if len(rec) > 0 {
			line, _ = cr.FieldPos(0)
		}
		records = append(records, rec)
		lines = append(lines, line)
	}
	return records, lines, nil
}

// sniffDelimiter picks the separator that yields the most consistent row width,
// preferring more columns to break ties. A file that parses as one column under
// every candidate falls back to a comma, which is what the wizard shows too.
func sniffDelimiter(text string, comment rune) rune {
	best, bestScore, bestWidth := ',', -1.0, 0
	for _, d := range candidateDelimiters {
		records, _, err := readAll(text, d, comment)
		if err != nil || len(records) == 0 {
			continue
		}
		if len(records) > 10 {
			records = records[:10]
		}
		width := len(records[0])
		if width < 2 {
			continue
		}
		same := 0
		for _, r := range records {
			if len(r) == width {
				same++
			}
		}
		score := float64(same) / float64(len(records))
		if score > bestScore || (score == bestScore && width > bestWidth) {
			best, bestScore, bestWidth = d, score, width
		}
	}
	return best
}

func widestRow(records [][]string) int {
	w := 0
	for _, r := range records {
		if len(r) > w {
			w = len(r)
		}
	}
	return w
}

// Severity ranks a finding by what it means for the import.
type Severity string

const (
	// SeverityError means the import will not do what the user intends —
	// a row will be dropped, or a required column is missing.
	SeverityError Severity = "error"
	// SeverityWarning means the import will proceed but something is likely
	// mis-read, or a resolution tier will not fire.
	SeverityWarning Severity = "warning"
	// SeverityInfo is an assumption worth confirming.
	SeverityInfo Severity = "info"
)

// Finding is one thing worth telling the user about their file.
type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	// Line is the 1-based source line, or 0 for a file-wide finding.
	Line int `json:"line,omitempty"`
}

// Finding codes, stable enough to grep for or match on in a script.
const (
	CodeMissingRole      = "missing_role"
	CodeDuplicateRole    = "duplicate_role"
	CodeUnmappedColumn   = "unmapped_column"
	CodeRowDropped       = "row_dropped"
	CodeQuantityCoerced  = "quantity_coerced"
	CodeDesignatorCount  = "designator_count"
	CodeShortRow         = "short_row"
	CodeSKUNoDistributor = "sku_without_distributor"
	CodeNoIdentity       = "no_identity_columns"
)

// BuildOptions carries the distributor decisions the wizard collects in its
// third step.
type BuildOptions struct {
	// DistributorIDs maps a distributor name as written in the file to a
	// partrack distributor id.
	DistributorIDs map[string]string
	// SingleDistributorID is stamped onto every SKU-bearing row when the file
	// has no distributor column, which is the only way such rows can resolve by
	// SKU.
	//
	// The name may be given without the id — an offline check knows which
	// distributor the user intends but cannot look up its id. The name is then
	// recorded on the row and the SKU warning softens to a note, but no id is
	// invented: the payload must never claim a resolution that has not happened.
	SingleDistributorID   string
	SingleDistributorName string
}

// Build is the result of mapping a sheet into import items.
type Build struct {
	Items    []apitypes.BOMImportItem `json:"items"`
	Findings []Finding                `json:"findings"`
	// ItemLines is the source line of each item in Items, index-aligned.
	ItemLines []int `json:"item_lines"`
	// Dropped counts rows the importer will discard.
	Dropped int `json:"dropped"`
}

// BuildItems folds the sheet into import items using the given column roles.
//
// The mapping and the drop rule reproduce buildItems() in bom-import-utils.ts
// exactly, including its quantity coercion, because the job here is to predict
// the import rather than to perform a better one. Everything it does silently is
// reported as a Finding.
func BuildItems(sheet *Sheet, roles []ColumnRole, opts BuildOptions) Build {
	var b Build
	b.Findings = append(b.Findings, checkRoles(sheet, roles)...)

	idx := func(role ColumnRole) int {
		for i, r := range roles {
			if r == role {
				return i
			}
		}
		return -1
	}
	desigIdx, qtyIdx := idx(RoleDesignators), idx(RoleQuantity)
	descIdx, mfrIdx := idx(RoleDescription), idx(RoleManufacturer)
	mpnIdx, nameIdx := idx(RoleMPN), idx(RoleName)
	skuIdx, distIdx := idx(RoleSKU), idx(RoleDistributor)
	valueIdx, fpIdx := idx(RoleValue), idx(RoleFootprint)

	cell := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	optional := func(row []string, i int) *string {
		if v := cell(row, i); v != "" {
			return &v
		}
		return nil
	}

	skuRowsWithoutDistributor := 0

	for n, row := range sheet.Rows {
		line := sheet.RowLines[n]

		if len(row) < len(sheet.Headers) {
			b.Findings = append(b.Findings, Finding{
				Severity: SeverityWarning, Code: CodeShortRow, Line: line,
				Message: fmt.Sprintf("row has %d columns, the header has %d — later columns read as empty", len(row), len(sheet.Headers)),
			})
		}

		refdes := cell(row, desigIdx)
		rawQty := cell(row, qtyIdx)
		qty, qtyOK := parseQuantity(rawQty)

		// The wizard's filter: no designators or a non-positive quantity and the
		// row never reaches the server.
		if refdes == "" || qty <= 0 {
			b.Dropped++
			b.Findings = append(b.Findings, Finding{
				Severity: SeverityError, Code: CodeRowDropped, Line: line,
				Message: dropReason(refdes, rawQty, qty, qtyOK, desigIdx, qtyIdx),
			})
			continue
		}
		if qtyOK && !isCleanInteger(rawQty) {
			b.Findings = append(b.Findings, Finding{
				Severity: SeverityWarning, Code: CodeQuantityCoerced, Line: line,
				Message: fmt.Sprintf("quantity %q is read as %d — the importer takes the leading integer and ignores the rest", rawQty, qty),
			})
		}
		if n := countDesignators(refdes); n > 1 && int32(n) != qty {
			b.Findings = append(b.Findings, Finding{
				Severity: SeverityWarning, Code: CodeDesignatorCount, Line: line,
				Message: fmt.Sprintf("%d reference designators (%s) but quantity %d", n, refdes, qty),
			})
		}

		sku := optional(row, skuIdx)

		var distributorName, resolvedDistributorID *string
		if name := cell(row, distIdx); name != "" {
			distributorName = &name
			if id, ok := opts.DistributorIDs[name]; ok && id != "" {
				resolvedDistributorID = &id
			}
		} else if sku != nil && opts.SingleDistributorName != "" {
			// No distributor column: the wizard stamps the one chosen
			// distributor onto every SKU row so the SKU tier can fire.
			name := opts.SingleDistributorName
			distributorName = &name
			if opts.SingleDistributorID != "" {
				id := opts.SingleDistributorID
				resolvedDistributorID = &id
			}
		}
		if sku != nil && resolvedDistributorID == nil {
			skuRowsWithoutDistributor++
		}

		b.Items = append(b.Items, apitypes.BOMImportItem{
			ReferenceDesignators:  refdes,
			Quantity:              qty,
			RawDescription:        optional(row, descIdx),
			Manufacturer:          optional(row, mfrIdx),
			MPN:                   optional(row, mpnIdx),
			Name:                  optional(row, nameIdx),
			SKU:                   sku,
			DistributorName:       distributorName,
			ResolvedDistributorID: resolvedDistributorID,
			Value:                 optional(row, valueIdx),
			Footprint:             optional(row, fpIdx),
		})
		b.ItemLines = append(b.ItemLines, line)
	}

	if skuRowsWithoutDistributor > 0 {
		f := Finding{
			Severity: SeverityWarning, Code: CodeSKUNoDistributor,
			Message: fmt.Sprintf(
				"%s a SKU but no distributor — the import resolves a SKU only against a known distributor, so these fall through to the MPN and parametric tiers",
				plural(skuRowsWithoutDistributor, "row carries", "rows carry")),
		}
		// An intended distributor with no id is the offline case: the wizard will
		// supply the id, so this is a note about what to do, not a defect.
		if opts.SingleDistributorName != "" && opts.SingleDistributorID == "" {
			f.Severity, f.Message = SeverityInfo, fmt.Sprintf(
				"%s a SKU and will resolve against %q once it is chosen in the import wizard's distributor step",
				plural(skuRowsWithoutDistributor, "row carries", "rows carry"), opts.SingleDistributorName)
		}
		b.Findings = append(b.Findings, f)
	}
	if mpnIdx < 0 && skuIdx < 0 && len(b.Items) > 0 {
		b.Findings = append(b.Findings, Finding{
			Severity: SeverityInfo, Code: CodeNoIdentity,
			Message: "no MPN or SKU column — every row resolves parametrically, against the catalog's existing parts",
		})
	}

	sortFindings(b.Findings)
	return b
}

// checkRoles validates the column mapping itself.
func checkRoles(sheet *Sheet, roles []ColumnRole) []Finding {
	var out []Finding
	seen := map[ColumnRole][]string{}
	for i, r := range roles {
		if r == RoleIgnore {
			continue
		}
		name := fmt.Sprintf("column %d", i+1)
		if i < len(sheet.Headers) && sheet.Headers[i] != "" {
			name = sheet.Headers[i]
		}
		seen[r] = append(seen[r], name)
	}
	for _, role := range AllRoles {
		if cols := seen[role]; len(cols) > 1 {
			out = append(out, Finding{
				Severity: SeverityError, Code: CodeDuplicateRole,
				Message: fmt.Sprintf("role %q is mapped to %d columns (%s) — only the first is used",
					role, len(cols), strings.Join(cols, ", ")),
			})
		}
	}
	for _, role := range []ColumnRole{RoleDesignators, RoleQuantity} {
		if len(seen[role]) == 0 {
			out = append(out, Finding{
				Severity: SeverityError, Code: CodeMissingRole,
				Message: fmt.Sprintf("no column is mapped to %q — every row will be dropped", role),
			})
		}
	}

	// Columns nothing recognised are the quiet failure: Altium names its value
	// column "Comment", which auto-maps to nothing, and a BOM imported that way
	// loses every passive's value with no error anywhere.
	var ignored []string
	for i, r := range roles {
		if r != RoleIgnore || i >= len(sheet.Headers) {
			continue
		}
		if h := strings.TrimSpace(sheet.Headers[i]); h != "" {
			ignored = append(ignored, h)
		}
	}
	if len(ignored) > 0 {
		out = append(out, Finding{
			Severity: SeverityInfo, Code: CodeUnmappedColumn,
			Message: fmt.Sprintf("%s not mapped and will be ignored: %s",
				plural(len(ignored), "column is", "columns are"), strings.Join(ignored, ", ")),
		})
	}
	return out
}

// parseQuantity reproduces JavaScript's parseInt, which is what the wizard uses:
// it takes an optional sign and the leading run of digits and ignores whatever
// follows, so "1,000" is 1 and "3 pcs" is 3. The bool reports whether any digits
// were found at all (NaN in the browser, which the wizard's filter then drops).
func parseQuantity(s string) (int32, bool) {
	s = strings.TrimSpace(s)
	i, neg := 0, false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0, false
	}
	n, err := strconv.ParseInt(s[start:i], 10, 32)
	if err != nil {
		return 0, false // overflows int32; treated as unusable
	}
	if neg {
		n = -n
	}
	return int32(n), true
}

// isCleanInteger reports whether the cell is exactly an integer, so a coerced
// read can be distinguished from an ordinary one.
func isCleanInteger(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	_, err := strconv.ParseInt(s, 10, 32)
	return err == nil
}

// countDesignators counts reference designators in a cell. It is a heuristic —
// separators vary by tool — so it is only ever used to raise a question, never
// to change what is imported.
func countDesignators(s string) int {
	n := 0
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if strings.TrimSpace(f) != "" {
			n++
		}
	}
	return n
}

// dropReason explains a discarded row in the terms the user can act on.
func dropReason(refdes, rawQty string, qty int32, qtyOK bool, desigIdx, qtyIdx int) string {
	switch {
	case desigIdx < 0:
		return "dropped: no designators column is mapped"
	case refdes == "" && qtyIdx < 0:
		return "dropped: no reference designators, and no quantity column is mapped"
	case refdes == "":
		return "dropped: no reference designators"
	case qtyIdx < 0:
		return "dropped: no quantity column is mapped"
	case rawQty == "":
		return "dropped: quantity cell is empty"
	case !qtyOK:
		return fmt.Sprintf("dropped: quantity %q is not a number", rawQty)
	default:
		return fmt.Sprintf("dropped: quantity is %d", qty)
	}
}

// sortFindings orders findings by severity then source line, so the output leads
// with what breaks the import.
func sortFindings(f []Finding) {
	rank := map[Severity]int{SeverityError: 0, SeverityWarning: 1, SeverityInfo: 2}
	sort.SliceStable(f, func(i, j int) bool {
		if rank[f[i].Severity] != rank[f[j].Severity] {
			return rank[f[i].Severity] < rank[f[j].Severity]
		}
		return f[i].Line < f[j].Line
	})
}

// InterpretItems converts import items into the narrower shape
// POST /bom/interpret-preview accepts, attaching each row's original cells so
// the server's LLM fallback can see the columns the mapping discarded.
func InterpretItems(items []apitypes.BOMImportItem, sheet *Sheet, itemLines []int) []apitypes.BOMInterpretItem {
	byLine := map[int]map[string]string{}
	if sheet != nil {
		for n, row := range sheet.Rows {
			cells := map[string]string{}
			for i, h := range sheet.Headers {
				if v := strings.TrimSpace(valueAt(row, i)); h != "" && v != "" {
					cells[h] = v
				}
			}
			if len(cells) > 0 {
				byLine[sheet.RowLines[n]] = cells
			}
		}
	}
	out := make([]apitypes.BOMInterpretItem, len(items))
	for i, it := range items {
		out[i] = apitypes.BOMInterpretItem{
			Refdes:    it.ReferenceDesignators,
			Footprint: deref(it.Footprint),
			Value:     deref(it.Value),
			Mpn:       deref(it.MPN),
			Sku:       deref(it.SKU),
		}
		if i < len(itemLines) {
			out[i].Row = byLine[itemLines[i]]
		}
	}
	return out
}

func valueAt(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// plural renders a count with the right verb agreement, so a report of a single
// problem does not read as though it were written by a machine.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
