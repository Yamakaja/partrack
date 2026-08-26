package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Format selects an output encoding. The data content is the same in both — only
// the encoding differs — so piping never loses information.
type Format string

const (
	FormatPlain Format = "plain"
	FormatJSON  Format = "json"
)

// ParseFormat validates and normalizes a format string.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "plain":
		return FormatPlain, nil
	case "json":
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("unknown format %q (want plain or json)", s)
	}
}

// writeJSON writes v as indented JSON. HTML escaping is off so datasheet and
// product URLs stay readable.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// writeKV writes aligned key/value lines.
func writeKV(w io.Writer, pairs [][2]string) {
	width := 0
	for _, p := range pairs {
		if len(p[0]) > width {
			width = len(p[0])
		}
	}
	for _, p := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", width, p[0], p[1])
	}
}

// writeTable writes a column-aligned table. Header cells are emitted as given;
// callers upper-case them for the search-style listings.
func writeTable(w io.Writer, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(header) > 0 {
		writeRow(tw, header)
	}
	for _, r := range rows {
		writeRow(tw, r)
	}
	tw.Flush() //nolint:errcheck
}

func writeRow(w io.Writer, cells []string) {
	for i, c := range cells {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		// A tab inside a cell would corrupt the column layout.
		fmt.Fprint(w, strings.ReplaceAll(c, "\t", " "))
	}
	fmt.Fprintln(w)
}

// writeSection prints a blank-line-separated heading, used by the multi-part
// dossier output of `pt show`.
func writeSection(w io.Writer, title string) {
	fmt.Fprintf(w, "\n%s\n", title)
}

// dash is the placeholder for an absent value. Using a consistent one means
// `awk`-style column extraction never shifts.
const dash = "—"

// str dereferences a *string, yielding dash when nil or empty.
func str(p *string) string {
	if p == nil || *p == "" {
		return dash
	}
	return *p
}

// strOr dereferences a *string, yielding alt when nil or empty.
func strOr(p *string, alt string) string {
	if p == nil || *p == "" {
		return alt
	}
	return *p
}

// num formats a *int32, yielding dash when nil.
func num(p *int32) string {
	if p == nil {
		return dash
	}
	return strconv.FormatInt(int64(*p), 10)
}

// truncate shortens s to at most n runes, ending with an ellipsis. Descriptions
// are long enough to wreck a terminal table, so list views clamp them.
func truncate(s string, n int) string {
	if n <= 1 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
