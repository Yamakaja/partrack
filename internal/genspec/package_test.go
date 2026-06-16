package genspec

import "testing"

func TestCanonicalPackage(t *testing.T) {
	tests := []struct {
		footprint string
		wantPkg   string
		wantMount string
		wantClass string
		wantOK    bool
	}{
		// KiCad chip sizes — imperial precedes metric.
		{"R_0805_2012Metric_Pad1.20x1.40mm_HandSolder", "0805", "smd", "R", true},
		{"C_0805_2012Metric", "0805", "smd", "C", true},
		{"L_0805_2012Metric", "0805", "smd", "L", true},
		{"LED_0805_2012Metric_Pad1.15x1.40mm_HandSolder", "0805", "smd", "LED", true},
		{"R_1210_3225Metric_Pad1.30x2.65mm_HandSolder", "1210", "smd", "R", true},
		{"R_2512_6332Metric_Pad1.40x3.35mm_HandSolder", "2512", "smd", "R", true},
		{"R_1218_3246Metric", "1218", "smd", "R", true},
		{"R_2816_7142Metric", "2816", "smd", "R", true},

		// The metric trap: the "0603"/"0402" inside is the METRIC twin.
		{"R_0201_0603Metric", "0201", "smd", "R", true},
		{"R_0201_0603Metric_Pad0.64x0.40mm_HandSolder", "0201", "smd", "R", true},
		// Variable-length imperial codes.
		{"R_01005_0402Metric", "01005", "smd", "R", true},
		{"C_008004_0201Metric", "008004", "smd", "C", true},

		// Imperial embedded in a vendor token; metric corroborates.
		{"R_Shunt_Vishay_WSK2512_6332Metric_T1.19mm", "2512", "smd", "R", true},

		// Bare codes (Altium-style) default to imperial.
		{"0603", "0603", "smd", "", true},
		{"0402", "0402", "smd", "", true},
		// Standalone metric → mapped to imperial.
		{"2012Metric", "0805", "smd", "", true},
		// IPC-7351 names carry a metric code.
		{"RESC1608X55N", "0603", "smd", "", true},
		{"CAPC2012X127N", "0805", "smd", "", true},

		// Arrays: no confident size.
		{"R_Array_Convex_2x0606", "", "smd", "R", false},
		{"R_Array_Convex_2x1206", "", "smd", "R", false},
		{"R_Cat16-4", "", "smd", "R", false},

		// Non-chip families.
		{"SOT-23", "SOT-23", "smd", "", true},
		{"SOIC-8", "SOIC-8", "smd", "", true},
		{"Package_TO_SOT_SMD:SOT-23-5", "SOT-23-5", "smd", "", true},
		{"R_Axial_DIN0207_L6.3mm_D2.5mm_P10.16mm", "Axial DIN0207", "tht", "R", true},

		// Empty.
		{"", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.footprint, func(t *testing.T) {
			pkg, mount, class, ok := CanonicalPackage(tt.footprint)
			if pkg != tt.wantPkg || mount != tt.wantMount || class != tt.wantClass || ok != tt.wantOK {
				t.Errorf("CanonicalPackage(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
					tt.footprint, pkg, mount, class, ok, tt.wantPkg, tt.wantMount, tt.wantClass, tt.wantOK)
			}
		})
	}
}
