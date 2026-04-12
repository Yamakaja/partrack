// Package scanner parses distributor bag barcodes (ISO 15434 / IPC-2D format).
// Currently supports DigiKey and Mouser bag labels.
package scanner

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrUnknownFormat is returned when the barcode header is not a recognised
// ISO 15434 format (i.e. not a DigiKey or Mouser bag label).
var ErrUnknownFormat = errors.New("unrecognized barcode format")

// ErrParseFailure is returned when the header is recognised but the field
// data cannot be parsed.
var ErrParseFailure = errors.New("barcode parse failure")

// ParsedBarcode holds the fields extracted from an IPC-2D bag label.
type ParsedBarcode struct {
	SKU             string // distributor order code (P or 30P)
	MPN             string // manufacturer part number (1P)
	Quantity        int    // pack quantity (Q; 0 if absent)
	Distributor     string // identified distributor ("DigiKey", "Mouser", or "")
	Manufacturer    string // manufacturer name (1V; Mouser)
	Lot             string // lot number (1T)
	DateCode        string // date code (9D; DigiKey)
	CustomerOrderNo string // customer's PO number (K)
	SalesOrderNo    string // distributor's SO / invoice reference (1K for DigiKey, 11K for Mouser)
	CountryOfOrigin string // country of origin (4L)
}

const (
	// ipc2dHeader is the mandatory start sequence for ISO 15434 records.
	ipc2dHeader = "[)>\x1e"
	rs          = "\x1e" // record separator
	gs          = "\x1d" // group (field) separator
	eot         = "\x04" // end of transmission (optional trailer)
)

// Parse attempts to decode raw as an IPC-2D barcode (DigiKey or Mouser).
// It returns ErrUnknownFormat if the header is absent, or ErrParseFailure
// if parsing fails after recognising the header.
// Plain MPN strings are intentionally NOT treated as a fallback — callers
// should handle ErrUnknownFormat separately.
func Parse(raw string) (*ParsedBarcode, error) {
	// Normalise line endings some scanners emit.
	raw = strings.TrimRight(raw, "\r\n"+eot)

	// Some decoders substitute ISO 15434 control bytes with their Unicode
	// Control Pictures equivalents (U+241E → RS, U+241D → GS, U+2404 → EOT).
	raw = strings.ReplaceAll(raw, "\u241e", rs)
	raw = strings.ReplaceAll(raw, "\u241d", gs)
	raw = strings.ReplaceAll(raw, "\u2404", eot)

	if !strings.HasPrefix(raw, ipc2dHeader) {
		return nil, ErrUnknownFormat
	}

	// Strip the fixed header: "[)>\x1e"
	body := raw[len(ipc2dHeader):]

	// Split on the group separator to get all fields.
	// The first field is the format identifier ("06" or "12").
	fields := strings.Split(body, gs)
	if len(fields) == 0 {
		return nil, ErrParseFailure
	}

	// First field is the format ID. Strip any embedded RS characters.
	fmtID := strings.ReplaceAll(fields[0], rs, "")
	fmtID = strings.TrimSpace(fmtID)
	if fmtID != "06" && fmtID != "12" {
		return nil, ErrUnknownFormat
	}

	result := &ParsedBarcode{}

	// Intermediate vars for fields whose semantic meaning depends on the
	// distributor, resolved after all fields are extracted.
	var (
		has30P    bool   // 30P is DigiKey-specific; definitive DigiKey signal
		digikeySO string // 1K = DigiKey sales order number
		mouserInv string // 11K = Mouser invoice number (same field ID is DigiKey packing list)
	)

	for _, field := range fields[1:] {
		fmt.Println(field)
		// Strip any RS characters that may appear within a field.
		field = strings.ReplaceAll(field, rs, "")
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		// Cases are ordered longest-prefix-first so that e.g. "30P" is tested
		// before "P", and "11K" before "1K" before "K".
		switch {
		case strings.HasPrefix(field, "30P"):
			result.SKU = strings.TrimPrefix(field, "30P")
			has30P = true

		case strings.HasPrefix(field, "1P"):
			result.MPN = strings.TrimPrefix(field, "1P")

		case strings.HasPrefix(field, "1T"):
			result.Lot = strings.TrimPrefix(field, "1T")

		case strings.HasPrefix(field, "1V"):
			// Manufacturer name — present on Mouser labels.
			result.Manufacturer = strings.TrimPrefix(field, "1V")

		case strings.HasPrefix(field, "14K"):
			// Mouser order line position — not mapped to a semantic field.

		case strings.HasPrefix(field, "11K"):
			mouserInv = strings.TrimPrefix(field, "11K")

		case strings.HasPrefix(field, "10K"):
			// DigiKey inventory number — not mapped.

		case strings.HasPrefix(field, "1K"):
			digikeySO = strings.TrimPrefix(field, "1K")

		case strings.HasPrefix(field, "9D"):
			result.DateCode = strings.TrimPrefix(field, "9D")

		case strings.HasPrefix(field, "4L"):
			result.CountryOfOrigin = strings.TrimPrefix(field, "4L")

		case strings.HasPrefix(field, "P"):
			// Generic distributor SKU. 30P takes precedence if both are present.
			if result.SKU == "" {
				result.SKU = strings.TrimPrefix(field, "P")
			}

		case strings.HasPrefix(field, "Q"):
			if q, err := strconv.Atoi(strings.TrimPrefix(field, "Q")); err == nil {
				result.Quantity = q
			}

		case strings.HasPrefix(field, "K"):
			// Customer's own purchase order number (same meaning across distributors).
			result.CustomerOrderNo = strings.TrimPrefix(field, "K")

			// Deliberately ignored: 11Z, 12Z, 13Z, 20Z — DigiKey internal / padding.
		}
	}

	if result.SKU == "" && result.MPN == "" {
		return nil, ErrParseFailure
	}

	// --- Distributor heuristics ---
	// Applied after full field extraction. Note that both DigiKey and Mouser
	// use format ID "06", so format ID alone cannot identify the distributor.
	switch {
	case has30P:
		// 30P is DigiKey-specific — definitive.
		result.Distributor = "DigiKey"
	case result.Manufacturer != "" || mouserInv != "":
		// 1V (manufacturer name) and 11K (invoice) are Mouser-specific.
		result.Distributor = "Mouser"
	case fmtID == "12":
		result.Distributor = "Mouser"
	case digikeySO != "":
		// 1K (DigiKey sales order) is not present on Mouser labels.
		result.Distributor = "DigiKey"
	}

	// Map the distributor-specific order reference to SalesOrderNo.
	switch result.Distributor {
	case "DigiKey":
		result.SalesOrderNo = digikeySO
	case "Mouser":
		result.SalesOrderNo = mouserInv
	}

	return result, nil
}
