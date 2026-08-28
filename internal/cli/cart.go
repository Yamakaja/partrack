package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type cartShowFlags struct {
	fields  []string
	demands bool
}

var cartFlags cartShowFlags

var cartCmd = &cobra.Command{
	Use:   "cart",
	Short: "The team's shopping cart",
	Long: `One cart per team: the staging area between deciding you need a part and
raising an order for it.

  pt cart show
  pt cart show --demands`,
}

var cartShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show what is in the cart",
	Long: `List the cart's items with the listing each will be ordered from.

A listing marked "auto" was picked by the team's sourcing policy rather than by
a person, and the reason column says which rule applied. --demands additionally
attributes each item's quantity to the pending builds that asked for it.

Reading the cart needs the purchaser role.

  pt cart show
  pt cart show --fields mpn,qty,dist,unit,line
  pt --format json cart show | jq '[.items[].line_total] | add'`,
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCartShow(cmd.Context())
	},
}

var cartColumns = []column[CartItem]{
	{Key: "id", Header: "ID", Value: func(c CartItem) string { return shortID(c.ID) }},
	{Key: "mpn", Header: "MPN", Value: func(c CartItem) string { return strOr(c.PartMpn, shortID(c.PartID)) }},
	{Key: "mfr", Header: "MFR", Aliases: []string{"manufacturer"},
		Value: func(c CartItem) string { return str(c.PartManufacturer) }},
	{Key: "desc", Header: "DESC", Aliases: []string{"description"}, Wide: true,
		Value: func(c CartItem) string {
			if c.PartShortDescription != nil && *c.PartShortDescription != "" {
				return *c.PartShortDescription
			}
			return str(c.PartDescription)
		}},
	{Key: "qty", Header: "QTY", Aliases: []string{"quantity"},
		Value: func(c CartItem) string { return strconv.FormatInt(int64(c.Quantity), 10) },
		JSON:  func(c CartItem) any { return c.Quantity }},
	{Key: "source", Header: "SOURCE",
		Value: func(c CartItem) string { return strings.ReplaceAll(c.Source, "_", " ") }},
	{Key: "dist", Header: "DISTRIBUTOR", Aliases: []string{"distributor"},
		Value: func(c CartItem) string {
			if c.Listing == nil {
				return dash
			}
			return strOr(c.Listing.DistributorName, dash)
		}},
	{Key: "sku", Header: "SKU", Value: func(c CartItem) string {
		if c.Listing == nil {
			return dash
		}
		return strOr(c.Listing.SKU, dash)
	}},
	{Key: "unit", Header: "UNIT", Aliases: []string{"unit_price"},
		Value: func(c CartItem) string {
			if c.Listing == nil {
				return dash
			}
			return str(c.Listing.UnitPrice)
		}},
	{Key: "order_qty", Header: "ORDER QTY", Aliases: []string{"optimized_qty"},
		Value: func(c CartItem) string {
			if c.Listing == nil {
				return dash
			}
			return strconv.FormatInt(int64(c.Listing.OptimizedQty), 10)
		},
		JSON: func(c CartItem) any {
			if c.Listing == nil {
				return nil
			}
			return c.Listing.OptimizedQty
		}},
	{Key: "line", Header: "LINE", Aliases: []string{"line_total"},
		Value: func(c CartItem) string { return strOr(lineTotal(c), dash) },
		JSON: func(c CartItem) any {
			if t := lineTotal(c); t != nil {
				if v, err := strconv.ParseFloat(*t, 64); err == nil {
					return v
				}
			}
			return nil
		}},
	{Key: "chosen", Header: "CHOSEN",
		Value: func(c CartItem) string {
			if c.Listing == nil {
				return dash
			}
			if c.Listing.Chosen {
				return "by hand"
			}
			return "auto"
		},
		JSON: func(c CartItem) any {
			if c.Listing == nil {
				return nil
			}
			return c.Listing.Chosen
		}},
	{Key: "reason", Header: "REASON", Wide: true,
		Value: func(c CartItem) string {
			if c.Listing == nil || c.Listing.Reason == "" {
				return dash
			}
			r := strings.ReplaceAll(c.Listing.Reason, "_", " ")
			if c.Listing.PreferredStale {
				// The preference was honoured against stock data old enough that the
				// server flagged it, which changes how much the choice is worth.
				r += " (stale stock)"
			}
			return r
		}},
}

var defaultCartFields = []string{"mpn", "mfr", "qty", "dist", "sku", "unit", "order_qty", "line", "chosen"}

// lineTotal is the optimized order quantity at the resolved unit price, which is
// what the line will actually cost — not quantity × unit price.
func lineTotal(c CartItem) *string {
	if c.Listing == nil || c.Listing.UnitPrice == nil {
		return nil
	}
	unit, err := strconv.ParseFloat(*c.Listing.UnitPrice, 64)
	if err != nil {
		return nil
	}
	qty := c.Listing.OptimizedQty
	if qty == 0 {
		qty = c.Quantity
	}
	s := strconv.FormatFloat(unit*float64(qty), 'f', 2, 64)
	return &s
}

func runCartShow(ctx context.Context) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	var cart struct {
		Items []CartItem `json:"items"`
	}
	if err := client.Get(ctx, "/teams/"+teamID+"/cart", nil, &cart); err != nil {
		return err
	}

	cols, err := resolveFields(pickFieldsFrom(cartFlags.fields, defaultCartFields), cartColumns)
	if err != nil {
		return err
	}

	// Cart rows address parts: `pt show @2` after `pt cart show` is the useful
	// follow-up, and there is no `pt cart show <item>` to want an item id for.
	refs := make([]Ref, len(cart.Items))
	for i, it := range cart.Items {
		refs[i] = identOf(it.PartID, it.PartMpn, it.PartManufacturer)
	}
	ap.saveResults(teamID, kindPart, refs)

	if ap.format == FormatJSON {
		rows := make([]map[string]any, len(cart.Items))
		for i, it := range cart.Items {
			row := jsonRow(it, cols)
			row["n"] = i + 1
			row["part_id"] = it.PartID
			if cartFlags.demands {
				row["build_demands"] = it.BuildDemands
			}
			rows[i] = row
		}
		return writeJSON(ap.out, map[string]any{"items": rows, "total": len(rows)})
	}

	if len(cart.Items) == 0 {
		fmt.Fprintln(ap.out, "The cart is empty.")
		return nil
	}

	rows := make([][]string, len(cart.Items))
	for i, it := range cart.Items {
		rows[i] = numberedRow(i+1, rowFor(it, cols, 34))
	}
	writeTable(ap.out, numberedHeader(cols, nil), rows)

	printCartTotals(cart.Items)

	if cartFlags.demands {
		printCartDemands(cart.Items)
	}
	return nil
}

// printCartTotals reports the money and the gaps. An item with no listing cannot
// be ordered at all, so it is called out rather than silently costing nothing.
func printCartTotals(items []CartItem) {
	var total float64
	priced, unlisted := 0, 0
	for _, it := range items {
		if it.Listing == nil {
			unlisted++
			continue
		}
		if t := lineTotal(it); t != nil {
			if v, err := strconv.ParseFloat(*t, 64); err == nil {
				total += v
				priced++
			}
		}
	}
	fmt.Fprintf(ap.out, "%s", countFooter(len(items), int64(len(items)), "item"))
	if priced > 0 {
		fmt.Fprintf(ap.out, ", %s across %d priced line%s",
			strconv.FormatFloat(total, 'f', 2, 64), priced, plural(priced))
	}
	fmt.Fprintln(ap.out)
	if unlisted > 0 {
		fmt.Fprintf(ap.out,
			"%d item%s has no listing and cannot be ordered — no distributor stocks it, or none is configured.\n",
			unlisted, plural(unlisted))
	}
}

func printCartDemands(items []CartItem) {
	writeSection(ap.out, "WHY THESE ARE HERE")
	var rows [][]string
	for _, it := range items {
		for _, d := range it.BuildDemands {
			rows = append(rows, []string{
				strOr(it.PartMpn, shortID(it.PartID)),
				truncate(d.ProjectName, 26),
				d.BomRevisionLabel,
				strconv.FormatInt(int64(d.QuantityNeeded), 10),
			})
		}
	}
	if len(rows) == 0 {
		fmt.Fprintln(ap.out, "No pending build accounts for any of these — every item was added by hand.")
		return
	}
	writeTable(ap.out, []string{"MPN", "PROJECT", "REVISION", "NEEDED"}, rows)
}

func init() {
	f := cartShowCmd.Flags()
	f.StringSliceVar(&cartFlags.fields, "fields", nil,
		"columns to show, in order (default "+strings.Join(defaultCartFields, ",")+")")
	f.BoolVar(&cartFlags.demands, "demands", false,
		"also show which pending builds each item's quantity comes from")

	cartCmd.AddCommand(cartShowCmd)
}
