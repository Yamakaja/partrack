package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var categoriesFlags struct {
	nonEmpty bool
}

var categoriesCmd = &cobra.Command{
	Use:     "categories [query]",
	Aliases: []string{"cats"},
	Short:   "Browse the category tree with your part counts",
	Long: `List categories with the number of parts your team has in each. Counts are
direct, not recursive — a parent shows only parts filed directly against it.

With a query, list matching categories flat. Without one, print the tree.

  pt categories
  pt categories resistor
  pt categories --non-empty`,
	Args: usageArgs(cobra.MaximumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		return runCategories(cmd.Context(), query)
	},
}

func runCategories(ctx context.Context, query string) error {
	rd, err := ap.loadRefData(ctx)
	if err != nil {
		return err
	}
	counts, err := ap.categoryCounts(ctx)
	if err != nil {
		return err
	}

	if query != "" || categoriesFlags.nonEmpty {
		return listCategoriesFlat(rd, counts, query)
	}
	if ap.format == FormatJSON {
		return writeJSON(ap.out, categoryJSON(rd, counts, rd.Categories))
	}
	printCategoryTree(rd, counts, nil, 0)
	return nil
}

// categoryCounts fetches per-category part counts for the active team.
func (a *app) categoryCounts(ctx context.Context) (map[int32]int64, error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	teamID, err := a.teamID(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Categories []struct {
			ID    int32 `json:"id"`
			Count int64 `json:"count"`
		} `json:"categories"`
	}
	if err := client.Get(ctx, "/teams/"+teamID+"/parts/categories", nil, &resp); err != nil {
		return nil, err
	}
	counts := make(map[int32]int64, len(resp.Categories))
	for _, c := range resp.Categories {
		counts[c.ID] = c.Count
	}
	return counts, nil
}

func listCategoriesFlat(rd *refData, counts map[int32]int64, query string) error {
	lower := strings.ToLower(query)
	var matched []Category
	for _, c := range rd.Categories {
		if categoriesFlags.nonEmpty && counts[c.ID] == 0 {
			continue
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(c.DisplayName), lower) &&
			!strings.Contains(strings.ToLower(c.Slug), lower) {
			continue
		}
		matched = append(matched, c)
	}

	if ap.format == FormatJSON {
		return writeJSON(ap.out, categoryJSON(rd, counts, matched))
	}
	if len(matched) == 0 {
		fmt.Fprintln(ap.out, "No categories matched.")
		return nil
	}
	rows := make([][]string, len(matched))
	for i, c := range matched {
		rows[i] = []string{
			strconv.FormatInt(int64(c.ID), 10),
			c.Slug,
			rd.path(&matched[i]),
			formatCount(counts[c.ID]),
		}
	}
	writeTable(ap.out, []string{"ID", "SLUG", "PATH", "PARTS"}, rows)
	return nil
}

func printCategoryTree(rd *refData, counts map[int32]int64, parentID *int32, depth int) {
	for _, c := range rd.children(parentID) {
		count := ""
		if n := counts[c.ID]; n > 0 {
			count = fmt.Sprintf("  %d", n)
		}
		fmt.Fprintf(ap.out, "%s%s%s\n", strings.Repeat("  ", depth), c.DisplayName, count)
		printCategoryTree(rd, counts, &c.ID, depth+1)
	}
}

func categoryJSON(rd *refData, counts map[int32]int64, cats []Category) map[string]any {
	items := make([]map[string]any, len(cats))
	for i := range cats {
		items[i] = map[string]any{
			"id":             cats[i].ID,
			"slug":           cats[i].Slug,
			"display_name":   cats[i].DisplayName,
			"parent_id":      cats[i].ParentID,
			"path":           rd.path(&cats[i]),
			"value_param":    cats[i].ValueParam,
			"primary_params": cats[i].PrimaryParams,
			"part_count":     counts[cats[i].ID],
		}
	}
	return map[string]any{"categories": items}
}

func formatCount(n int64) string {
	if n == 0 {
		return dash
	}
	return strconv.FormatInt(n, 10)
}

var teamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "List the teams you belong to",
	Args:  usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		teams, err := ap.listTeams(cmd.Context())
		if err != nil {
			return err
		}
		if ap.format == FormatJSON {
			return writeJSON(ap.out, map[string]any{"teams": teams, "active": ap.cfg.Team})
		}
		if len(teams) == 0 {
			fmt.Fprintln(ap.out, "You are not a member of any team.")
			return nil
		}
		rows := make([][]string, len(teams))
		for i, t := range teams {
			marker := ""
			if strings.EqualFold(t.Name, ap.cfg.Team) || t.ID == ap.cfg.Team {
				marker = " *"
			}
			rows[i] = []string{t.Name + marker, t.Role, strOr(t.Description, dash)}
		}
		writeTable(ap.out, []string{"NAME", "ROLE", "DESCRIPTION"}, rows)
		if ap.cfg.Team != "" {
			fmt.Fprintln(ap.out, "* active team")
		}
		return nil
	},
}

func init() {
	categoriesCmd.Flags().BoolVar(&categoriesFlags.nonEmpty, "non-empty", false,
		"list only categories your team has parts in")
}
