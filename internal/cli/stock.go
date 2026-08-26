package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var stockFlags struct {
	history int
}

var stockCmd = &cobra.Command{
	Use:   "stock <part>",
	Short: "Show on-hand, reserved and free stock for a part",
	Long: `Report what the team actually holds: quantity on hand, how much is reserved
against open builds, what is therefore free to use, the restock thresholds, and
the storage location.

Stock on hand is the running total of an immutable ledger; --history prints the
adjustments behind it, each with the event that caused it.

  pt stock STM32F103C8T6
  pt stock @1 --history=20`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStock(cmd.Context(), args[0])
	},
}

func runStock(ctx context.Context, ref string) error {
	detail, teamID, err := ap.resolveAndFetch(ctx, ref)
	if err != nil {
		return err
	}
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	allocations := fetchAllocations(ctx, client, teamID, detail.ID)

	var adjustments []StockAdjustment
	var adjTotal int64
	if stockFlags.history > 0 && detail.InventoryEntryID != nil {
		adjustments, adjTotal, err = fetchAdjustments(ctx, client, teamID, *detail.InventoryEntryID, stockFlags.history)
		if err != nil {
			return err
		}
	}

	onHand := int32(0)
	if detail.QuantityOnHand != nil {
		onHand = *detail.QuantityOnHand
	}
	var reserved int32
	for _, a := range allocations {
		reserved += a.QuantityReserved
	}

	if ap.format == FormatJSON {
		out := map[string]any{
			"part_id": detail.ID,
			"mpn":     detail.Mpn,
			"tracked": detail.InventoryEntryID != nil,
			"quantity_on_hand": func() any {
				if detail.InventoryEntryID == nil {
					return nil
				}
				return onHand
			}(),
			"quantity_reserved":      reserved,
			"quantity_free":          onHand - reserved,
			"minimum_stock_quantity": detail.MinimumStockQuantity,
			"reorder_quantity":       detail.ReorderQuantity,
			"storage_location":       detail.StorageLocation,
			"allocations":            allocations,
		}
		if stockFlags.history > 0 {
			out["adjustments"] = adjustments
			out["adjustments_total"] = adjTotal
		}
		return writeJSON(ap.out, out)
	}

	fmt.Fprintf(ap.out, "%s", strOr(detail.Mpn, detail.ID))
	if detail.Manufacturer != nil && *detail.Manufacturer != "" {
		fmt.Fprintf(ap.out, "  (%s)", *detail.Manufacturer)
	}
	fmt.Fprintln(ap.out)

	if detail.InventoryEntryID == nil {
		fmt.Fprintln(ap.out, "Not tracked by this team — no inventory entry exists.")
		return nil
	}

	pairs := [][2]string{
		{"On hand", strconv.FormatInt(int64(onHand), 10)},
		{"Reserved", strconv.FormatInt(int64(reserved), 10)},
		{"Free", strconv.FormatInt(int64(onHand-reserved), 10)},
		{"Minimum", num(detail.MinimumStockQuantity)},
		{"Reorder qty", num(detail.ReorderQuantity)},
		{"Location", locationString(detail.StorageLocation)},
	}
	if detail.MinimumStockQuantity != nil && onHand < *detail.MinimumStockQuantity {
		short := *detail.MinimumStockQuantity - onHand
		pairs = append(pairs, [2]string{"Status",
			fmt.Sprintf("below minimum by %d", short)})
	}
	writeKV(ap.out, pairs)

	if len(allocations) > 0 {
		printAllocations(allocations)
	}

	if stockFlags.history > 0 {
		if len(adjustments) == 0 {
			writeSection(ap.out, "LEDGER")
			fmt.Fprintln(ap.out, "No adjustments recorded.")
		} else {
			writeSection(ap.out, "LEDGER")
			rows := make([][]string, len(adjustments))
			for i, a := range adjustments {
				rows[i] = []string{
					refreshedAt(&a.AdjustedAt),
					signed(a.Delta),
					adjustmentSource(a),
					strOr(a.Reason, dash),
					a.CreatedByDisplayName,
				}
			}
			writeTable(ap.out, []string{"DATE", "DELTA", "SOURCE", "REASON", "BY"}, rows)
			if adjTotal > int64(len(adjustments)) {
				fmt.Fprintf(ap.out, "%d of %d adjustments\n", len(adjustments), adjTotal)
			}
		}
	}
	return nil
}

// signed renders a ledger delta with an explicit sign, so a withdrawal and a
// receipt are distinguishable at a glance.
func signed(v int32) string {
	if v > 0 {
		return "+" + strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatInt(int64(v), 10)
}

// adjustmentSource names the event behind a ledger row. A null source_type is a
// manual correction, which is meaningful rather than missing.
func adjustmentSource(a StockAdjustment) string {
	if a.SourceType == nil || *a.SourceType == "" {
		return "manual"
	}
	return strings.ReplaceAll(*a.SourceType, "_", " ")
}

func init() {
	stockCmd.Flags().IntVar(&stockFlags.history, "history", 0,
		"also print the N most recent stock adjustments")
	stockCmd.Flags().Lookup("history").NoOptDefVal = "20"
}
