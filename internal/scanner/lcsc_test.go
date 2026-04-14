package scanner

import (
	"testing"
)

func TestParseLCSC(t *testing.T) {
	cases := []struct {
		raw     string
		sku     string
		mpn     string
		qty     int
		lot     string
		orderNo string
	}{
		{
			raw:     `{pbn:PICK2205300004,on:GB2512180610,pc:C2977777,pm:CH340N,qty:10,mc:TLAN62,cc:1,pdi:190633867,hp:0,wc:zh}`,
			sku:     "C2977777",
			mpn:     "CH340N",
			qty:     10,
			lot:     "PICK2205300004",
			orderNo: "GB2512180610",
		},
		{
			// Empty mc field, hp:null
			raw:     `{pbn:PICK2512180128,on:GB2512180781,pc:C157624,pm:XC7Z020-2CLG484I,qty:3,mc:,cc:1,pdi:190638042,hp:null,wc:ZH}`,
			sku:     "C157624",
			mpn:     "XC7Z020-2CLG484I",
			qty:     3,
			lot:     "PICK2512180128",
			orderNo: "GB2512180781",
		},
		{
			// MPN contains a colon — must be preserved verbatim.
			raw:     `{pbn:PICK2205300004,on:GB2512180781,pc:C253882,pm:MT41K256M16TW-107:P,qty:3,mc:TLAN62,cc:1,pdi:190638043,hp:0,wc:zh}`,
			sku:     "C253882",
			mpn:     "MT41K256M16TW-107:P",
			qty:     3,
			lot:     "PICK2205300004",
			orderNo: "GB2512180781",
		},
	}

	for _, tc := range cases {
		t.Run(tc.sku, func(t *testing.T) {
			b, err := ParseAny(tc.raw)
			if err != nil {
				t.Fatalf("ParseAny returned error: %v", err)
			}
			if b.SKU != tc.sku {
				t.Errorf("SKU: got %q, want %q", b.SKU, tc.sku)
			}
			if b.MPN != tc.mpn {
				t.Errorf("MPN: got %q, want %q", b.MPN, tc.mpn)
			}
			if b.Quantity != tc.qty {
				t.Errorf("Quantity: got %d, want %d", b.Quantity, tc.qty)
			}
			if b.Lot != tc.lot {
				t.Errorf("Lot: got %q, want %q", b.Lot, tc.lot)
			}
			if b.SalesOrderNo != tc.orderNo {
				t.Errorf("SalesOrderNo: got %q, want %q", b.SalesOrderNo, tc.orderNo)
			}
			if b.Distributor != "LCSC" {
				t.Errorf("Distributor: got %q, want %q", b.Distributor, "LCSC")
			}
		})
	}
}

func TestParseLCSC_NotLCSC(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"missing braces", `pc:C2977777,pm:CH340N,qty:10`},
		{"only opening brace", `{pc:C2977777`},
		{"empty braces", `{}`},
		{"braces but no pc or pm", `{pbn:PICK123,qty:5}`},
		{"ipc2d format", "[)>\x1e06\x1dPABCDEFG\x1d1PMPN123\x1dQ10\x04"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := ParseLCSC(tc.raw)
			if b != nil {
				t.Errorf("ParseLCSC(%q) = %+v, want nil", tc.raw, b)
			}
		})
	}
}
