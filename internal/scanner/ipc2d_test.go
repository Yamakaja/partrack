package scanner

import (
	"errors"
	"testing"
)

// Real-world DigiKey format-06 bag label (P + 30P + full metadata).
func TestParse_DigiKey_RealWorld(t *testing.T) {
	raw := "[)>\x1e06\x1d" +
		"P445-2213-1-ND\x1d" +
		"1PACM1211-102-2PL-TL01\x1d" +
		"30P445-2213-1-ND\x1d" +
		"K28134596\x1d" + // customer PO
		"1K95634467\x1d" + // DigiKey sales order
		"10K117756332\x1d" + // inventory number (ignored)
		"9D2535\x1d" + // date code
		"1T0058669202\x1d" + // lot number
		"11K1\x1d" + // packing list (ignored — DigiKey context)
		"4LCN\x1d" +
		"Q5\x1d" +
		"11ZPICK\x1d12Z765070\x1d13Z999999\x1d" + // internal/padding (ignored)
		"\x04"

	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SKU != "445-2213-1-ND" {
		t.Errorf("SKU: got %q, want %q", got.SKU, "445-2213-1-ND")
	}
	if got.MPN != "ACM1211-102-2PL-TL01" {
		t.Errorf("MPN: got %q, want %q", got.MPN, "ACM1211-102-2PL-TL01")
	}
	if got.Quantity != 5 {
		t.Errorf("Quantity: got %d, want %d", got.Quantity, 5)
	}
	if got.Distributor != "DigiKey" {
		t.Errorf("Distributor: got %q, want %q", got.Distributor, "DigiKey")
	}
	if got.Lot != "0058669202" {
		t.Errorf("Lot: got %q, want %q", got.Lot, "0058669202")
	}
	if got.DateCode != "2535" {
		t.Errorf("DateCode: got %q, want %q", got.DateCode, "2535")
	}
	if got.CustomerOrderNo != "28134596" {
		t.Errorf("CustomerOrderNo: got %q, want %q", got.CustomerOrderNo, "28134596")
	}
	if got.SalesOrderNo != "95634467" {
		t.Errorf("SalesOrderNo: got %q, want %q", got.SalesOrderNo, "95634467")
	}
	if got.CountryOfOrigin != "CN" {
		t.Errorf("CountryOfOrigin: got %q, want %q", got.CountryOfOrigin, "CN")
	}
}

// Real-world Mouser format-06 label (MPN only, no distributor SKU).
func TestParse_Mouser_RealWorld(t *testing.T) {
	raw := "[)>\x1e06\x1d" +
		"K27279572\x1d" + // customer order number
		"14K018\x1d" + // order line position (ignored)
		"1PAD826ARZ-REEL\x1d" + // MPN
		"Q5\x1d" +
		"11K071953802\x1d" + // Mouser invoice number → SalesOrderNo
		"4LTW\x1d" +
		"1VAnalog Devices Inc.\x1d" +
		"\x04"

	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SKU != "" {
		t.Errorf("SKU: got %q, want empty", got.SKU)
	}
	if got.MPN != "AD826ARZ-REEL" {
		t.Errorf("MPN: got %q, want %q", got.MPN, "AD826ARZ-REEL")
	}
	if got.Quantity != 5 {
		t.Errorf("Quantity: got %d, want %d", got.Quantity, 5)
	}
	if got.Distributor != "Mouser" {
		t.Errorf("Distributor: got %q, want %q", got.Distributor, "Mouser")
	}
	if got.Manufacturer != "Analog Devices Inc." {
		t.Errorf("Manufacturer: got %q, want %q", got.Manufacturer, "Analog Devices Inc.")
	}
	if got.CustomerOrderNo != "27279572" {
		t.Errorf("CustomerOrderNo: got %q, want %q", got.CustomerOrderNo, "27279572")
	}
	if got.SalesOrderNo != "071953802" {
		t.Errorf("SalesOrderNo: got %q, want %q", got.SalesOrderNo, "071953802")
	}
	if got.CountryOfOrigin != "TW" {
		t.Errorf("CountryOfOrigin: got %q, want %q", got.CountryOfOrigin, "TW")
	}
}

// DigiKey label with 30P field only (no P field).
func TestParse_DigiKey_30P(t *testing.T) {
	raw := "[)>\x1e06\x1d30P296-1234-1-ND\x1d1PBC847\x1dQ100\x1d\x04"
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
	if got.Distributor != "DigiKey" {
		t.Errorf("Distributor: got %q, want %q", got.Distributor, "DigiKey")
	}
}

// Format-06 with generic P field only — distributor cannot be determined.
func TestParse_Format06_AmbiguousDistributor(t *testing.T) {
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
	if got.Distributor != "" {
		t.Errorf("Distributor: got %q, want empty (ambiguous without 30P or 1K)", got.Distributor)
	}
}

// Mouser format-12 variant (legacy).
func TestParse_Mouser_Format12(t *testing.T) {
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
	if got.Distributor != "Mouser" {
		t.Errorf("Distributor: got %q, want %q", got.Distributor, "Mouser")
	}
}

// Partrack-generated listing QR — DigiKey (XD + 30P, no quantity).
func TestParse_PartrackerQR_DigiKey(t *testing.T) {
	raw := "[)>\x1e06\x1d" +
		"1PLM358DR\x1d" +
		"1VTexas Instruments\x1d" +
		"XDDigiKey\x1d" +
		"30P296-1395-5-ND\x04"

	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SKU != "296-1395-5-ND" {
		t.Errorf("SKU: got %q, want %q", got.SKU, "296-1395-5-ND")
	}
	if got.MPN != "LM358DR" {
		t.Errorf("MPN: got %q, want %q", got.MPN, "LM358DR")
	}
	if got.Manufacturer != "Texas Instruments" {
		t.Errorf("Manufacturer: got %q, want %q", got.Manufacturer, "Texas Instruments")
	}
	if got.Distributor != "DigiKey" {
		t.Errorf("Distributor: got %q, want %q", got.Distributor, "DigiKey")
	}
	if got.Quantity != 0 {
		t.Errorf("Quantity: got %d, want 0", got.Quantity)
	}
}

// Partrack-generated listing QR — Mouser (XD + P, no quantity).
func TestParse_PartrackerQR_Mouser(t *testing.T) {
	raw := "[)>\x1e06\x1d" +
		"1PLM358DR\x1d" +
		"1VTexas Instruments\x1d" +
		"XDMouser\x1d" +
		"P595-LM358DR\x04"

	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SKU != "595-LM358DR" {
		t.Errorf("SKU: got %q, want %q", got.SKU, "595-LM358DR")
	}
	if got.Distributor != "Mouser" {
		t.Errorf("Distributor: got %q, want %q", got.Distributor, "Mouser")
	}
	if got.Quantity != 0 {
		t.Errorf("Quantity: got %d, want 0", got.Quantity)
	}
}

// Partrack-generated part-level QR — MPN + manufacturer only, no SKU, no distributor.
func TestParse_PartrackerQR_PartOnly(t *testing.T) {
	raw := "[)>\x1e06\x1d" +
		"1PLM358DR\x1d" +
		"1VTexas Instruments\x04"

	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MPN != "LM358DR" {
		t.Errorf("MPN: got %q, want %q", got.MPN, "LM358DR")
	}
	if got.Manufacturer != "Texas Instruments" {
		t.Errorf("Manufacturer: got %q, want %q", got.Manufacturer, "Texas Instruments")
	}
	if got.SKU != "" {
		t.Errorf("SKU: got %q, want empty", got.SKU)
	}
	if got.Distributor != "" {
		t.Errorf("Distributor: got %q, want empty", got.Distributor)
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
