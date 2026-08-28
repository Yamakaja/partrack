package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type buildsListFlags struct {
	project string
	status  []string
	sortBy  string
	fields  []string
	limit   int
	offset  int
}

type buildsShowFlags struct {
	preview bool
}

var (
	buildListFlags buildsListFlags
	buildShowFlags buildsShowFlags
)

var buildsCmd = &cobra.Command{
	Use:   "builds",
	Short: "PCB assembly runs and what they reserve",
	Long: `A build records assembling a quantity of one BOM revision. Pending builds hold
soft allocations against stock; assembled ones have already deducted it.

  pt builds list --status pending
  pt builds pipeline
  pt builds show @1`,
}

var buildsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List builds",
	Long: `List the team's builds, numbered so the next command can address them as @1,
@2, ….

  pt builds list
  pt builds list --project "Power Supply v2" --status pending
  pt builds list --sort created_at:desc --fields id,project,revision,qty,status`,
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBuildsList(cmd.Context())
	},
}

var buildsPipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Active builds grouped by status",
	Long: `Print every build that has not reached a terminal status, grouped into the four
stages it moves through: draft, pending, ordered, ready.

  pt builds pipeline`,
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBuildsPipeline(cmd.Context())
	},
}

var buildsShowCmd = &cobra.Command{
	Use:   "show <build>",
	Short: "Show one build, its allocations and its orders",
	Long: `Print a build's status, the stock it reserves, the orders raised for it and any
substitute selections it carries.

--preview recomputes what the build will consume per BOM line, applying its
excluded designators, line overrides and substitute selections. That costs the
server a query per line, so it is opt-in.

  pt builds show @1
  pt builds show --preview 3f2a`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBuildsShow(cmd.Context(), args[0])
	},
}

// buildSortColumns mirrors validBuildSortColumns on the server. Listed here so a
// bad --sort fails locally with the vocabulary instead of being ignored server-side.
var buildSortColumns = []string{
	"project_name", "bom_revision_label", "status", "quantity", "created_at",
}

var buildColumns = []column[BuildListItem]{
	{Key: "id", Header: "ID", Value: func(b BuildListItem) string { return shortID(b.ID) }},
	{Key: "uuid", Header: "UUID", Value: func(b BuildListItem) string { return b.ID }},
	{Key: "project", Header: "PROJECT", Aliases: []string{"project_name"}, Wide: true,
		Value: func(b BuildListItem) string { return b.ProjectName }},
	{Key: "revision", Header: "REVISION", Aliases: []string{"rev", "bom_revision_label"},
		Value: func(b BuildListItem) string { return b.BomRevisionLabel }},
	{Key: "qty", Header: "QTY", Aliases: []string{"quantity"},
		Value: func(b BuildListItem) string { return strconv.FormatInt(int64(b.Quantity), 10) },
		JSON:  func(b BuildListItem) any { return b.Quantity }},
	{Key: "status", Header: "STATUS", Value: func(b BuildListItem) string { return b.Status }},
	{Key: "created", Header: "CREATED", Aliases: []string{"created_at"},
		Value: func(b BuildListItem) string { return shortDate(b.CreatedAt) }},
	{Key: "assembled", Header: "ASSEMBLED", Aliases: []string{"assembled_at"},
		Value: func(b BuildListItem) string { return shortDatePtr(b.AssembledAt) }},
	{Key: "by", Header: "BY", Aliases: []string{"created_by"},
		Value: func(b BuildListItem) string { return str(b.CreatedByDisplayName) }},
	{Key: "notes", Header: "NOTES", Wide: true,
		Value: func(b BuildListItem) string { return str(b.Notes) }},
}

var defaultBuildFields = []string{"id", "project", "revision", "qty", "status", "created"}

// fetchBuilds lists builds with the given query. Shared by the list command and
// by build reference resolution.
func (a *app) fetchBuilds(ctx context.Context, teamID string, q url.Values) (*List[BuildListItem], error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	var resp List[BuildListItem]
	if err := client.Get(ctx, "/teams/"+teamID+"/builds", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func runBuildsList(ctx context.Context) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("limit", strconv.Itoa(buildListFlags.limit))
	q.Set("offset", strconv.Itoa(buildListFlags.offset))

	// The status filter is repeatable server-side, so unlike orders this needs
	// only one request however many statuses were asked for.
	for _, s := range splitList(buildListFlags.status) {
		q.Add("status", strings.ToLower(strings.ReplaceAll(s, "-", "_")))
	}

	if buildListFlags.project != "" {
		p, err := ap.resolveProjectRef(ctx, teamID, buildListFlags.project)
		if err != nil {
			return err
		}
		q.Set("project_id", p.ID)
	}

	if buildListFlags.sortBy != "" {
		key, dir, _ := strings.Cut(buildListFlags.sortBy, ":")
		key = strings.ToLower(strings.TrimSpace(key))
		valid := false
		for _, c := range buildSortColumns {
			if c == key {
				valid = true
				break
			}
		}
		if !valid {
			return exitErrf(ExitUsage, "unknown sort field %q — available: %s",
				key, strings.Join(buildSortColumns, ", "))
		}
		q.Set("sort_by", key)
		if strings.EqualFold(dir, "desc") {
			q.Set("sort_dir", "desc")
		} else {
			q.Set("sort_dir", "asc")
		}
	}

	resp, err := ap.fetchBuilds(ctx, teamID, q)
	if err != nil {
		return err
	}

	cols, err := resolveFields(pickFieldsFrom(buildListFlags.fields, defaultBuildFields), buildColumns)
	if err != nil {
		return err
	}

	refs := make([]Ref, len(resp.Items))
	for i, b := range resp.Items {
		refs[i] = buildRef(b)
	}
	ap.saveResults(teamID, kindBuild, refs)

	if ap.format == FormatJSON {
		rows := make([]map[string]any, len(resp.Items))
		for i, b := range resp.Items {
			row := jsonRow(b, cols)
			row["n"] = i + 1
			row["uuid"] = b.ID
			rows[i] = row
		}
		return writeJSON(ap.out, map[string]any{
			"total": resp.Total, "fetched": len(resp.Items), "items": rows,
		})
	}

	if len(resp.Items) == 0 {
		fmt.Fprintln(ap.out, "No builds matched.")
		return nil
	}
	rows := make([][]string, len(resp.Items))
	for i, b := range resp.Items {
		rows[i] = numberedRow(i+1, rowFor(b, cols, 30))
	}
	writeTable(ap.out, numberedHeader(cols, nil), rows)
	fmt.Fprintln(ap.out, countFooter(len(resp.Items), resp.Total, "build"))
	return nil
}

// pipelineStages is the fixed set of buckets the endpoint returns. Printing all
// four, empty ones included, keeps the shape of the answer constant.
var pipelineStages = []string{"draft", "pending", "ordered", "ready"}

func runBuildsPipeline(ctx context.Context) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	var pipeline map[string][]PipelineBuild
	if err := client.Get(ctx, "/teams/"+teamID+"/builds/pipeline", nil, &pipeline); err != nil {
		return err
	}

	if ap.format == FormatJSON {
		return writeJSON(ap.out, pipeline)
	}

	// One flat @N set across the stages, in the order they are printed, so
	// `pt builds show @3` means the third row on screen.
	var refs []Ref
	total := 0
	for _, stage := range pipelineStages {
		for _, b := range pipeline[stage] {
			refs = append(refs, Ref{Kind: kindBuild, ID: b.ID,
				Label: b.ProjectName + " " + b.BomRevisionLabel})
			total++
		}
	}
	ap.saveResults(teamID, kindBuild, refs)

	if total == 0 {
		fmt.Fprintln(ap.out, "No active builds.")
		return nil
	}

	n := 0
	for _, stage := range pipelineStages {
		builds := pipeline[stage]
		writeSection(ap.out, strings.ToUpper(stage))
		if len(builds) == 0 {
			fmt.Fprintln(ap.out, "none")
			continue
		}
		rows := make([][]string, len(builds))
		for i, b := range builds {
			n++
			rows[i] = numberedRow(n, []string{
				shortID(b.ID),
				truncate(b.ProjectName, 30),
				b.BomRevisionLabel,
				strconv.FormatInt(int64(b.Quantity), 10),
				shortDate(b.CreatedAt),
			})
		}
		writeTable(ap.out, []string{"#", "ID", "PROJECT", "REVISION", "QTY", "CREATED"}, rows)
	}
	fmt.Fprintln(ap.out)
	fmt.Fprintln(ap.out, countFooter(total, int64(total), "active build"))
	return nil
}

func runBuildsShow(ctx context.Context, ref string) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	buildID, err := ap.resolveBuildRef(ctx, teamID, ref)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	var build BuildDetail
	if err := client.Get(ctx, "/teams/"+teamID+"/builds/"+buildID, nil, &build); err != nil {
		return err
	}
	ap.remember(teamID, Ref{Kind: kindBuild, ID: build.ID,
		Label: build.ProjectName + " " + build.BomRevisionLabel})

	var preview struct {
		Items []BuildPreviewItem `json:"items"`
	}
	if buildShowFlags.preview {
		if err := client.Get(ctx, "/teams/"+teamID+"/builds/"+buildID+"/preview", nil, &preview); err != nil {
			return err
		}
	}

	if ap.format == FormatJSON {
		out := map[string]any{"build": build}
		if buildShowFlags.preview {
			out["preview"] = preview.Items
		}
		return writeJSON(ap.out, out)
	}

	fmt.Fprintf(ap.out, "%s  %s  ×%d\n", build.ProjectName, build.BomRevisionLabel, build.Quantity)
	pairs := [][2]string{
		{"Build", shortID(build.ID)},
		{"Status", build.Status},
		{"Created", strings.TrimSpace(build.CreatedAt + "  by " + strOr(build.CreatedByDisplayName, ""))},
	}
	if build.AssembledAt != nil {
		pairs = append(pairs, [2]string{"Assembled",
			strings.TrimSpace(*build.AssembledAt + "  by " + strOr(build.AssembledByDisplayName, ""))})
	}
	if build.AssembledQuantity != nil {
		pairs = append(pairs, [2]string{"Assembled qty", num(build.AssembledQuantity)})
	}
	if len(build.ExcludedDesignators) > 0 {
		pairs = append(pairs, [2]string{"Excluded",
			truncate(strings.Join(build.ExcludedDesignators, " "), 60)})
	}
	if build.ExcludedFromCart {
		pairs = append(pairs, [2]string{"Cart", "excluded from shortfall aggregation"})
	}
	if build.Notes != nil && *build.Notes != "" {
		pairs = append(pairs, [2]string{"Notes", *build.Notes})
	}
	writeKV(ap.out, pairs)

	if len(build.Allocations) > 0 {
		writeSection(ap.out, "RESERVED STOCK")
		rows := make([][]string, len(build.Allocations))
		for i, a := range build.Allocations {
			rows[i] = []string{
				strOr(a.PartMpn, shortID(a.PartID)),
				str(a.PartManufacturer),
				strconv.FormatInt(int64(a.QuantityNeeded), 10),
				strconv.FormatInt(int64(a.QuantityReserved), 10),
				strconv.FormatInt(int64(a.QuantityReceived), 10),
			}
		}
		writeTable(ap.out, []string{"MPN", "MFR", "NEEDED", "RESERVED", "RECEIVED"}, rows)
	}

	if len(build.SubstituteSelections) > 0 {
		writeSection(ap.out, "SUBSTITUTES CHOSEN")
		rows := make([][]string, len(build.SubstituteSelections))
		for i, s := range build.SubstituteSelections {
			rows[i] = []string{shortID(s.BomLineItemID), strOr(s.SelectedPartMpn, shortID(s.SelectedPartID))}
		}
		writeTable(ap.out, []string{"LINE", "PART"}, rows)
		if build.SelectionsStale {
			fmt.Fprintln(ap.out,
				"These selections were made against stock that has since changed — re-check before kitting.")
		}
	}

	if len(build.LineItemOverrides) > 0 {
		writeSection(ap.out, "LINE OVERRIDES")
		rows := make([][]string, len(build.LineItemOverrides))
		for i, o := range build.LineItemOverrides {
			rows[i] = []string{shortID(o.BomLineItemID), o.OverrideType}
		}
		writeTable(ap.out, []string{"LINE", "OVERRIDE"}, rows)
	}

	if len(build.OrderIDs) > 0 {
		writeSection(ap.out, "ORDERS")
		for _, id := range build.OrderIDs {
			fmt.Fprintf(ap.out, "%s\n", shortID(id))
		}
	}

	// Consumed items are null until the build is assembled, which is a different
	// statement from an assembled build that consumed nothing.
	if build.Items != nil {
		writeSection(ap.out, "CONSUMED")
		if len(build.Items) == 0 {
			fmt.Fprintln(ap.out, "Nothing was deducted.")
		} else {
			rows := make([][]string, len(build.Items))
			for i, c := range build.Items {
				rows[i] = []string{
					strOr(c.PartMpn, shortID(c.PartID)),
					str(c.PartManufacturer),
					strconv.FormatInt(int64(c.QuantityDeducted), 10),
				}
			}
			writeTable(ap.out, []string{"MPN", "MFR", "DEDUCTED"}, rows)
		}
	}

	if build.KittingSession != nil {
		writeSection(ap.out, "KITTING")
		writeKV(ap.out, [][2]string{
			{"Session", shortID(build.KittingSession.ID)},
			{"Status", build.KittingSession.Status},
		})
	}

	if buildShowFlags.preview {
		printBuildPreview(preview.Items)
	}
	return nil
}

func printBuildPreview(items []BuildPreviewItem) {
	writeSection(ap.out, "WILL CONSUME")
	if len(items) == 0 {
		fmt.Fprintln(ap.out, "Nothing — every line is excluded or unresolved.")
		return
	}
	rows := make([][]string, len(items))
	short := 0
	for i, it := range items {
		status := "ok"
		if !it.Sufficient {
			status = "short"
			short++
		}
		perBoard := strconv.FormatInt(int64(it.EffectiveQuantityPerBoard), 10)
		if it.EffectiveQuantityPerBoard != it.QuantityPerBoard {
			// The BOM says one thing, the build's exclusions another; showing both
			// is the only way the difference is visible.
			perBoard = fmt.Sprintf("%d/%d", it.EffectiveQuantityPerBoard, it.QuantityPerBoard)
		}
		rows[i] = []string{
			truncate(it.ReferenceDesignators, 24),
			strOr(it.ResolvedPartMpn, dash),
			perBoard,
			strconv.FormatInt(int64(it.EffectiveTotalQuantity), 10),
			strconv.FormatInt(int64(it.QuantityOnHand), 10),
			status,
			strOr(it.Override, dash),
		}
	}
	writeTable(ap.out, []string{"REFDES", "MPN", "PER BOARD", "TOTAL", "ON HAND", "STATUS", "OVERRIDE"}, rows)
	if short > 0 {
		fmt.Fprintf(ap.out, "%d of %d lines are short.\n", short, len(items))
	} else {
		fmt.Fprintf(ap.out, "All %d lines are covered.\n", len(items))
	}
}

func init() {
	lf := buildsListCmd.Flags()
	lf.StringVar(&buildListFlags.project, "project", "", "only builds of this project (name or id)")
	lf.StringSliceVar(&buildListFlags.status, "status", nil,
		"build status, repeatable: draft, pending, ordered, ready, assembled, cancelled")
	lf.StringVar(&buildListFlags.sortBy, "sort", "",
		"sort by "+strings.Join(buildSortColumns, ", ")+", e.g. created_at:desc")
	lf.StringSliceVar(&buildListFlags.fields, "fields", nil,
		"columns to show, in order (default "+strings.Join(defaultBuildFields, ",")+")")
	lf.IntVar(&buildListFlags.limit, "limit", 50, "maximum rows to fetch (server caps a page at 200)")
	lf.IntVar(&buildListFlags.offset, "offset", 0, "row offset to start from")

	buildsShowCmd.Flags().BoolVar(&buildShowFlags.preview, "preview", false,
		"also compute what the build will consume, line by line")

	buildsCmd.AddCommand(buildsListCmd, buildsPipelineCmd, buildsShowCmd)
}
