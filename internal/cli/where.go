package cli

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var whereFlags struct {
	location string
	limit    int
}

var whereCmd = &cobra.Command{
	Use:   "where [part]",
	Short: "Locate a part, or list what is stored in a location",
	Long: `With a part reference, print where it lives — the full location path and grid
coordinate. With --location, print what is stored there instead, including every
descendant location. With neither, print the storage tree.

  pt where STM32F103C8T6
  pt where --location "Drawer A"
  pt where`,
	Args: usageArgs(cobra.MaximumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		teamID, err := ap.teamID(ctx)
		if err != nil {
			return err
		}
		switch {
		case len(args) == 1:
			return runWherePart(ctx, args[0])
		case whereFlags.location != "":
			return runWhereLocation(ctx, teamID, whereFlags.location)
		default:
			return runStorageTree(ctx, teamID)
		}
	},
}

func runWherePart(ctx context.Context, ref string) error {
	detail, _, err := ap.resolveAndFetch(ctx, ref)
	if err != nil {
		return err
	}
	if ap.format == FormatJSON {
		return writeJSON(ap.out, map[string]any{
			"part_id":          detail.ID,
			"mpn":              detail.Mpn,
			"storage_location": detail.StorageLocation,
			"quantity_on_hand": detail.QuantityOnHand,
		})
	}
	if detail.StorageLocation == nil {
		fmt.Fprintf(ap.out, "%s has no storage location assigned.\n", strOr(detail.Mpn, detail.ID))
		return exitErrf(ExitNotFound, "no location for %s", strOr(detail.Mpn, detail.ID))
	}
	writeKV(ap.out, [][2]string{
		{"Part", strOr(detail.Mpn, detail.ID)},
		{"Location", locationString(detail.StorageLocation)},
		{"On hand", num(detail.QuantityOnHand)},
	})
	return nil
}

func runWhereLocation(ctx context.Context, teamID, ref string) error {
	client, err := ap.requireAuthedClient()
	if err != nil {
		return err
	}
	locID, locLabel, err := ap.resolveLocation(ctx, teamID, ref)
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("limit", strconv.Itoa(whereFlags.limit))
	var resp struct {
		Total int64 `json:"total"`
		Items []struct {
			ID                string  `json:"id"`
			PartID            string  `json:"part_id"`
			PartMPN           *string `json:"part_mpn"`
			PartManufacturer  *string `json:"part_manufacturer"`
			PartDescription   *string `json:"part_description"`
			QuantityOnHand    int32   `json:"quantity_on_hand"`
			StorageCoordinate []int32 `json:"storage_coordinate"`
			LocationLabel     *string `json:"storage_location_label"`
		} `json:"items"`
	}
	if err := client.Get(ctx, "/teams/"+teamID+"/storage-locations/"+locID+"/inventory", q, &resp); err != nil {
		return err
	}

	if ap.format == FormatJSON {
		return writeJSON(ap.out, resp)
	}
	if len(resp.Items) == 0 {
		fmt.Fprintln(ap.out, "Nothing stored here.")
		return nil
	}
	// The listing includes descendant locations, so a row's label is only worth
	// repeating when it sits somewhere other than the location asked for.
	rows := make([][]string, len(resp.Items))
	idents := make([]Ref, len(resp.Items))
	for i, it := range resp.Items {
		rows[i] = numberedRow(i+1, []string{
			str(it.PartMPN),
			str(it.PartManufacturer),
			truncate(strOr(it.PartDescription, dash), 40),
			strconv.FormatInt(int64(it.QuantityOnHand), 10),
			cellWithin(locLabel, it.LocationLabel, it.StorageCoordinate),
		})
		idents[i] = identOf(it.PartID, it.PartMPN, it.PartManufacturer)
	}
	ap.saveResults(teamID, kindPart, idents)
	writeTable(ap.out, []string{"#", "MPN", "MFR", "DESC", "STOCK", "CELL"}, rows)
	if int64(len(resp.Items)) < resp.Total {
		fmt.Fprintf(ap.out, "%d of %d entries\n", len(resp.Items), resp.Total)
	} else {
		fmt.Fprintf(ap.out, "%d entr%s\n", len(resp.Items), map[bool]string{true: "y", false: "ies"}[len(resp.Items) == 1])
	}
	return nil
}

// runStorageTree prints the location hierarchy, which is the map you need
// before you can filter a search by location.
func runStorageTree(ctx context.Context, teamID string) error {
	locs, err := ap.storageLocations(ctx, teamID)
	if err != nil {
		return err
	}
	if ap.format == FormatJSON {
		return writeJSON(ap.out, map[string]any{"items": locs})
	}
	if len(locs) == 0 {
		fmt.Fprintln(ap.out, "No storage locations defined.")
		return nil
	}
	byParent := map[string][]StorageLocation{}
	for _, l := range locs {
		key := ""
		if l.ParentID != nil {
			key = *l.ParentID
		}
		byParent[key] = append(byParent[key], l)
	}
	for k := range byParent {
		sort.Slice(byParent[k], func(i, j int) bool { return byParent[k][i].Label < byParent[k][j].Label })
	}
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, l := range byParent[parent] {
			fmt.Fprintf(ap.out, "%s%s  %s\n", strings.Repeat("  ", depth), l.Label, shortID(l.ID))
			walk(l.ID, depth+1)
		}
	}
	walk("", 0)
	return nil
}

// storageLocations lists the team's locations.
func (a *app) storageLocations(ctx context.Context, teamID string) ([]StorageLocation, error) {
	client, err := a.requireAuthedClient()
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items []StorageLocation `json:"items"`
	}
	if err := client.Get(ctx, "/teams/"+teamID+"/storage-locations", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// resolveLocation turns a location label or id into an id. Labels are unique
// only among siblings, so an ambiguous label lists the candidates with their
// parents rather than guessing.
func (a *app) resolveLocation(ctx context.Context, teamID, ref string) (id, label string, err error) {
	ref = strings.TrimSpace(ref)
	locs, err := a.storageLocations(ctx, teamID)
	if err != nil {
		return "", "", err
	}
	if uuidRe.MatchString(ref) {
		id = strings.ToLower(ref)
		for _, l := range locs {
			if strings.EqualFold(l.ID, id) {
				return id, l.Label, nil
			}
		}
		return id, "", nil
	}
	byID := make(map[string]StorageLocation, len(locs))
	for _, l := range locs {
		byID[l.ID] = l
	}

	lower := strings.ToLower(ref)
	var exact, partial []StorageLocation
	for _, l := range locs {
		name := strings.ToLower(l.Label)
		switch {
		case name == lower:
			exact = append(exact, l)
		case strings.Contains(name, lower):
			partial = append(partial, l)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	switch len(candidates) {
	case 0:
		return "", "", exitErrf(ExitNotFound, "no storage location matching %q — list them with `pt where`", ref)
	case 1:
		return candidates[0].ID, candidates[0].Label, nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches %d locations:", ref, len(candidates))
		for _, l := range candidates {
			fmt.Fprintf(&b, "\n  %-8s %s", shortID(l.ID), locationPath(byID, l))
		}
		return "", "", exitErrf(ExitAmbiguous, "%s", b.String())
	}
}

// locationPath renders a location's ancestry for disambiguation messages.
func locationPath(byID map[string]StorageLocation, l StorageLocation) string {
	var parts []string
	seen := map[string]bool{}
	cur := l
	for {
		if seen[cur.ID] {
			break
		}
		seen[cur.ID] = true
		parts = append([]string{cur.Label}, parts...)
		if cur.ParentID == nil {
			break
		}
		next, ok := byID[*cur.ParentID]
		if !ok {
			break
		}
		cur = next
	}
	return strings.Join(parts, " / ")
}

func init() {
	whereCmd.Flags().StringVarP(&whereFlags.location, "location", "l", "",
		"list the contents of this storage location instead")
	whereCmd.Flags().IntVar(&whereFlags.limit, "limit", 100, "maximum entries to list")
}

// cellWithin renders a row's position inside a location listing. When the row
// sits in the location that was asked for, only the grid cell is interesting;
// a row from a descendant location is named in full.
func cellWithin(asked string, label *string, coord []int32) string {
	if label != nil && asked != "" && *label == asked {
		if len(coord) == 0 {
			return dash
		}
		cells := make([]string, len(coord))
		for i, c := range coord {
			cells[i] = strconv.FormatInt(int64(c), 10)
		}
		return strings.Join(cells, ",")
	}
	return formatLocation(label, coord)
}
