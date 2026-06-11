// Package substitute holds the shared rule for choosing which approved
// substitute part fulfils a BOM line item in a build. The stock-check preview
// (internal/handler/stockcheck.go) and the persisted build selection
// (internal/worker/job_build_substitute_selections.go) both call Select so the
// number the user sees can never diverge from the choice the build records.
package substitute

// FreeStock returns on-hand minus already-reserved stock, floored at zero.
func FreeStock(onHand, reserved int32) int32 {
	free := onHand - reserved
	if free < 0 {
		return 0
	}
	return free
}

// Item is one BOM line item's demand within a build.
type Item struct {
	LineItemID string
	PrimaryID  string // resolved primary part id; "" when the line item is unresolved
	Required   int32  // qty-per-board * build quantity
}

// Select decides, per line item, which substitute part should fulfil the build.
//
// A substitute is chosen only when the primary part's free stock is insufficient
// AND the substitute's free stock fully covers the required demand (a build is
// never split across two parts for one line item). Substitutes for each line
// item must be supplied in priority order (highest priority first); the first
// one that fully covers wins.
//
// The returned map contains only line items where a substitute is chosen — an
// absent line item means "use the primary part".
func Select(items []Item, subsByItem map[string][]string, onHand, reserved map[string]int32) map[string]string {
	selected := make(map[string]string)
	for _, li := range items {
		if li.PrimaryID == "" || li.Required <= 0 {
			continue
		}
		if FreeStock(onHand[li.PrimaryID], reserved[li.PrimaryID]) >= li.Required {
			continue // primary covers it; no substitute needed
		}
		for _, subID := range subsByItem[li.LineItemID] {
			if FreeStock(onHand[subID], reserved[subID]) >= li.Required {
				selected[li.LineItemID] = subID
				break
			}
		}
	}
	return selected
}
