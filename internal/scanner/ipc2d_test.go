package scanner

import (
	"errors"
	"testing"
)

func TestParse_DigiKey(t *testing.T) {
	// Typical DigiKey format-06 bag label
	raw := "[)>\x1e06\x1dP296-1234-1-ND\x1d1PBC847\x1dQ100\x1d1T2401\x1d4LUS\x1d\x04"
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SKU != "296-1234-1-ND" {
		t.Errorf("SKU: got %q, want %q", got.SKU, "296-1234-1-ND")
	}
	if got.MPN != "BC847" {
		t.Errorf("MPN: got %q, want %q", got.MPN, "BC847")
	}
	if got.Quantity != 100 {
		t.Errorf("Quantity: got %d, want %d", got.Quantity, 100)
	}
	if got.Lot != "2401" {
		t.Errorf("Lot: got %q, want %q", got.Lot, "2401")
	}
}

func TestParse_Mouser_Format12(t *testing.T) {
	// Mouser format-12 variant
	raw := "[)>\x1e12\x1dPBC847\x1d1PBC847BPN\x1dQ50\x1d\x04"
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SKU != "BC847" {
		t.Errorf("SKU: got %q, want %q", got.SKU, "BC847")
	}
	if got.Quantity != 50 {
		t.Errorf("Quantity: got %d, want %d", got.Quantity, 50)
	}
}

func TestParse_UnknownFormat(t *testing.T) {
	_, err := Parse("BC847")
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("expected ErrUnknownFormat, got %v", err)
	}
}

func TestParse_UnknownFormatID(t *testing.T) {
	raw := "[)>\x1e99\x1dPSKU\x1d"
	_, err := Parse(raw)
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("expected ErrUnknownFormat for unknown format ID, got %v", err)
	}
}

func TestParse_MissingSkuAndMPN(t *testing.T) {
	raw := "[)>\x1e06\x1dQ100\x1d"
	_, err := Parse(raw)
	if !errors.Is(err, ErrParseFailure) {
		t.Errorf("expected ErrParseFailure, got %v", err)
	}
}
