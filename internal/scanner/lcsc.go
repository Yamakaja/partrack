package scanner

import (
	"strconv"
	"strings"
)

// ParseLCSC detects and parses LCSC/JLCPCB bag label QR codes.
//
// Format: {pbn:PICK2205300004,on:GB2512180610,pc:C2977777,pm:CH340N,qty:10,...}
//
// Keys and values are unquoted. Values may contain colons — each token is split
// on its FIRST colon only (e.g. "pm:MT41K256M16TW-107:P" → key="pm", value="MT41K256M16TW-107:P").
//
// Returns nil when the input is not an LCSC QR code (missing braces, or both
// pc and pm fields are absent).
func ParseLCSC(raw string) *ParsedBarcode {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
		return nil
	}

	fields := parseLCSCKV(raw[1 : len(raw)-1])

	pc := fields["pc"] // LCSC component code = distributor SKU
	pm := fields["pm"] // manufacturer part number

	if pc == "" && pm == "" {
		return nil
	}

	qty := 0
	if q, err := strconv.Atoi(fields["qty"]); err == nil {
		qty = q
	}

	return &ParsedBarcode{
		SKU:          pc,
		MPN:          pm,
		Quantity:     qty,
		Distributor:  "LCSC",
		Lot:          fields["pbn"], // pick batch number — closest to a lot/traceability ID
		SalesOrderNo: fields["on"],  // LCSC order number (informational; not used for order matching)
	}
}

// parseLCSCKV splits a comma-separated "key:value" string into a map.
// Each token is split on the FIRST colon only. Tokens without a colon are
// skipped. Values equal to "null" (case-insensitive) are normalised to "".
func parseLCSCKV(s string) map[string]string {
	m := make(map[string]string)
	for token := range strings.SplitSeq(s, ",") {
		k, v, found := strings.Cut(token, ":")
		if !found {
			continue
		}
		if strings.EqualFold(v, "null") {
			v = ""
		}
		m[k] = v
	}
	return m
}
