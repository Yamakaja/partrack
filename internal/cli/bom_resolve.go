package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Yamakaja/partrack/internal/apitypes"
)

// resolveForecast asks the server what importing these lines would do. It walks
// the real tier ladder against the real catalog (internal/bomresolve, shared
// with the import job), which is why this cannot be answered locally: the
// package matching, the value epsilon and the prior-revision lookups are all
// catalog-side, and approximating them here would produce a confident wrong
// answer.
func resolveForecast(ctx context.Context, items []apitypes.BOMImportItem, projectRef string) (*apitypes.BOMResolvePreview, string, error) {
	client, err := ap.requireAuthedClient()
	if err != nil {
		return nil, "", err
	}
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return nil, "", err
	}

	req := apitypes.BOMResolvePreviewRequest{Items: items}
	var projectName string
	if projectRef != "" {
		p, err := ap.resolveProjectRef(ctx, teamID, projectRef)
		if err != nil {
			return nil, "", err
		}
		req.ProjectID, projectName = p.ID, p.Name
	}

	var out apitypes.BOMResolvePreview
	if err := client.Post(ctx, "/teams/"+teamID+"/bom/resolve-preview", req, &out); err != nil {
		return nil, "", err
	}
	return &out, projectName, nil
}

// stateLabel names a state for display. The unresolved state is the empty
// string on the wire, which would otherwise print as a blank cell.
func stateLabel(state string) string {
	if state == "" {
		return "unresolved"
	}
	return state
}

// stateMeaning explains a resolution state in terms of what the user will have
// to do about it, which is the only reason they are reading this table.
func stateMeaning(state string) string {
	switch state {
	case "auto_sku":
		return "resolves — SKU matched a distributor listing"
	case "auto_mpn":
		return "resolves — exact MPN in your catalog"
	case "auto_mpn_fuzzy":
		return "resolves, but flagged — MPN matched as a substring; confirm at review"
	case "auto_prior_revision":
		return "resolves — a previous revision of this project already chose a part"
	case "auto_generic":
		return "resolves — one catalog part satisfies the parametric spec"
	case "ambiguous":
		return "needs you to pick — several parts match"
	case "importing":
		return "not in your library — will be looked up at the distributor"
	case "":
		return "unresolved — nothing matched and there is nothing to look up"
	}
	return ""
}

// printForecast renders the resolution outcome per state, then the lines that
// will not simply land.
func printForecast(out *apitypes.BOMResolvePreview, projectName string, rows []bomRow) {
	writeSection(ap.out, "WHAT THE IMPORT WOULD DO")
	if projectName != "" {
		fmt.Fprintf(ap.out, "Against project %s.\n\n", projectName)
	} else {
		fmt.Fprint(ap.out,
			"No project given, so the prior-revision tier is skipped: lines an earlier\n"+
				"revision already resolved will look unresolved here. Pass --project for that.\n\n")
	}

	// A stable order, best outcome first, so the shape of the answer is the same
	// on every BOM.
	order := []string{"auto_sku", "auto_mpn", "auto_prior_revision", "auto_generic",
		"auto_mpn_fuzzy", "ambiguous", "importing", ""}
	summary := make([][]string, 0, len(order))
	resolved := 0
	for _, state := range order {
		n := out.Summary[state]
		if n == 0 {
			continue
		}
		switch state {
		case "auto_sku", "auto_mpn", "auto_prior_revision", "auto_generic", "auto_mpn_fuzzy":
			resolved += n
		}
		summary = append(summary, []string{stateLabel(state), fmt.Sprint(n), stateMeaning(state)})
	}
	writeTable(ap.out, []string{"OUTCOME", "LINES", "MEANING"}, summary)
	fmt.Fprintf(ap.out, "\n%d of %d lines resolve without you.\n", resolved, len(out.Items))

	byLine := map[int]bomRow{}
	for i, r := range rows {
		byLine[i] = r
	}

	needsWork := make([][]string, 0)
	for _, it := range out.Items {
		switch it.State {
		case "auto_sku", "auto_mpn", "auto_prior_revision", "auto_generic":
			continue
		}
		r := byLine[it.Index]
		detail := stateMeaning(it.State)
		if n := len(it.Candidates); n > 0 {
			detail = fmt.Sprintf("%d candidates: %s", n, candidateSummary(it.Candidates))
		} else if it.Part != nil {
			detail = "→ " + partLabel(*it.Part)
		}
		needsWork = append(needsWork, []string{
			fmt.Sprint(r.Line), it.ReferenceDesignators,
			firstNonEmpty(r.Value, r.MPN, r.SKU, dash),
			stateLabel(it.State), detail,
		})
	}
	if len(needsWork) > 0 {
		// Includes auto_mpn_fuzzy, which does resolve: a substring MPN match is a
		// weaker claim than an exact one, so the review queue surfaces it for a
		// confirm and so does this.
		writeSection(ap.out, "LINES TO LOOK AT")
		writeTable(ap.out, []string{"LINE", "REFDES", "VALUE", "OUTCOME", "DETAIL"}, needsWork)
	}

	fmt.Fprint(ap.out,
		"\nForecast against the catalog as it stands. A line marked `importing` depends\n"+
			"on a distributor lookup this does not perform, and parts created during the\n"+
			"import can change what later lines match.\n")
}

func candidateSummary(cands []apitypes.BOMResolveCandidate) string {
	const show = 3
	names := make([]string, 0, show)
	for i, c := range cands {
		if i == show {
			names = append(names, fmt.Sprintf("… (+%d)", len(cands)-show))
			break
		}
		names = append(names, partLabel(c))
	}
	return strings.Join(names, ", ")
}

func partLabel(c apitypes.BOMResolveCandidate) string {
	label := strOr(c.Mpn, "")
	if label == "" {
		label = strOr(c.Description, c.PartID)
	}
	if c.QuantityOnHand != nil {
		label += fmt.Sprintf(" (%d in stock)", *c.QuantityOnHand)
	}
	return truncate(label, 40)
}
