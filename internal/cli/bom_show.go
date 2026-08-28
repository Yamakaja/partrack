package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type bomShowFlags struct {
	revision   string
	unresolved bool
	fields     []string
	limit      int
}

type bomStockFlags struct {
	revision string
	quantity int
	short    bool
}

var (
	bomShowF  bomShowFlags
	bomStockF bomStockFlags
)

var bomShowCmd = &cobra.Command{
	Use:   "show <project>",
	Short: "Show an imported BOM revision and how its lines resolved",
	Long: `Print the line items of a project's BOM revision — reference designators, the
part each line resolved to, and the tier that decided it.

Without --revision this reads the project's active revision, which is the one
stock checks and builds run against.

--unresolved narrows to the lines that still need a person: nothing matched,
several things matched, or a distributor lookup is outstanding.

  pt bom show "Power Supply v2"
  pt bom show @1 --unresolved
  pt bom show @1 --revision rev-3 --fields refdes,qty,mpn,state`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBOMShow(cmd.Context(), args[0])
	},
}

var bomStockCmd = &cobra.Command{
	Use:   "stock <project>",
	Short: "Check a BOM revision against stock for a build quantity",
	Long: `Ask what building N boards of a project would take, line by line: what is
needed, what is on hand, what is already reserved by other builds, what is on
order, and what is short.

The build quantity is ephemeral — it is a parameter of the question, not a
property of the project — so --quantity is required.

  pt bom stock "Power Supply v2" --quantity 10
  pt bom stock @1 --quantity 25 --short`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBOMStock(cmd.Context(), args[0])
	},
}

var bomColumns = []column[BOMLineItem]{
	{Key: "id", Header: "ID", Value: func(l BOMLineItem) string { return shortID(l.ID) }},
	{Key: "refdes", Header: "REFDES", Aliases: []string{"designators"}, Wide: true,
		Value: func(l BOMLineItem) string { return l.ReferenceDesignators }},
	{Key: "qty", Header: "QTY", Aliases: []string{"quantity"},
		Value: func(l BOMLineItem) string { return strconv.FormatInt(int64(l.Quantity), 10) },
		JSON:  func(l BOMLineItem) any { return l.Quantity }},
	{Key: "value", Header: "VALUE", Value: func(l BOMLineItem) string { return str(l.Value) }},
	{Key: "footprint", Header: "FOOTPRINT", Value: func(l BOMLineItem) string { return str(l.Footprint) }},
	{Key: "bom_mpn", Header: "BOM MPN", Aliases: []string{"raw_mpn"},
		Value: func(l BOMLineItem) string { return str(l.MPN) }},
	{Key: "mpn", Header: "MPN", Aliases: []string{"resolved_mpn"},
		Value: func(l BOMLineItem) string { return str(l.ResolvedPartMpn) }},
	{Key: "mfr", Header: "MFR", Aliases: []string{"manufacturer"},
		Value: func(l BOMLineItem) string { return str(l.ResolvedPartMfr) }},
	{Key: "pkg", Header: "PKG", Aliases: []string{"package"},
		Value: func(l BOMLineItem) string { return str(l.ResolvedPartPackage) }},
	{Key: "stock", Header: "STOCK", Aliases: []string{"on_hand", "quantity_on_hand"},
		// Null means the team has no inventory entry for the part at all, which is
		// a different statement from holding zero.
		Value: func(l BOMLineItem) string { return num(l.QuantityOnHand) },
		JSON:  func(l BOMLineItem) any { return intOrNil(l.QuantityOnHand) }},
	{Key: "state", Header: "STATE", Aliases: []string{"resolution_state"},
		Value: func(l BOMLineItem) string {
			if l.Ignored {
				return "ignored"
			}
			return stateLabel(strOr(l.ResolutionState, ""))
		}},
	{Key: "subs", Header: "SUBS", Aliases: []string{"substitutes"},
		Value: func(l BOMLineItem) string {
			if len(l.Substitutes) == 0 {
				return dash
			}
			return strconv.Itoa(len(l.Substitutes))
		},
		JSON: func(l BOMLineItem) any { return len(l.Substitutes) }},
	{Key: "desc", Header: "DESC", Aliases: []string{"description"}, Wide: true,
		Value: func(l BOMLineItem) string {
			return strOr(l.ResolvedPartDesc, strOr(l.RawDescription, dash))
		}},
}

var defaultBOMFields = []string{"refdes", "qty", "value", "mpn", "mfr", "stock", "state"}

// unresolvedStates are the resolution states that still need a person. A fuzzy
// MPN match resolves but is a weaker claim than an exact one, so the review
// queue surfaces it and so does --unresolved.
var unresolvedStates = map[string]bool{
	"":               true,
	"ambiguous":      true,
	"importing":      true,
	"auto_mpn_fuzzy": true,
}

// revisionFor picks the revision to read: the one named by --revision, or the
// project's active one. A project with no active revision has never had a BOM
// imported, which is worth saying rather than 404ing.
func (a *app) revisionFor(ctx context.Context, teamID string, p *ProjectListItem, ref string) (string, string, error) {
	if strings.TrimSpace(ref) == "" {
		if p.ActiveBomRevisionID == nil {
			return "", "", exitErrf(ExitNotFound,
				"%s has no active BOM revision — import one, or name a revision with --revision", p.Name)
		}
		return *p.ActiveBomRevisionID, strOr(p.ActiveBomRevisionLabel, ""), nil
	}

	revisions, err := a.fetchRevisions(ctx, teamID, p.ID)
	if err != nil {
		return "", "", err
	}
	if uuidRe.MatchString(ref) {
		want := strings.ToLower(ref)
		for _, r := range revisions {
			if strings.EqualFold(r.ID, want) {
				return r.ID, r.Label, nil
			}
		}
		return "", "", exitErrf(ExitNotFound, "no revision with id %s in %s", shortID(ref), p.Name)
	}
	r, err := pickOne(ref, "revision", revisions,
		func(r RevisionListItem) matchRank { return rankText(r.Label, ref) },
		func(r RevisionListItem) string {
			return fmt.Sprintf("%-8s %-16s %s", shortID(r.ID), r.Label, shortDate(r.ImportedAt))
		})
	if err != nil {
		return "", "", err
	}
	return r.ID, r.Label, nil
}

func runBOMShow(ctx context.Context, ref string) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	p, err := ap.resolveProjectRef(ctx, teamID, ref)
	if err != nil {
		return err
	}
	revID, revLabel, err := ap.revisionFor(ctx, teamID, p, bomShowF.revision)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	var rev RevisionDetail
	path := "/teams/" + teamID + "/projects/" + p.ID + "/revisions/" + revID
	if err := client.Get(ctx, path, nil, &rev); err != nil {
		return err
	}

	items := rev.Items
	if bomShowF.unresolved {
		filtered := make([]BOMLineItem, 0, len(items))
		for _, l := range items {
			if l.Ignored {
				continue
			}
			if unresolvedStates[strOr(l.ResolutionState, "")] {
				filtered = append(filtered, l)
			}
		}
		items = filtered
	}
	shown := len(items)
	if bomShowF.limit > 0 && shown > bomShowF.limit {
		items = items[:bomShowF.limit]
	}

	cols, err := resolveFields(pickFieldsFrom(bomShowF.fields, defaultBOMFields), bomColumns)
	if err != nil {
		return err
	}

	// BOM rows address the parts they resolved to, so `pt show @3` works on the
	// line you just read. An unresolved line still takes its slot, as a ref with
	// no id: dropping it would silently shift every @N after it onto the wrong
	// part.
	refs := make([]Ref, len(items))
	for i, l := range items {
		if l.ResolvedPartID != nil {
			refs[i] = identOf(*l.ResolvedPartID, l.ResolvedPartMpn, l.ResolvedPartMfr)
		} else {
			refs[i] = Ref{Kind: kindPart, Label: l.ReferenceDesignators}
		}
	}
	ap.saveResults(teamID, kindPart, refs)

	if ap.format == FormatJSON {
		rows := make([]map[string]any, len(items))
		for i, l := range items {
			row := jsonRow(l, cols)
			row["n"] = i + 1
			row["line_item_id"] = l.ID
			row["resolved_part_id"] = l.ResolvedPartID
			rows[i] = row
		}
		return writeJSON(ap.out, map[string]any{
			"project":  p.Name,
			"revision": map[string]any{"id": rev.ID, "label": rev.Label, "is_active": rev.IsActive},
			"total":    len(rev.Items),
			"fetched":  len(rows),
			"items":    rows,
		})
	}

	fmt.Fprintf(ap.out, "%s  revision %s", p.Name, revLabel)
	if rev.IsActive {
		fmt.Fprint(ap.out, "  (active)")
	}
	fmt.Fprintf(ap.out, "\nImported %s by %s\n", shortDate(rev.ImportedAt), rev.ImportedByDisplayName)

	if len(items) == 0 {
		if bomShowF.unresolved {
			fmt.Fprintln(ap.out, "\nEvery line resolved.")
		} else {
			fmt.Fprintln(ap.out, "\nThis revision has no line items.")
		}
		return nil
	}

	fmt.Fprintln(ap.out)
	rows := make([][]string, len(items))
	for i, l := range items {
		rows[i] = numberedRow(i+1, rowFor(l, cols, 24))
	}
	writeTable(ap.out, numberedHeader(cols, nil), rows)
	fmt.Fprintln(ap.out, bomShowFooter(len(items), shown, len(rev.Items)))
	return nil
}

// bomShowFooter states the three counts that can differ: printed, matching the
// filter, and present in the revision.
func bomShowFooter(printed, matched, total int) string {
	switch {
	case printed < matched:
		return fmt.Sprintf("%d of %d matching lines (%d in the revision) — raise --limit to see more",
			printed, matched, total)
	case matched < total:
		return fmt.Sprintf("%d of %d lines", matched, total)
	default:
		return countFooter(printed, int64(total), "line")
	}
}

func runBOMStock(ctx context.Context, ref string) error {
	if bomStockF.quantity <= 0 {
		return exitErrf(ExitUsage,
			"--quantity is required and must be positive: how many boards are you building?")
	}
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	p, err := ap.resolveProjectRef(ctx, teamID, ref)
	if err != nil {
		return err
	}
	revID, revLabel, err := ap.revisionFor(ctx, teamID, p, bomStockF.revision)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("build_quantity", strconv.Itoa(bomStockF.quantity))
	var check StockCheck
	path := "/teams/" + teamID + "/projects/" + p.ID + "/revisions/" + revID + "/stock-check"
	if err := client.Get(ctx, path, q, &check); err != nil {
		return err
	}

	items := check.Items
	if bomStockF.short {
		filtered := make([]StockCheckItem, 0, len(items))
		for _, it := range items {
			if it.Shortfall > 0 {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	// As in `bom show`, a line with no resolved part keeps its slot so @N lines up
	// with the printed row numbers.
	refs := make([]Ref, len(items))
	for i, it := range items {
		if it.ResolvedPartID != nil {
			refs[i] = identOf(*it.ResolvedPartID, it.ResolvedPartMpn, nil)
		} else {
			refs[i] = Ref{Kind: kindPart, Label: it.ReferenceDesignators}
		}
	}
	ap.saveResults(teamID, kindPart, refs)

	if ap.format == FormatJSON {
		return writeJSON(ap.out, map[string]any{
			"project":         p.Name,
			"revision":        map[string]any{"id": check.RevisionID, "label": revLabel},
			"build_quantity":  check.BuildQuantity,
			"fully_buildable": check.FullyBuildable,
			"total":           len(check.Items),
			"items":           items,
		})
	}

	fmt.Fprintf(ap.out, "%s  revision %s  ×%d boards\n", p.Name, revLabel, check.BuildQuantity)

	shortLines := 0
	for _, it := range check.Items {
		if it.Shortfall > 0 {
			shortLines++
		}
	}
	writeKV(ap.out, [][2]string{
		{"Lines", strconv.Itoa(len(check.Items))},
		{"Short", strconv.Itoa(shortLines)},
		{"Fully buildable", strconv.FormatInt(int64(check.FullyBuildable), 10) + " boards"},
	})

	if len(items) == 0 {
		if bomStockF.short {
			fmt.Fprintln(ap.out, "\nNothing is short — you can build this now.")
		} else {
			fmt.Fprintln(ap.out, "\nThis revision has no line items.")
		}
		return nil
	}

	writeSection(ap.out, "PER LINE")
	rows := make([][]string, len(items))
	for i, it := range items {
		part := strOr(it.ResolvedPartMpn, dash)
		if it.SelectedSubstituteMPN != nil && *it.SelectedSubstituteMPN != "" {
			// A substitute is doing the work; naming the primary alone would be a
			// lie about what gets fitted.
			part = *it.SelectedSubstituteMPN + " (sub)"
		}
		rows[i] = []string{
			truncate(it.ReferenceDesignators, 22),
			part,
			strconv.FormatInt(int64(it.RequiredQuantity), 10),
			strconv.FormatInt(int64(it.QuantityOnHand), 10),
			strconv.FormatInt(int64(it.QuantityReserved), 10),
			strconv.FormatInt(int64(it.QuantityFree), 10),
			strconv.FormatInt(int64(it.QuantityOnOrder), 10),
			shortfallCell(it.Shortfall),
			it.StockStatus,
		}
	}
	writeTable(ap.out, []string{
		"REFDES", "PART", "NEED", "ON HAND", "RESERVED", "FREE", "ON ORDER", "SHORT", "STATUS",
	}, rows)

	if bomStockF.short {
		fmt.Fprintf(ap.out, "%d of %d lines are short.\n", len(items), len(check.Items))
	} else {
		fmt.Fprintln(ap.out, countFooter(len(items), int64(len(check.Items)), "line"))
	}
	return nil
}

// shortfallCell renders a shortfall, keeping zero visually quiet so the lines
// that need action stand out in the column.
func shortfallCell(n int32) string {
	if n <= 0 {
		return dash
	}
	return strconv.FormatInt(int64(n), 10)
}

func init() {
	sf := bomShowCmd.Flags()
	sf.StringVar(&bomShowF.revision, "revision", "", "revision by label or id (default: the active one)")
	sf.BoolVar(&bomShowF.unresolved, "unresolved", false, "only lines that still need a part chosen")
	sf.StringSliceVar(&bomShowF.fields, "fields", nil,
		"columns to show, in order (default "+strings.Join(defaultBOMFields, ",")+")")
	sf.IntVar(&bomShowF.limit, "limit", 0, "maximum lines to print (0 for all)")

	kf := bomStockCmd.Flags()
	kf.StringVar(&bomStockF.revision, "revision", "", "revision by label or id (default: the active one)")
	kf.IntVarP(&bomStockF.quantity, "quantity", "q", 0, "how many boards to check for (required)")
	kf.BoolVar(&bomStockF.short, "short", false, "only lines with a shortfall")

	bomCmd.AddCommand(bomShowCmd, bomStockCmd)
}
