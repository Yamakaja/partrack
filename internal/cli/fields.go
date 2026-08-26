package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Column selection for the list views. `--fields` picks and orders columns for
// both plain and JSON output, so `pt search --fields mpn,stock --format json`
// emits exactly those keys and nothing else — cheaper to consume than piping the
// full row through jq.
type column struct {
	Key     string // canonical name, also the JSON key
	Header  string // plain-text header
	Aliases []string
	Value   func(PartListItem) string

	// JSON overrides the encoded value when the display string is the wrong
	// type — a quantity must arrive as a number and an absent value as null,
	// not as the em-dash placeholder. Nil falls back to Value, mapping the
	// placeholder to null.
	JSON func(PartListItem) any
}

var partColumns = []column{
	{Key: "id", Header: "ID", Value: func(p PartListItem) string { return shortID(p.ID) }},
	{Key: "uuid", Header: "UUID", Value: func(p PartListItem) string { return p.ID }},
	{Key: "mpn", Header: "MPN", Value: func(p PartListItem) string { return str(p.Mpn) }},
	{Key: "mfr", Header: "MFR", Aliases: []string{"manufacturer"},
		Value: func(p PartListItem) string { return str(p.Manufacturer) }},
	{Key: "desc", Header: "DESC", Aliases: []string{"description"}, Value: func(p PartListItem) string {
		// The short description is the curated one; fall back to the full text.
		if p.ShortDescription != nil && *p.ShortDescription != "" {
			return *p.ShortDescription
		}
		return str(p.Description)
	}},
	{Key: "category", Header: "CATEGORY", Value: func(p PartListItem) string {
		return strOr(p.CategoryName, strOr(p.Category, dash))
	}},
	{Key: "pkg", Header: "PKG", Aliases: []string{"package"},
		Value: func(p PartListItem) string { return str(p.Package) }},
	{Key: "status", Header: "STATUS", Aliases: []string{"lifecycle", "lifecycle_status"},
		Value: func(p PartListItem) string { return p.LifecycleStatus }},
	{Key: "scope", Header: "SCOPE", Value: func(p PartListItem) string { return p.Scope }},
	{Key: "stock", Header: "STOCK", Aliases: []string{"qty", "on_hand"},
		Value: func(p PartListItem) string {
			// An untracked part has no inventory entry at all, which is a
			// different statement from "we have zero", so it renders as a dash.
			if p.InventoryEntryID == nil {
				return dash
			}
			return num(p.QuantityOnHand)
		},
		JSON: func(p PartListItem) any {
			if p.InventoryEntryID == nil {
				return nil
			}
			return intOrNil(p.QuantityOnHand)
		}},
	{Key: "min", Header: "MIN", Aliases: []string{"min_stock"},
		Value: func(p PartListItem) string { return num(p.MinimumStockQuantity) },
		JSON:  func(p PartListItem) any { return intOrNil(p.MinimumStockQuantity) }},
	{Key: "reorder", Header: "REORDER",
		Value: func(p PartListItem) string { return num(p.ReorderQuantity) },
		JSON:  func(p PartListItem) any { return intOrNil(p.ReorderQuantity) }},
	{Key: "loc", Header: "LOC", Aliases: []string{"location", "storage"}, Value: func(p PartListItem) string {
		return formatLocation(p.StorageLocationLabel, p.StorageCoordinate)
	}},
	{Key: "tracked", Header: "TRACKED",
		Value: func(p PartListItem) string {
			if p.InventoryEntryID == nil {
				return "no"
			}
			return "yes"
		},
		JSON: func(p PartListItem) any { return p.InventoryEntryID != nil }},
}

// defaultPartFields is what `pt search` shows with no --fields. It answers the
// bench question — what is it, have we got any, and where is it.
var defaultPartFields = []string{"mpn", "mfr", "desc", "pkg", "stock", "loc", "status"}

// formatLocation renders a storage label plus its grid coordinate, e.g.
// "Drawer A/3,4". The coordinate is what tells you which compartment, so it is
// worth the extra characters.
func formatLocation(label *string, coord []int32) string {
	if label == nil || *label == "" {
		return dash
	}
	if len(coord) == 0 {
		return *label
	}
	cells := make([]string, len(coord))
	for i, c := range coord {
		cells[i] = strconv.FormatInt(int64(c), 10)
	}
	return *label + "/" + strings.Join(cells, ",")
}

// resolveFields maps a user field list onto columns, preserving the given order.
func resolveFields(spec []string) ([]column, error) {
	var out []column
	for _, raw := range spec {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		col, ok := lookupColumn(name)
		if !ok {
			return nil, exitErrf(ExitUsage, "unknown field %q — available: %s", raw, availableFields())
		}
		out = append(out, col)
	}
	if len(out) == 0 {
		return nil, exitErrf(ExitUsage, "no fields selected")
	}
	return out, nil
}

func lookupColumn(name string) (column, bool) {
	for _, c := range partColumns {
		if c.Key == name {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == name {
				return c, true
			}
		}
	}
	return column{}, false
}

func availableFields() string {
	keys := make([]string, len(partColumns))
	for i, c := range partColumns {
		keys[i] = c.Key
	}
	return strings.Join(keys, ", ")
}

// splitList splits a comma-separated flag value, trimming and dropping empties.
// Repeating the flag and comma-separating it both work.
func splitList(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// paramColumns derives the extra spec columns for --params. A bare --params
// takes every parameter present in the result set; a named subset takes only
// those, matched case-insensitively against slug and display name.
func paramColumns(items []PartListItem, wanted []string) []string {
	present := map[string]bool{}
	for _, it := range items {
		for slug := range it.Parameters {
			present[slug] = true
		}
	}
	if len(wanted) == 0 {
		keys := make([]string, 0, len(present))
		for k := range present {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}
	var out []string
	for _, w := range wanted {
		lower := strings.ToLower(strings.TrimSpace(w))
		for slug := range present {
			if strings.ToLower(slug) == lower {
				out = append(out, slug)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// headerFor renders a parameter slug as a column header.
func headerFor(slug string) string {
	return strings.ToUpper(strings.ReplaceAll(slug, "_", " "))
}

// rowFor builds one plain-text row from the selected columns and param columns.
func rowFor(p PartListItem, cols []column, params []string, descWidth int) []string {
	cells := make([]string, 0, len(cols)+len(params))
	for _, c := range cols {
		v := c.Value(p)
		if c.Key == "desc" && descWidth > 0 {
			v = truncate(v, descWidth)
		}
		cells = append(cells, v)
	}
	for _, slug := range params {
		v, ok := p.Parameters[slug]
		if !ok || v == "" {
			v = dash
		}
		cells = append(cells, v)
	}
	return cells
}

// intOrNil encodes a nullable quantity as a JSON number or null, never as a
// placeholder string.
func intOrNil(v *int32) any {
	if v == nil {
		return nil
	}
	return *v
}

// jsonRow builds the JSON object for one row under --fields, keeping only the
// selected keys. Absent values encode as null rather than the display dash, so
// `jq '.items[] | select(.stock == null)'` means what it reads like.
func jsonRow(p PartListItem, cols []column, params []string) map[string]any {
	obj := make(map[string]any, len(cols)+len(params))
	for _, c := range cols {
		if c.JSON != nil {
			obj[c.Key] = c.JSON(p)
			continue
		}
		if v := c.Value(p); v != dash {
			obj[c.Key] = v
		} else {
			obj[c.Key] = nil
		}
	}
	for _, slug := range params {
		if v, ok := p.Parameters[slug]; ok {
			obj[slug] = v
		} else {
			obj[slug] = nil
		}
	}
	return obj
}

// numberedHeader prefixes a header row with the @N index column.
func numberedHeader(cols []column, params []string) []string {
	header := make([]string, 0, len(cols)+len(params)+1)
	header = append(header, "#")
	for _, c := range cols {
		header = append(header, c.Header)
	}
	for _, slug := range params {
		header = append(header, headerFor(slug))
	}
	return header
}

// numberedRow prefixes a data row with its 1-based @N index.
func numberedRow(n int, cells []string) []string {
	return append([]string{fmt.Sprintf("%d", n)}, cells...)
}
