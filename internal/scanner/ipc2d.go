// Package scanner parses distributor bag barcodes (ISO 15434 / IPC-2D format).
// Currently supports DigiKey and Mouser bag labels.
package scanner

import (
	"errors"
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
	SKU      string // distributor SKU / part number
	MPN      string // manufacturer part number
	Quantity int    // pack quantity (0 if not present)
	Lot      string // date code / lot number
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

	if !strings.HasPrefix(raw, ipc2dHeader) {
		return nil, ErrUnknownFormat
	}

	// Strip the fixed header: "[)>\x1e"
	body := raw[len(ipc2dHeader):]

	// The next characters should be the format identifier ("06" or "12").
	// Split the entire body on the group separator to get all fields.
	// Format identifier and records coexist in the same GS-delimited stream.
	fields := strings.Split(body, gs)
	if len(fields) == 0 {
		return nil, ErrParseFailure
	}

	// First field is the format ID (e.g. "06", "12", "06\x1e06"). We strip
	// any RS characters within it and just verify we have a known format.
	fmtID := strings.ReplaceAll(fields[0], rs, "")
	fmtID = strings.TrimSpace(fmtID)
	if fmtID != "06" && fmtID != "12" {
		return nil, ErrUnknownFormat
	}

	result := &ParsedBarcode{}

	for _, field := range fields[1:] {
		// Strip any RS characters that may appear within a field.
		field = strings.ReplaceAll(field, rs, "")
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		switch {
		case strings.HasPrefix(field, "1P"):
			// MPN (manufacturer part number)
			result.MPN = strings.TrimPrefix(field, "1P")

		case strings.HasPrefix(field, "P"):
			// Distributor SKU
			result.SKU = strings.TrimPrefix(field, "P")

		case strings.HasPrefix(field, "Q"):
			// Quantity
			qStr := strings.TrimPrefix(field, "Q")
			if q, err := strconv.Atoi(qStr); err == nil {
				result.Quantity = q
			}

		case strings.HasPrefix(field, "1T"):
			// Lot / date code
			result.Lot = strings.TrimPrefix(field, "1T")
		}
	}

	if result.SKU == "" && result.MPN == "" {
		return nil, ErrParseFailure
	}

	return result, nil
}
