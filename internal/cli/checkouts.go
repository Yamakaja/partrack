package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type checkoutsListFlags struct {
	fields []string
	limit  int
	offset int
}

var checkoutListFlags checkoutsListFlags

var checkoutsCmd = &cobra.Command{
	Use:     "checkouts",
	Aliases: []string{"checkout"},
	Short:   "Ad-hoc withdrawals of stock",
	Long: `A general checkout records parts taken out of stock without a build behind
them — bench work, repairs, samples.

  pt checkouts list
  pt checkouts show @1`,
}

var checkoutsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List general checkouts",
	Long: `List the team's checkouts, newest first, numbered so the next command can
address them as @1, @2, ….

  pt checkouts list
  pt checkouts list --limit 10`,
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheckoutsList(cmd.Context())
	},
}

var checkoutsShowCmd = &cobra.Command{
	Use:   "show <checkout>",
	Short: "Show one checkout and what it withdrew",
	Long: `Print a checkout's reason and every part it took out, with the ledger entry the
withdrawal produced.

A checkout has no name, so address it as @N from the last listing or by an id
prefix.

  pt checkouts show @1
  pt checkouts show 3f2a`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCheckoutsShow(cmd.Context(), args[0])
	},
}

var checkoutColumns = []column[CheckoutListItem]{
	{Key: "id", Header: "ID", Value: func(c CheckoutListItem) string { return shortID(c.ID) }},
	{Key: "uuid", Header: "UUID", Value: func(c CheckoutListItem) string { return c.ID }},
	{Key: "when", Header: "WHEN", Aliases: []string{"performed_at", "date"},
		Value: func(c CheckoutListItem) string { return shortDate(c.PerformedAt) }},
	{Key: "reason", Header: "REASON", Wide: true,
		Value: func(c CheckoutListItem) string { return str(c.Reason) }},
	{Key: "by", Header: "BY", Aliases: []string{"performed_by"},
		Value: func(c CheckoutListItem) string { return c.PerformedByDisplayName }},
	{Key: "items", Header: "ITEMS", Aliases: []string{"item_count"},
		Value: func(c CheckoutListItem) string { return strconv.FormatInt(c.ItemCount, 10) },
		JSON:  func(c CheckoutListItem) any { return c.ItemCount }},
}

var defaultCheckoutFields = []string{"id", "when", "reason", "by", "items"}

func runCheckoutsList(ctx context.Context) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("limit", strconv.Itoa(checkoutListFlags.limit))
	q.Set("offset", strconv.Itoa(checkoutListFlags.offset))

	var resp List[CheckoutListItem]
	if err := client.Get(ctx, "/teams/"+teamID+"/checkouts", q, &resp); err != nil {
		return err
	}

	cols, err := resolveFields(pickFieldsFrom(checkoutListFlags.fields, defaultCheckoutFields), checkoutColumns)
	if err != nil {
		return err
	}

	refs := make([]Ref, len(resp.Items))
	for i, c := range resp.Items {
		refs[i] = checkoutRef(c)
	}
	ap.saveResults(teamID, kindCheckout, refs)

	if ap.format == FormatJSON {
		rows := make([]map[string]any, len(resp.Items))
		for i, c := range resp.Items {
			row := jsonRow(c, cols)
			row["n"] = i + 1
			row["uuid"] = c.ID
			rows[i] = row
		}
		return writeJSON(ap.out, map[string]any{
			"total": resp.Total, "fetched": len(resp.Items), "items": rows,
		})
	}

	if len(resp.Items) == 0 {
		fmt.Fprintln(ap.out, "No checkouts recorded.")
		return nil
	}
	rows := make([][]string, len(resp.Items))
	for i, c := range resp.Items {
		rows[i] = numberedRow(i+1, rowFor(c, cols, 40))
	}
	writeTable(ap.out, numberedHeader(cols, nil), rows)
	fmt.Fprintln(ap.out, countFooter(len(resp.Items), resp.Total, "checkout"))
	return nil
}

func runCheckoutsShow(ctx context.Context, ref string) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	checkoutID, err := ap.resolveCheckoutRef(ctx, ref)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	var checkout CheckoutDetail
	if err := client.Get(ctx, "/teams/"+teamID+"/checkouts/"+checkoutID, nil, &checkout); err != nil {
		return err
	}
	ap.remember(teamID, Ref{Kind: kindCheckout, ID: checkout.ID, Label: checkout.PerformedAt})

	if ap.format == FormatJSON {
		return writeJSON(ap.out, checkout)
	}

	writeKV(ap.out, [][2]string{
		{"Checkout", shortID(checkout.ID)},
		{"When", checkout.PerformedAt},
		{"By", checkout.PerformedByDisplayName},
		{"Reason", str(checkout.Reason)},
	})

	if len(checkout.Items) == 0 {
		fmt.Fprintln(ap.out, "\nNothing was withdrawn.")
		return nil
	}
	writeSection(ap.out, "WITHDRAWN")
	rows := make([][]string, len(checkout.Items))
	total := int32(0)
	for i, li := range checkout.Items {
		total += li.Quantity
		rows[i] = []string{
			strOr(li.PartMpn, shortID(li.PartID)),
			strconv.FormatInt(int64(li.Quantity), 10),
			// The ledger entry is what makes the withdrawal auditable; a null one
			// means the deduction was never written, which is worth seeing.
			shortIDOrDash(li.StockAdjustmentID),
		}
	}
	writeTable(ap.out, []string{"MPN", "QTY", "LEDGER"}, rows)
	fmt.Fprintf(ap.out, "%d part%s, %d unit%s\n",
		len(checkout.Items), plural(len(checkout.Items)), total, plural(int(total)))
	return nil
}

// shortIDOrDash abbreviates a nullable id for a table cell.
func shortIDOrDash(id *string) string {
	if id == nil || *id == "" {
		return dash
	}
	return shortID(*id)
}

func init() {
	lf := checkoutsListCmd.Flags()
	lf.StringSliceVar(&checkoutListFlags.fields, "fields", nil,
		"columns to show, in order (default "+strings.Join(defaultCheckoutFields, ",")+")")
	lf.IntVar(&checkoutListFlags.limit, "limit", 50, "maximum rows to fetch (server caps a page at 200)")
	lf.IntVar(&checkoutListFlags.offset, "offset", 0, "row offset to start from")

	checkoutsCmd.AddCommand(checkoutsListCmd, checkoutsShowCmd)
}
