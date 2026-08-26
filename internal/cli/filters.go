package cli

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Yamakaja/partrack/internal/apitypes"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/spf13/cobra"
)

var filtersFlags struct {
	maxOptions int
}

var filtersCmd = &cobra.Command{
	Use:   "filters <category>",
	Short: "List the parameters you can filter a category by",
	Long: `Print the parameters carried by parts in a category and its descendants, their
units and value types, and what your catalog actually holds for each — enum
vocabularies verbatim, numeric parameters as the observed range.

Two lists exist server-side: an admin-curated profile per category, and what the
catalog really contains. This shows the latter, because that is what --param can
match; curated entries are marked. Enum matching is exact string equality, so
copy those values verbatim.

  pt filters Resistors
  pt filters chip-resistor-surface-mount --max-values 0
  pt filters Capacitors --format json`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFilters(cmd.Context(), args[0])
	},
}

func runFilters(ctx context.Context, ref string) error {
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	rd, err := ap.loadRefData(ctx)
	if err != nil {
		return err
	}
	category, err := rd.resolveCategory(ref)
	if err != nil {
		return err
	}

	// Two curated things combine here.
	//
	// The class profile (value_param + primary_params) is admin-set per category
	// and lives on the parametric class, not the leaf — so it is resolved by
	// walking the cached tree rather than trusting the leaf's own row.
	valueParam, primaryParams, profileFrom := rd.effectiveProfile(category)
	curated := map[string]bool{}
	for _, slug := range primaryParams {
		curated[slug] = true
	}

	q := url.Values{}
	q.Set("team_id", teamID)
	var observed []apitypes.CategoryParameter
	obsPath := fmt.Sprintf("/categories/%d/parameters", category.ID)
	if err := client.Get(ctx, obsPath, q, &observed); err != nil {
		return err
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i].DisplayName < observed[j].DisplayName })

	if ap.format == FormatJSON {
		out := map[string]any{
			"category": map[string]any{
				"id":   category.ID,
				"slug": category.Slug,
				"name": category.DisplayName,
				"path": rd.path(category),
			},
			"value_param":           valueParam,
			"primary_params":        primaryParams,
			"parameters":            observed,
			"excluded_range_params": rd.rangeParams(),
		}
		if profileFrom != nil && profileFrom.ID != category.ID {
			out["profile_inherited_from"] = map[string]any{
				"id": profileFrom.ID, "slug": profileFrom.Slug, "name": profileFrom.DisplayName,
			}
		}
		return writeJSON(ap.out, out)
	}

	pairs := [][2]string{
		{"Category", rd.path(category)},
		{"Id", strconv.FormatInt(int64(category.ID), 10)},
		{"Value param", strOr(&valueParam, "(none — not a parametric class)")},
	}
	if profileFrom != nil && profileFrom.ID != category.ID {
		pairs = append(pairs, [2]string{"Profile from", profileFrom.DisplayName})
	}
	writeKV(ap.out, pairs)

	if len(observed) == 0 {
		if len(primaryParams) > 0 {
			fmt.Fprintln(ap.out, "\nThis class has a curated profile, but no parts in this category carry parameter values yet.")
		} else {
			fmt.Fprintln(ap.out, "\nNo parameters found on parts in this category.")
		}
		return nil
	}

	writeSection(ap.out, "PARAMETERS")
	rows := make([][]string, 0, len(observed))
	for _, p := range observed {
		marker := ""
		switch {
		case valueParam != "" && valueParam == p.Slug:
			marker = " *"
		case curated[p.Slug]:
			marker = " +"
		}
		rows = append(rows, []string{
			p.Slug + marker,
			p.DisplayName,
			strOr(p.UnitSymbol, dash),
			p.ValueType,
			observedValues(p, filtersFlags.maxOptions),
		})
	}
	writeTable(ap.out, []string{"SLUG", "NAME", "UNIT", "TYPE", "IN YOUR CATALOG"}, rows)

	if valueParam != "" {
		fmt.Fprintln(ap.out, "* the magnitude a bare value refers to")
	}
	if len(curated) > 0 {
		fmt.Fprintln(ap.out, "+ part of the curated class profile")
	}

	// The server omits range-typed parameters from this list, so a part can show
	// "Operating Temp. -40°C ~ 125°C" in `pt show` and still not appear here.
	// Name them rather than letting the absence read as "not stored".
	if ranges := rd.rangeParams(); len(ranges) > 0 {
		slugs := make([]string, len(ranges))
		for i, p := range ranges {
			slugs[i] = p.Slug
		}
		fmt.Fprintf(ap.out,
			"\nNot listed: range parameters (%s) — the catalog endpoint excludes them.\n"+
				"They are filterable, but a bound matches the lower end of each part's span.\n",
			strings.Join(slugs, ", "))
	}

	fmt.Fprintf(ap.out, "\nFilter with: pt search -c %s --param <slug>=<value>\n", category.Slug)
	return nil
}

// observedValues renders what the catalog holds for one parameter: the enum
// vocabulary verbatim (matching is exact string equality), or the numeric span
// in engineering notation so you can see what a range filter has to cover.
func observedValues(p apitypes.CategoryParameter, max int) string {
	if len(p.EnumValues) > 0 {
		vals := append([]string(nil), p.EnumValues...)
		sort.Strings(vals)
		if max > 0 && len(vals) > max {
			return strings.Join(vals[:max], ", ") + fmt.Sprintf(", … (+%d)", len(vals)-max)
		}
		return strings.Join(vals, ", ")
	}
	if p.MinNumeric == nil && p.MaxNumeric == nil {
		return dash
	}
	unit := ""
	if p.UnitSymbol != nil {
		unit = *p.UnitSymbol
	}
	lo, hi := dash, dash
	if p.MinNumeric != nil {
		lo = paramparse.FormatEngineering(*p.MinNumeric, unit)
	}
	if p.MaxNumeric != nil {
		hi = paramparse.FormatEngineering(*p.MaxNumeric, unit)
	}
	if lo == hi {
		return lo
	}
	return lo + " .. " + hi
}

func init() {
	filtersCmd.Flags().IntVar(&filtersFlags.maxOptions, "max-values", 12,
		"maximum distinct values to list per parameter (0 for all)")
}
