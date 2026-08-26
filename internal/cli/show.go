package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var showFlags struct {
	only []string
}

// showSections are the blocks `pt show` can print, in display order.
var showSections = []string{"identity", "inventory", "params", "listings", "note", "allocations"}

var showCmd = &cobra.Command{
	Use:   "show <part>",
	Short: "Show everything known about one part",
	Long: `Print a part's dossier: identity and lifecycle, the team's inventory profile
and location, every parameter with its source, distributor listings with stock
and price breaks, the team's note and flag, and any active build reservations.

The part may be given as an MPN, a full or abbreviated id, or @N from the last
search. Restrict the output with --only:

  pt show STM32F103C8T6
  pt show @2 --only params
  pt show 3f2a --only listings,inventory --format json`,
	Args: usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShow(cmd.Context(), args[0])
	},
}

func runShow(ctx context.Context, ref string) error {
	detail, teamID, err := ap.resolveAndFetch(ctx, ref)
	if err != nil {
		return err
	}
	want, err := selectedSections(showFlags.only)
	if err != nil {
		return err
	}

	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}

	// The note and allocations are separate endpoints, fetched only when their
	// section is going to be shown.
	var note *partNote
	if want["note"] {
		note = fetchNote(ctx, client, teamID, detail.ID)
	}
	var allocations []PartAllocation
	if want["allocations"] {
		allocations = fetchAllocations(ctx, client, teamID, detail.ID)
	}

	if ap.format == FormatJSON {
		out := map[string]any{"part": detail}
		if note != nil {
			out["note"] = note
		}
		if want["allocations"] {
			out["allocations"] = allocations
		}
		return writeJSON(ap.out, out)
	}

	if want["identity"] {
		printIdentity(detail, note)
	}
	if want["inventory"] {
		printInventory(detail, allocations)
	}
	if want["params"] {
		printParameters(detail)
	}
	if want["listings"] {
		printListings(detail)
	}
	if want["note"] && note != nil && strings.TrimSpace(note.Body) != "" {
		writeSection(ap.out, "NOTE")
		fmt.Fprintln(ap.out, strings.TrimRight(note.Body, "\n"))
	}
	if want["allocations"] && len(allocations) > 0 {
		printAllocations(allocations)
	}
	return nil
}

// selectedSections turns --only into a lookup, defaulting to everything.
func selectedSections(only []string) (map[string]bool, error) {
	want := map[string]bool{}
	names := splitList(only)
	if len(names) == 0 {
		for _, s := range showSections {
			want[s] = true
		}
		return want, nil
	}
	for _, n := range names {
		n = strings.ToLower(n)
		switch n {
		case "parameters", "specs":
			n = "params"
		case "stock":
			n = "inventory"
		}
		if !contains(showSections, n) {
			return nil, exitErrf(ExitUsage, "unknown section %q — available: %s",
				n, strings.Join(showSections, ", "))
		}
		want[n] = true
	}
	return want, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func printIdentity(p *PartDetail, note *partNote) {
	pairs := [][2]string{
		{"MPN", str(p.Mpn)},
		{"Manufacturer", str(p.Manufacturer)},
		{"Description", strOr(p.Description, strOr(p.ShortDescription, dash))},
		{"Category", strOr(p.CategoryName, strOr(p.Category, dash))},
		{"Package", str(p.Package)},
		{"Lifecycle", p.LifecycleStatus},
		{"Scope", p.Scope},
		{"Id", p.ID},
	}
	if note != nil && note.Flag != "" && note.Flag != "none" {
		pairs = append(pairs, [2]string{"Flag", note.Flag})
	}
	if len(p.Aliases) > 0 {
		pairs = append(pairs, [2]string{"Aliases", strings.Join(p.Aliases, ", ")})
	}
	if p.DatasheetURL != nil && *p.DatasheetURL != "" {
		pairs = append(pairs, [2]string{"Datasheet", *p.DatasheetURL})
	}
	if p.ProductURL != nil && *p.ProductURL != "" {
		pairs = append(pairs, [2]string{"Product page", *p.ProductURL})
	}
	if p.EnrichedAt != nil && *p.EnrichedAt != "" {
		pairs = append(pairs, [2]string{"Enriched", *p.EnrichedAt})
	}
	writeKV(ap.out, pairs)
}

func printInventory(p *PartDetail, allocations []PartAllocation) {
	writeSection(ap.out, "INVENTORY")
	if p.InventoryEntryID == nil {
		fmt.Fprintln(ap.out, "Not tracked by this team.")
		return
	}
	onHand := int32(0)
	if p.QuantityOnHand != nil {
		onHand = *p.QuantityOnHand
	}
	reserved := int32(0)
	for _, a := range allocations {
		reserved += a.QuantityReserved
	}
	pairs := [][2]string{{"On hand", strconv.FormatInt(int64(onHand), 10)}}
	// Reservations are only known when the allocations section was fetched, so
	// free stock is reported only then — never guessed.
	if len(allocations) > 0 {
		pairs = append(pairs,
			[2]string{"Reserved", strconv.FormatInt(int64(reserved), 10)},
			[2]string{"Free", strconv.FormatInt(int64(onHand-reserved), 10)})
	}
	pairs = append(pairs,
		[2]string{"Minimum", num(p.MinimumStockQuantity)},
		[2]string{"Reorder qty", num(p.ReorderQuantity)},
		[2]string{"Location", locationString(p.StorageLocation)})
	if p.MinimumStockQuantity != nil && onHand < *p.MinimumStockQuantity {
		pairs = append(pairs, [2]string{"Status", "below minimum"})
	}
	writeKV(ap.out, pairs)
}

// locationString renders the full location path plus the grid coordinate.
func locationString(loc *StorageLocationRef) string {
	if loc == nil {
		return dash
	}
	label := loc.Label
	if loc.AncestorPath != nil && *loc.AncestorPath != "" {
		label = *loc.AncestorPath + " / " + label
	}
	return formatLocation(&label, loc.Coordinate)
}

func printParameters(p *PartDetail) {
	total := len(p.Parameters.UserSpecified) + len(p.Parameters.DistributorSourced)
	if total == 0 {
		return
	}
	writeSection(ap.out, "PARAMETERS")

	// User-specified and distributor-sourced parameters are separate types on the
	// wire — a user value carries no source distributor at all — so they are
	// flattened into display rows here rather than into one parameter slice.
	rows := make([][]string, 0, total)
	unmapped := 0
	appendRow := func(prm PartParameter, source string) {
		name := prm.Name
		if prm.ParameterDisplayName != nil && *prm.ParameterDisplayName != "" {
			name = *prm.ParameterDisplayName
		}
		// A parameter with no canonical definition behind it has not been mapped
		// in the registry yet, so `pt search --param` can never match it — the
		// filters key on parameter_id. Mark it, or its absence from `pt filters`
		// looks like the value simply is not stored.
		slug := dash
		if prm.ParameterSlug != nil && *prm.ParameterSlug != "" {
			slug = *prm.ParameterSlug
		}
		if prm.ParameterID == nil {
			slug = "(unmapped)"
			unmapped++
		}
		rows = append(rows, []string{name, prm.Value, slug, source})
	}
	for _, prm := range p.Parameters.UserSpecified {
		appendRow(prm.PartParameter, "user")
	}
	for _, prm := range p.Parameters.DistributorSourced {
		appendRow(prm.PartParameter, strOr(prm.SourceDistributorName, "distributor"))
	}
	// User values win over distributor ones, so they stay first; within each
	// group, alphabetical by name.
	sort.SliceStable(rows, func(i, j int) bool {
		iUser, jUser := rows[i][3] == "user", rows[j][3] == "user"
		if iUser != jUser {
			return iUser
		}
		return rows[i][0] < rows[j][0]
	})
	writeTable(ap.out, []string{"PARAMETER", "VALUE", "SLUG", "SOURCE"}, rows)
	if unmapped > 0 {
		fmt.Fprintf(ap.out,
			"%d parameter(s) are not mapped to the registry and cannot be filtered on; "+
				"an admin links them under Parameter Registry.\n", unmapped)
	}
}

func printListings(p *PartDetail) {
	if len(p.Listings) == 0 {
		return
	}
	writeSection(ap.out, "LISTINGS")
	rows := make([][]string, len(p.Listings))
	for i, l := range p.Listings {
		marker := ""
		if p.PreferredListingID != nil && *p.PreferredListingID == l.ID {
			marker = " *"
		}
		rows[i] = []string{
			l.DistributorName + marker,
			l.Sku,
			l.PackagingType,
			num(l.StockAvailable),
			str(l.UnitPrice),
			strconv.FormatInt(int64(l.Moq), 10),
			formatPriceBreaks(l.PriceBreaks),
			refreshedAt(l.LastRefreshedAt),
		}
	}
	writeTable(ap.out, []string{"DISTRIBUTOR", "SKU", "PACKAGING", "STOCK", "PRICE", "MOQ", "BREAKS", "REFRESHED"}, rows)
	if p.PreferredListingID != nil {
		fmt.Fprintln(ap.out, "* preferred listing")
	}
}

// formatPriceBreaks condenses the JSONB tier list into "1:0.12 10:0.09 100:0.07".
func formatPriceBreaks(raw json.RawMessage) string {
	if len(raw) == 0 {
		return dash
	}
	var breaks []PriceBreak
	if err := json.Unmarshal(raw, &breaks); err != nil || len(breaks) == 0 {
		return dash
	}
	sort.Slice(breaks, func(i, j int) bool { return breaks[i].Quantity < breaks[j].Quantity })
	parts := make([]string, 0, len(breaks))
	for _, b := range breaks {
		parts = append(parts, fmt.Sprintf("%d:%s",
			b.Quantity, strconv.FormatFloat(b.UnitPrice, 'f', -1, 64)))
	}
	return strings.Join(parts, " ")
}

// refreshedAt shortens an RFC3339 timestamp to a date, which is the resolution
// that matters for "how stale is this price".
func refreshedAt(ts *string) string {
	if ts == nil || *ts == "" {
		return "never"
	}
	if len(*ts) >= 10 {
		return (*ts)[:10]
	}
	return *ts
}

func printAllocations(allocations []PartAllocation) {
	writeSection(ap.out, "RESERVED FOR BUILDS")
	rows := make([][]string, len(allocations))
	for i, a := range allocations {
		rows[i] = []string{
			a.ProjectName,
			a.BuildStatus,
			strconv.FormatInt(int64(a.BuildQuantity), 10),
			strconv.FormatInt(int64(a.QuantityNeeded), 10),
			strconv.FormatInt(int64(a.QuantityReserved), 10),
			shortID(a.BuildID),
		}
	}
	writeTable(ap.out, []string{"PROJECT", "STATUS", "BOARDS", "NEEDED", "RESERVED", "BUILD"}, rows)
}

// partNote is GET /teams/{t}/parts/{id}/note.
type partNote struct {
	Body      string `json:"body"`
	Flag      string `json:"flag"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// fetchNote reads the team's note. A failure is not fatal — the rest of the
// dossier is still worth printing — so it returns nil on error.
func fetchNote(ctx context.Context, client *Client, teamID, partID string) *partNote {
	var note partNote
	if err := client.Get(ctx, "/teams/"+teamID+"/parts/"+partID+"/note", nil, &note); err != nil {
		return nil
	}
	return &note
}

// fetchAllocations reads active build reservations, tolerating failure the same
// way fetchNote does.
func fetchAllocations(ctx context.Context, client *Client, teamID, partID string) []PartAllocation {
	var resp struct {
		Items []PartAllocation `json:"items"`
	}
	if err := client.Get(ctx, "/teams/"+teamID+"/parts/"+partID+"/allocations", nil, &resp); err != nil {
		return nil
	}
	return resp.Items
}

// fetchAdjustments reads a page of the stock ledger for an inventory entry.
func fetchAdjustments(ctx context.Context, client *Client, teamID, entryID string, limit int) ([]StockAdjustment, int64, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	var resp struct {
		Total int64             `json:"total"`
		Items []StockAdjustment `json:"items"`
	}
	if err := client.Get(ctx, "/teams/"+teamID+"/inventory/"+entryID+"/adjustments", q, &resp); err != nil {
		return nil, 0, err
	}
	return resp.Items, resp.Total, nil
}

func init() {
	showCmd.Flags().StringSliceVar(&showFlags.only, "only", nil,
		"sections to print: "+strings.Join(showSections, ", "))
}
