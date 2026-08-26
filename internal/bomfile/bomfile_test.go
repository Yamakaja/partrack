package bomfile

import (
	"strings"
	"testing"
)

// parse is a test helper: parse text with the given options or fail.
func parse(t *testing.T, text string, opts ParseOptions) *Sheet {
	t.Helper()
	sheet, err := Parse(strings.NewReader(text), opts)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return sheet
}

// findings of a given code, for assertions.
func withCode(f []Finding, code string) []Finding {
	var out []Finding
	for _, x := range f {
		if x.Code == code {
			out = append(out, x)
		}
	}
	return out
}

// The wizard's quantity cell goes through JavaScript's parseInt, which takes the
// leading digits and ignores the rest. A Go-native parse would reject "1,000"
// where the browser silently imports 1, so the semantics have to be copied
// exactly — this is the difference between predicting the import and describing
// a different one.
func TestParseQuantityMatchesJavaScript(t *testing.T) {
	tests := []struct {
		in   string
		want int32
		ok   bool
	}{
		{"3", 3, true},
		{" 4 ", 4, true},
		{"1,000", 1, true}, // parseInt stops at the comma
		{"3 pcs", 3, true},
		{"0", 0, true},
		{"-2", -2, true},
		{"", 0, false},
		{"abc", 0, false},
		{"n/a", 0, false},
		{"1.9", 1, true}, // parseInt truncates at the dot
	}
	for _, tc := range tests {
		got, ok := parseQuantity(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseQuantity(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// A row with no designators or a non-positive quantity never reaches the server:
// the wizard filters it out after mapping. The filter must be reproduced, and
// each dropped row reported — a silently vanishing line is the friction this
// whole command exists to remove.
func TestRowsTheWizardDropsAreReported(t *testing.T) {
	sheet := parse(t, strings.Join([]string{
		"Ref,Qty,Value,Footprint",
		"R1,1,10k,R_0603_1608Metric",
		"R2,,4k7,R_0603_1608Metric", // empty quantity
		",1,100n,C_0402",            // no designators
		"R4,0,1k,R_0603_1608Metric", // zero quantity
		"R5,n/a,2k2,R_0603",         // unparseable quantity
	}, "\n"), ParseOptions{})

	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})

	if len(b.Items) != 1 {
		t.Fatalf("kept %d items, want 1: %+v", len(b.Items), b.Items)
	}
	if b.Items[0].ReferenceDesignators != "R1" {
		t.Errorf("kept the wrong row: %+v", b.Items[0])
	}
	if b.Dropped != 4 {
		t.Errorf("Dropped = %d, want 4", b.Dropped)
	}

	drops := withCode(b.Findings, CodeRowDropped)
	if len(drops) != 4 {
		t.Fatalf("got %d drop findings, want 4: %+v", len(drops), drops)
	}
	// Each drop must point at the real line in the file and say why.
	wantLines := map[int]string{3: "empty", 4: "no reference designators", 5: "quantity is 0", 6: "not a number"}
	for _, d := range drops {
		frag, ok := wantLines[d.Line]
		if !ok {
			t.Errorf("unexpected drop at line %d: %s", d.Line, d.Message)
			continue
		}
		if !strings.Contains(d.Message, frag) {
			t.Errorf("line %d: %q does not mention %q", d.Line, d.Message, frag)
		}
		if d.Severity != SeverityError {
			t.Errorf("a dropped row should be an error, got %s", d.Severity)
		}
	}
}

// A quantity the importer coerces still imports — but as a different number than
// the file says, which the user needs told.
func TestCoercedQuantityIsFlaggedButKept(t *testing.T) {
	sheet := parse(t, "Ref,Qty\nR1,\"1,000\"\nR2,5\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})

	if len(b.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(b.Items))
	}
	if b.Items[0].Quantity != 1 {
		t.Errorf("quantity = %d, want 1 (parseInt semantics)", b.Items[0].Quantity)
	}
	coerced := withCode(b.Findings, CodeQuantityCoerced)
	if len(coerced) != 1 {
		t.Fatalf("got %d coercion findings, want 1: %+v", len(coerced), coerced)
	}
	if coerced[0].Line != 2 {
		t.Errorf("coercion reported at line %d, want 2", coerced[0].Line)
	}
	// A clean integer must not be flagged.
	if strings.Contains(coerced[0].Message, "\"5\"") {
		t.Errorf("a clean quantity was flagged: %s", coerced[0].Message)
	}
}

// The SKU tier requires a distributor id. A BOM with SKUs and no distributor
// column resolves nothing by SKU, which looks like a partrack failure unless it
// is called out before the import.
func TestSKUWithoutDistributorIsFlagged(t *testing.T) {
	text := "Ref,Qty,SKU\nR1,1,311-10.0KHRCT-ND\nR2,1,311-4.7KHRCT-ND\n"
	sheet := parse(t, text, ParseOptions{})
	roles := AutoMapHeaders(sheet.Headers)

	b := BuildItems(sheet, roles, BuildOptions{})
	f := withCode(b.Findings, CodeSKUNoDistributor)
	if len(f) != 1 {
		t.Fatalf("expected the SKU/distributor warning: %+v", b.Findings)
	}
	if !strings.Contains(f[0].Message, "2 rows") {
		t.Errorf("finding should count the affected rows: %s", f[0].Message)
	}
	for _, it := range b.Items {
		if it.ResolvedDistributorID != nil {
			t.Errorf("no distributor was chosen, so none should be stamped: %+v", it)
		}
	}

	// Choosing one distributor stamps every SKU row and clears the warning.
	b = BuildItems(sheet, roles, BuildOptions{SingleDistributorID: "dist-1", SingleDistributorName: "DigiKey"})
	if len(withCode(b.Findings, CodeSKUNoDistributor)) != 0 {
		t.Errorf("warning should be gone once a distributor is chosen: %+v", b.Findings)
	}
	for _, it := range b.Items {
		if it.ResolvedDistributorID == nil || *it.ResolvedDistributorID != "dist-1" {
			t.Errorf("SKU row was not stamped with the chosen distributor: %+v", it)
		}
	}
}

// A distributor column resolves per-name, and a name absent from the mapping
// leaves the row without an id — the same as the wizard, which sends null.
func TestDistributorColumnMapsPerName(t *testing.T) {
	sheet := parse(t, "Ref,Qty,SKU,Supplier\nR1,1,ABC,DigiKey\nR2,1,DEF,Farnell\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{
		DistributorIDs: map[string]string{"DigiKey": "dk-id"},
	})
	if len(b.Items) != 2 {
		t.Fatalf("got %d items", len(b.Items))
	}
	if b.Items[0].ResolvedDistributorID == nil || *b.Items[0].ResolvedDistributorID != "dk-id" {
		t.Errorf("mapped distributor not applied: %+v", b.Items[0])
	}
	if b.Items[1].ResolvedDistributorID != nil {
		t.Errorf("unmapped distributor should leave no id: %+v", b.Items[1])
	}
	if b.Items[1].DistributorName == nil || *b.Items[1].DistributorName != "Farnell" {
		t.Errorf("the written name is kept even when unmapped: %+v", b.Items[1])
	}
}

func TestDesignatorCountMismatchIsFlagged(t *testing.T) {
	sheet := parse(t, "Ref,Qty\n\"R1,R2,R3\",3\n\"C1,C2\",5\nR9,1\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})

	f := withCode(b.Findings, CodeDesignatorCount)
	if len(f) != 1 {
		t.Fatalf("got %d mismatch findings, want 1: %+v", len(f), f)
	}
	if f[0].Line != 3 {
		t.Errorf("mismatch reported at line %d, want 3", f[0].Line)
	}
}

func TestMissingAndDuplicateRoles(t *testing.T) {
	sheet := parse(t, "Ref,Reference,Value\nR1,R1,10k\n", ParseOptions{})
	roles := AutoMapHeaders(sheet.Headers)

	b := BuildItems(sheet, roles, BuildOptions{})
	if len(withCode(b.Findings, CodeDuplicateRole)) != 1 {
		t.Errorf("two designator columns should be flagged: %+v", b.Findings)
	}
	if len(withCode(b.Findings, CodeMissingRole)) != 1 {
		t.Errorf("a missing quantity column should be flagged: %+v", b.Findings)
	}
	// With no quantity column every row drops, which must also be said.
	if b.Dropped != 1 {
		t.Errorf("Dropped = %d, want 1", b.Dropped)
	}
}

func TestAutoMapCoversCommonExports(t *testing.T) {
	tests := map[string]ColumnRole{
		"Ref":                        RoleDesignators,
		"Designator":                 RoleDesignators,
		"  QTY  ":                    RoleQuantity,
		"Manufacturer Part Number 1": RoleMPN,
		"Supplier Part Number 1":     RoleSKU,
		"Package / Case":             RoleFootprint,
		"Value":                      RoleValue,
		"Description":                RoleDescription,
		"Nonsense":                   RoleIgnore,
	}
	for header, want := range tests {
		if got := AutoMapHeader(header); got != want {
			t.Errorf("AutoMapHeader(%q) = %q, want %q", header, got, want)
		}
	}
}

// Delimiter sniffing has to cope with the semicolon exports common in locales
// where the comma is a decimal separator.
func TestSniffDelimiter(t *testing.T) {
	tests := []struct {
		name string
		text string
		want rune
	}{
		{"comma", "Ref,Qty,Value\nR1,1,10k\n", ','},
		{"semicolon", "Ref;Qty;Value\nR1;1;10k\n", ';'},
		{"tab", "Ref\tQty\tValue\nR1\t1\t10k\n", '\t'},
	}
	for _, tc := range tests {
		sheet := parse(t, tc.text, ParseOptions{})
		if sheet.Delimiter != tc.want {
			t.Errorf("%s: sniffed %q, want %q", tc.name, sheet.Delimiter, tc.want)
		}
		if !sheet.Sniffed {
			t.Errorf("%s: Sniffed should be true when no delimiter was given", tc.name)
		}
		if len(sheet.Headers) != 3 {
			t.Errorf("%s: got %d headers, want 3", tc.name, len(sheet.Headers))
		}
	}

	// An explicit delimiter is honoured and reported as not sniffed.
	sheet := parse(t, "Ref;Qty\nR1;1\n", ParseOptions{Delimiter: ';'})
	if sheet.Sniffed {
		t.Error("an explicit delimiter must not be reported as sniffed")
	}
}

// Line numbers must survive skipped preamble lines and comments, or a finding
// points at the wrong row and costs more time than it saves.
func TestLineNumbersSurviveSkipAndComments(t *testing.T) {
	text := strings.Join([]string{
		"Exported by SomeTool", // 1, skipped
		"Generated 2026-08-26", // 2, skipped
		"# a comment",          // 3, comment
		"Ref,Qty",              // 4, header
		"R1,1",                 // 5
		"# another comment",    // 6
		"R2,",                  // 7 — dropped
	}, "\n")

	sheet := parse(t, text, ParseOptions{SkipLines: 2, Comment: '#'})
	if len(sheet.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(sheet.Rows))
	}
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})

	drops := withCode(b.Findings, CodeRowDropped)
	if len(drops) != 1 {
		t.Fatalf("got %d drops, want 1", len(drops))
	}
	if drops[0].Line != 7 {
		t.Errorf("drop reported at line %d, want 7 (the real line in the file)", drops[0].Line)
	}
}

func TestShortRowIsFlagged(t *testing.T) {
	sheet := parse(t, "Ref,Qty,Value,Footprint\nR1,1,10k\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})

	if len(withCode(b.Findings, CodeShortRow)) != 1 {
		t.Errorf("a row shorter than the header should be flagged: %+v", b.Findings)
	}
	// It is still imported, with the missing column read as absent.
	if len(b.Items) != 1 || b.Items[0].Footprint != nil {
		t.Errorf("short row should import with a nil footprint: %+v", b.Items)
	}
}

// A BOM with neither MPN nor SKU is the headline prototyping case, worth stating
// because every row then depends on the parametric tier.
func TestPureParametricBOMIsNoted(t *testing.T) {
	sheet := parse(t, "Ref,Qty,Value,Footprint\nR1,1,10k,R_0603_1608Metric\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})
	if len(withCode(b.Findings, CodeNoIdentity)) != 1 {
		t.Errorf("expected the no-identity note: %+v", b.Findings)
	}
}

// InterpretItems must carry the whole original row, including columns the
// mapping ignored, because that is what the server's LLM fallback reads.
func TestInterpretItemsCarryUnmappedColumns(t *testing.T) {
	sheet := parse(t, "Ref,Qty,Value,Footprint,Note\nR1,1,10k,R_0603_1608Metric,pull-up\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})

	items := InterpretItems(b.Items, sheet, b.ItemLines)
	if len(items) != 1 {
		t.Fatalf("got %d interpret items", len(items))
	}
	if items[0].Refdes != "R1" || items[0].Value != "10k" {
		t.Errorf("mapped fields not carried: %+v", items[0])
	}
	if items[0].Row["Note"] != "pull-up" {
		t.Errorf("unmapped column missing from the row map: %+v", items[0].Row)
	}
}

// Findings lead with what breaks the import.
func TestFindingsSortErrorsFirst(t *testing.T) {
	f := []Finding{
		{Severity: SeverityInfo, Line: 1},
		{Severity: SeverityError, Line: 9},
		{Severity: SeverityWarning, Line: 2},
		{Severity: SeverityError, Line: 3},
	}
	sortFindings(f)
	want := []struct {
		s Severity
		l int
	}{{SeverityError, 3}, {SeverityError, 9}, {SeverityWarning, 2}, {SeverityInfo, 1}}
	for i, w := range want {
		if f[i].Severity != w.s || f[i].Line != w.l {
			t.Errorf("position %d = (%s, %d), want (%s, %d)", i, f[i].Severity, f[i].Line, w.s, w.l)
		}
	}
}

func TestParseRejectsUnusableInput(t *testing.T) {
	if _, err := Parse(strings.NewReader("   \n  \n"), ParseOptions{}); err == nil {
		t.Error("an empty file should be an error")
	}
	if _, err := Parse(strings.NewReader("a,b\n1,2\n"), ParseOptions{SkipLines: 99}); err == nil {
		t.Error("skipping past the end should be an error")
	}
}

// A file with no header row still has to be addressable.
func TestNoHeaderGivesPositionalNames(t *testing.T) {
	sheet := parse(t, "R1,1,10k\nR2,2,4k7\n", ParseOptions{NoHeader: true})
	if len(sheet.Rows) != 2 {
		t.Fatalf("got %d rows, want 2 (the first row is data)", len(sheet.Rows))
	}
	if sheet.Headers[0] != "column 1" {
		t.Errorf("headers = %v, want positional names", sheet.Headers)
	}
}

// Naming a distributor without its id is the offline check's case: it says what
// will happen once the wizard supplies the id, but must not write an id into the
// payload — a fake resolution is worse than an unresolved row.
func TestNamedDistributorWithoutIDDoesNotInventOne(t *testing.T) {
	sheet := parse(t, "Ref,Qty,SKU\nR1,1,311-10.0KHRCT-ND\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{SingleDistributorName: "DigiKey"})

	it := b.Items[0]
	if it.ResolvedDistributorID != nil {
		t.Errorf("an id was invented: %q", *it.ResolvedDistributorID)
	}
	if it.DistributorName == nil || *it.DistributorName != "DigiKey" {
		t.Errorf("the intended distributor should be recorded: %+v", it)
	}
	f := withCode(b.Findings, CodeSKUNoDistributor)
	if len(f) != 1 {
		t.Fatalf("expected one finding: %+v", b.Findings)
	}
	if f[0].Severity != SeverityInfo {
		t.Errorf("naming a distributor should soften the warning to a note, got %s", f[0].Severity)
	}
}

// A column nothing recognises is ignored in silence. Altium's value column is
// called "Comment" and auto-maps to nothing, so a BOM imported straight from
// Altium loses every passive's value with no error raised anywhere.
func TestUnmappedColumnsAreListed(t *testing.T) {
	sheet := parse(t, "Designator,Quantity,Comment,Footprint\nR1,1,10k,0603\n", ParseOptions{})
	b := BuildItems(sheet, AutoMapHeaders(sheet.Headers), BuildOptions{})

	f := withCode(b.Findings, CodeUnmappedColumn)
	if len(f) != 1 {
		t.Fatalf("expected one unmapped-column finding: %+v", b.Findings)
	}
	if !strings.Contains(f[0].Message, "Comment") {
		t.Errorf("finding should name the ignored column: %s", f[0].Message)
	}
	// And the value really is lost, which is the point.
	if b.Items[0].Value != nil {
		t.Errorf("value should be absent when its column is unmapped: %+v", b.Items[0])
	}

	// Mapping it explicitly recovers the value and clears the finding.
	roles := AutoMapHeaders(sheet.Headers)
	roles[2] = RoleValue
	b = BuildItems(sheet, roles, BuildOptions{})
	if len(withCode(b.Findings, CodeUnmappedColumn)) != 0 {
		t.Errorf("no columns should remain unmapped: %+v", b.Findings)
	}
	if b.Items[0].Value == nil || *b.Items[0].Value != "10k" {
		t.Errorf("value not picked up: %+v", b.Items[0])
	}
}
