package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type projectsListFlags struct {
	status string
	fields []string
	limit  int
	offset int
}

var projectListFlags projectsListFlags

var projectsCmd = &cobra.Command{
	Use:     "projects",
	Aliases: []string{"project"},
	Short:   "PCB projects and their BOM revisions",
	Long: `A project owns a series of BOM revisions; one of them is active at a time and
is what stock checks and builds run against.

  pt projects list
  pt projects show "Power Supply v2"`,
}

var projectsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects",
	Long: `List the team's projects, numbered so the next command can address them as @1,
@2, ….

  pt projects list
  pt projects list --status active --fields name,revision,status,manager`,
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectsList(cmd.Context())
	},
}

var projectsShowCmd = &cobra.Command{
	Use:   "show <project>",
	Short: "Show one project and its revision history",
	Long: `Print a project's identity, how its active BOM revision resolved, and every
revision imported into it.

A project is addressed by @N from the last listing, by name, or by an id prefix.

  pt projects show "Power Supply v2"
  pt projects show @1`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectsShow(cmd.Context(), args[0])
	},
}

var projectColumns = []column[ProjectListItem]{
	{Key: "id", Header: "ID", Value: func(p ProjectListItem) string { return shortID(p.ID) }},
	{Key: "uuid", Header: "UUID", Value: func(p ProjectListItem) string { return p.ID }},
	{Key: "name", Header: "NAME", Wide: true, Value: func(p ProjectListItem) string { return p.Name }},
	{Key: "status", Header: "STATUS", Value: func(p ProjectListItem) string { return p.Status }},
	{Key: "revision", Header: "REVISION", Aliases: []string{"rev", "active_revision"},
		Value: func(p ProjectListItem) string { return str(p.ActiveBomRevisionLabel) }},
	{Key: "desc", Header: "DESC", Aliases: []string{"description"}, Wide: true,
		Value: func(p ProjectListItem) string { return str(p.Description) }},
	{Key: "manager", Header: "MANAGER", Aliases: []string{"project_manager"},
		Value: func(p ProjectListItem) string { return str(p.ProjectManagerDisplayName) }},
	{Key: "created", Header: "CREATED", Aliases: []string{"created_at"},
		Value: func(p ProjectListItem) string { return shortDate(p.CreatedAt) }},
	{Key: "by", Header: "BY", Aliases: []string{"created_by"},
		Value: func(p ProjectListItem) string { return p.CreatedByDisplayName }},
}

var defaultProjectFields = []string{"id", "name", "status", "revision", "manager", "created"}

func runProjectsList(ctx context.Context) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("limit", strconv.Itoa(projectListFlags.limit))
	q.Set("offset", strconv.Itoa(projectListFlags.offset))
	if s := strings.TrimSpace(projectListFlags.status); s != "" {
		q.Set("status", strings.ToLower(s))
	}

	var resp List[ProjectListItem]
	if err := client.Get(ctx, "/teams/"+teamID+"/projects", q, &resp); err != nil {
		return err
	}

	cols, err := resolveFields(pickFieldsFrom(projectListFlags.fields, defaultProjectFields), projectColumns)
	if err != nil {
		return err
	}

	refs := make([]Ref, len(resp.Items))
	for i, p := range resp.Items {
		refs[i] = projectRef(p)
	}
	ap.saveResults(teamID, kindProject, refs)

	if ap.format == FormatJSON {
		rows := make([]map[string]any, len(resp.Items))
		for i, p := range resp.Items {
			row := jsonRow(p, cols)
			row["n"] = i + 1
			row["uuid"] = p.ID
			rows[i] = row
		}
		return writeJSON(ap.out, map[string]any{
			"total": resp.Total, "fetched": len(resp.Items), "items": rows,
		})
	}

	if len(resp.Items) == 0 {
		fmt.Fprintln(ap.out, "No projects matched.")
		return nil
	}
	rows := make([][]string, len(resp.Items))
	for i, p := range resp.Items {
		rows[i] = numberedRow(i+1, rowFor(p, cols, 32))
	}
	writeTable(ap.out, numberedHeader(cols, nil), rows)
	fmt.Fprintln(ap.out, countFooter(len(resp.Items), resp.Total, "project"))
	return nil
}

func runProjectsShow(ctx context.Context, ref string) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	p, err := ap.resolveProjectRef(ctx, teamID, ref)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	var detail ProjectDetail
	if err := client.Get(ctx, "/teams/"+teamID+"/projects/"+p.ID, nil, &detail); err != nil {
		return err
	}

	revisions, err := ap.fetchRevisions(ctx, teamID, p.ID)
	if err != nil {
		return err
	}
	note := fetchProjectNote(ctx, client, teamID, p.ID)

	if ap.format == FormatJSON {
		out := map[string]any{"project": detail, "revisions": revisions}
		if note != nil {
			out["note"] = note
		}
		return writeJSON(ap.out, out)
	}

	fmt.Fprintln(ap.out, detail.Name)
	pairs := [][2]string{
		{"Project", shortID(detail.ID)},
		{"Status", detail.Status},
		{"Active revision", str(detail.ActiveBomRevisionLabel)},
		{"Manager", str(detail.ProjectManagerDisplayName)},
		{"Created", strings.TrimSpace(detail.CreatedAt + "  by " + detail.CreatedByDisplayName)},
	}
	if detail.Description != nil && *detail.Description != "" {
		pairs = append(pairs, [2]string{"Description", *detail.Description})
	}
	if detail.SourceURL != nil && *detail.SourceURL != "" {
		pairs = append(pairs, [2]string{"Source", *detail.SourceURL})
	}
	writeKV(ap.out, pairs)

	if s := detail.ActiveRevisionSummary; s != nil {
		writeSection(ap.out, "ACTIVE REVISION")
		lines := [][2]string{
			{"Lines", strconv.FormatInt(s.ItemCount, 10)},
			{"Resolved", strconv.FormatInt(s.ResolvedCount, 10)},
			{"Unresolved", strconv.FormatInt(s.UnresolvedCount, 10)},
			{"Ambiguous", strconv.FormatInt(s.AmbiguousCount, 10)},
		}
		writeKV(ap.out, lines)
		if s.UnresolvedCount > 0 || s.AmbiguousCount > 0 {
			fmt.Fprintf(ap.out,
				"%d line%s still need a part chosen — `pt bom show %s --unresolved` lists them.\n",
				s.UnresolvedCount+s.AmbiguousCount, plural(int(s.UnresolvedCount+s.AmbiguousCount)),
				shortID(detail.ID))
		}
	}

	if len(revisions) > 0 {
		writeSection(ap.out, "REVISIONS")
		rows := make([][]string, len(revisions))
		for i, r := range revisions {
			active := ""
			if r.IsActive {
				active = "active"
			}
			rows[i] = []string{
				r.Label,
				shortDate(r.ImportedAt),
				r.ImportedByDisplayName,
				strconv.FormatInt(r.ItemCount, 10),
				strconv.FormatInt(r.ResolvedCount, 10),
				strconv.FormatInt(r.UnresolvedCount, 10),
				active,
			}
		}
		writeTable(ap.out, []string{"LABEL", "IMPORTED", "BY", "LINES", "RESOLVED", "UNRESOLVED", ""}, rows)
	}

	if note != nil && note.Body != "" {
		writeSection(ap.out, "NOTE")
		fmt.Fprintln(ap.out, note.Body)
	}
	return nil
}

// fetchRevisions lists a project's BOM revisions, newest first as the server
// returns them.
func (a *app) fetchRevisions(ctx context.Context, teamID, projectID string) ([]RevisionListItem, error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items []RevisionListItem `json:"items"`
	}
	if err := client.Get(ctx, "/teams/"+teamID+"/projects/"+projectID+"/revisions", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// fetchProjectNote reads the team's note on a project. A failure is not fatal:
// the note is a garnish on the dossier, not the answer.
func fetchProjectNote(ctx context.Context, client *Client, teamID, projectID string) *Note {
	var note Note
	if err := client.Get(ctx, "/teams/"+teamID+"/projects/"+projectID+"/note", nil, &note); err != nil {
		return nil
	}
	return &note
}

func init() {
	lf := projectsListCmd.Flags()
	lf.StringVar(&projectListFlags.status, "status", "", "project status, e.g. active or archived")
	lf.StringSliceVar(&projectListFlags.fields, "fields", nil,
		"columns to show, in order (default "+strings.Join(defaultProjectFields, ",")+")")
	lf.IntVar(&projectListFlags.limit, "limit", 50, "maximum rows to fetch (server caps a page at 200)")
	lf.IntVar(&projectListFlags.offset, "offset", 0, "row offset to start from")

	projectsCmd.AddCommand(projectsListCmd, projectsShowCmd)
}
