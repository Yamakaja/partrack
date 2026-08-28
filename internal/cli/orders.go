package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type ordersListFlags struct {
	status      []string
	distributor string
	fields      []string
	limit       int
	offset      int
}

type ordersShowFlags struct {
	distributorStatus bool
}

var (
	orderListFlags ordersListFlags
	orderShowFlags ordersShowFlags
)

var ordersCmd = &cobra.Command{
	Use:   "orders",
	Short: "Purchase orders raised with your distributors",
	Long: `List and inspect the orders your team has placed.

  pt orders list --status open
  pt orders show @1`,
}

var ordersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List purchase orders",
	Long: `List the team's orders, newest first, numbered so the next command can address
them as @1, @2, ….

  pt orders list
  pt orders list --status draft,submitted
  pt orders list --distributor DigiKey --fields id,dist,status,lines,total`,
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOrdersList(cmd.Context())
	},
}

var ordersShowCmd = &cobra.Command{
	Use:   "show <order>",
	Short: "Show one order with its line items",
	Long: `Print an order's header and every line on it, with the price snapshot taken
when the line was created — not the listing's price today.

An order is addressed by @N from the last listing, by an id prefix, by its
external reference, or by distributor name when that is unambiguous.

  pt orders show @1
  pt orders show 3f2a
  pt orders show --distributor-status @1`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOrdersShow(cmd.Context(), args[0])
	},
}

// orderStatuses is the enum the server stores. Listed here so an unknown value
// fails locally with the vocabulary rather than as a 400 from the server.
var orderStatuses = []string{
	"draft", "exported", "submitted", "confirmed", "shipped", "received", "cancelled",
}

// normalizeOrderStatus maps friendly spellings onto the enum. "open" is the
// question people actually ask — everything not yet received or cancelled.
func normalizeOrderStatus(vals []string) ([]string, error) {
	var out []string
	for _, v := range splitList(vals) {
		lower := strings.ToLower(strings.ReplaceAll(v, "-", "_"))
		switch lower {
		case "open", "outstanding":
			out = append(out, "draft", "exported", "submitted", "confirmed", "shipped")
			continue
		}
		valid := false
		for _, s := range orderStatuses {
			if s == lower {
				valid = true
				break
			}
		}
		if !valid {
			return nil, exitErrf(ExitUsage,
				"unknown order status %q — one of: %s, or `open` for everything still outstanding",
				v, strings.Join(orderStatuses, ", "))
		}
		out = append(out, lower)
	}
	return out, nil
}

var orderColumns = []column[OrderListItem]{
	{Key: "id", Header: "ID", Value: func(o OrderListItem) string { return shortID(o.ID) }},
	{Key: "uuid", Header: "UUID", Value: func(o OrderListItem) string { return o.ID }},
	{Key: "dist", Header: "DISTRIBUTOR", Aliases: []string{"distributor"},
		Value: func(o OrderListItem) string { return o.DistributorName }},
	{Key: "status", Header: "STATUS", Value: func(o OrderListItem) string { return o.Status }},
	{Key: "ref", Header: "REF", Aliases: []string{"external_reference", "reference"},
		Value: func(o OrderListItem) string { return str(o.ExternalReference) }},
	{Key: "lines", Header: "LINES", Aliases: []string{"line_items", "line_item_count"},
		Value: func(o OrderListItem) string { return strconv.FormatInt(o.LineItemCount, 10) },
		JSON:  func(o OrderListItem) any { return o.LineItemCount }},
	{Key: "total", Header: "TOTAL", Aliases: []string{"total_price"},
		Value: func(o OrderListItem) string { return str(o.TotalPrice) }},
	{Key: "created", Header: "CREATED", Aliases: []string{"created_at"},
		Value: func(o OrderListItem) string { return shortDate(o.CreatedAt) }},
	{Key: "by", Header: "BY", Aliases: []string{"created_by"},
		Value: func(o OrderListItem) string { return o.CreatedByDisplayName }},
}

var defaultOrderFields = []string{"id", "dist", "status", "ref", "lines", "total", "created"}

// fetchOrders lists orders with the given query. It is shared by the list command
// and by order reference resolution.
func (a *app) fetchOrders(ctx context.Context, teamID string, q url.Values) (*List[OrderListItem], error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	var resp List[OrderListItem]
	if err := client.Get(ctx, "/teams/"+teamID+"/orders", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func runOrdersList(ctx context.Context) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}

	statuses, err := normalizeOrderStatus(orderListFlags.status)
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("limit", strconv.Itoa(orderListFlags.limit))
	q.Set("offset", strconv.Itoa(orderListFlags.offset))
	if orderListFlags.distributor != "" {
		id, err := ap.resolveDistributor(ctx, orderListFlags.distributor)
		if err != nil {
			return err
		}
		q.Set("distributor_id", id)
	}

	// The endpoint takes a single status. Asking for several — which `--status
	// open` always does — means one request each, merged here.
	var items []OrderListItem
	var total int64
	switch len(statuses) {
	case 0:
		resp, err := ap.fetchOrders(ctx, teamID, q)
		if err != nil {
			return err
		}
		items, total = resp.Items, resp.Total
	default:
		seen := map[string]bool{}
		for _, s := range statuses {
			sq := cloneValues(q)
			sq.Set("status", s)
			resp, err := ap.fetchOrders(ctx, teamID, sq)
			if err != nil {
				return err
			}
			for _, o := range resp.Items {
				if !seen[o.ID] {
					seen[o.ID] = true
					items = append(items, o)
				}
			}
			total += resp.Total
		}
		sortByCreatedDesc(items, func(o OrderListItem) string { return o.CreatedAt })
	}

	cols, err := resolveFields(pickFieldsFrom(orderListFlags.fields, defaultOrderFields), orderColumns)
	if err != nil {
		return err
	}

	refs := make([]Ref, len(items))
	for i, o := range items {
		refs[i] = orderRef(o)
	}
	ap.saveResults(teamID, kindOrder, refs)

	if ap.format == FormatJSON {
		rows := make([]map[string]any, len(items))
		for i, o := range items {
			row := jsonRow(o, cols)
			row["n"] = i + 1
			row["uuid"] = o.ID
			rows[i] = row
		}
		return writeJSON(ap.out, map[string]any{
			"total": total, "fetched": len(items), "items": rows,
		})
	}

	if len(items) == 0 {
		fmt.Fprintln(ap.out, "No orders matched.")
		return nil
	}
	rows := make([][]string, len(items))
	for i, o := range items {
		rows[i] = numberedRow(i+1, rowFor(o, cols, 0))
	}
	writeTable(ap.out, numberedHeader(cols, nil), rows)
	fmt.Fprintln(ap.out, countFooter(len(items), total, "order"))
	return nil
}

func runOrdersShow(ctx context.Context, ref string) error {
	teamID, err := ap.teamID(ctx)
	if err != nil {
		return err
	}
	orderID, err := ap.resolveOrderRef(ctx, teamID, ref)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	var order OrderDetail
	if err := client.Get(ctx, "/teams/"+teamID+"/orders/"+orderID, nil, &order); err != nil {
		return err
	}
	ap.remember(teamID, Ref{Kind: kindOrder, ID: order.ID,
		Label: strOr(order.ExternalReference, order.DistributorName)})

	var builds []LinkedBuild
	if err := client.Get(ctx, "/teams/"+teamID+"/orders/"+orderID+"/linked-builds", nil, &builds); err != nil {
		return err
	}

	// The distributor-status endpoint calls the distributor and caches what comes
	// back onto the order, so it stays behind an explicit flag.
	var distStatus map[string]any
	if orderShowFlags.distributorStatus {
		if err := client.Get(ctx, "/teams/"+teamID+"/orders/"+orderID+"/distributor-status", nil, &distStatus); err != nil {
			return err
		}
	}

	if ap.format == FormatJSON {
		out := map[string]any{"order": order, "linked_builds": builds}
		if orderShowFlags.distributorStatus {
			out["distributor_status"] = distStatus
		}
		return writeJSON(ap.out, out)
	}

	fmt.Fprintf(ap.out, "%s order %s\n", order.DistributorName, shortID(order.ID))
	pairs := [][2]string{
		{"Status", order.Status},
		{"Reference", str(order.ExternalReference)},
		{"PO number", str(order.PurchaseOrderNumber)},
		{"Submitted via", str(order.SubmissionMethod)},
		{"Created", strings.TrimSpace(order.CreatedAt + "  by " + order.CreatedByDisplayName)},
		{"Received", str(order.ReceivedAt)},
		{"Lines", strconv.Itoa(order.LineItemCount)},
	}
	if order.Notes != nil && *order.Notes != "" {
		pairs = append(pairs, [2]string{"Notes", *order.Notes})
	}
	writeKV(ap.out, pairs)

	if len(order.Items) > 0 {
		writeSection(ap.out, "LINE ITEMS")
		rows := make([][]string, len(order.Items))
		for i, li := range order.Items {
			rows[i] = []string{
				strOr(li.PartMpn, shortID(li.PartID)),
				str(li.PartManufacturer),
				li.Sku,
				li.PackagingType,
				strconv.FormatInt(int64(li.QuantityOrdered), 10),
				strconv.FormatInt(int64(li.QuantityReceived), 10),
				str(li.UnitPriceSnapshot),
			}
		}
		writeTable(ap.out, []string{"MPN", "MFR", "SKU", "PKG", "ORDERED", "RECEIVED", "UNIT"}, rows)
	}

	if len(builds) > 0 {
		writeSection(ap.out, "RAISED FOR")
		rows := make([][]string, len(builds))
		for i, b := range builds {
			rows[i] = []string{
				shortID(b.ID), b.ProjectName, b.BomRevisionLabel,
				strconv.FormatInt(int64(b.Quantity), 10), b.Status,
				fmt.Sprintf("%d/%d", b.FulfilledLineItems, b.TotalLineItems),
			}
		}
		writeTable(ap.out, []string{"BUILD", "PROJECT", "REVISION", "QTY", "STATUS", "FULFILLED"}, rows)
	}

	if orderShowFlags.distributorStatus && distStatus != nil {
		writeSection(ap.out, "DISTRIBUTOR STATUS")
		printFlatMap(distStatus, []string{
			"status", "status_detail", "order_number", "total_price",
			"currency", "ship_method", "last_checked_at",
		})
	}
	return nil
}

func init() {
	lf := ordersListCmd.Flags()
	lf.StringSliceVar(&orderListFlags.status, "status", nil,
		"order status: draft, exported, submitted, confirmed, shipped, received, cancelled, or `open`")
	lf.StringVar(&orderListFlags.distributor, "distributor", "", "distributor by name or id")
	lf.StringSliceVar(&orderListFlags.fields, "fields", nil,
		"columns to show, in order (default "+strings.Join(defaultOrderFields, ",")+")")
	lf.IntVar(&orderListFlags.limit, "limit", 50, "maximum rows to fetch (server caps a page at 200)")
	lf.IntVar(&orderListFlags.offset, "offset", 0, "row offset to start from")

	ordersShowCmd.Flags().BoolVar(&orderShowFlags.distributorStatus, "distributor-status", false,
		"also ask the distributor for live order status (a network call to them, and it "+
			"updates the order's cached status)")

	ordersCmd.AddCommand(ordersListCmd, ordersShowCmd)
}
