package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Yamakaja/partrack/internal/genspec"
)

// maxInterpretPreviewItems bounds a single preview request.
const maxInterpretPreviewItems = 5000

// InterpretPreview handles POST /api/v1/bom/interpret-preview.
//
// It is a stateless parse of mapped BOM rows into structured interpretations,
// used by the import UI to show how each row was read before committing — no
// persistence, no team scope. The same parser runs the persisted pass at import.
func (h *Handler) InterpretPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			Refdes    string `json:"refdes"`
			Footprint string `json:"footprint"`
			Value     string `json:"value"`
			Mpn       string `json:"mpn"`
			Sku       string `json:"sku"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if len(req.Items) > maxInterpretPreviewItems {
		writeError(w, http.StatusBadRequest, "too many items", "BAD_REQUEST")
		return
	}

	out := make([]genspec.Interpretation, len(req.Items))
	for i, it := range req.Items {
		out[i] = genspec.Interpret(genspec.Input{
			Refdes:    it.Refdes,
			Footprint: it.Footprint,
			Value:     it.Value,
			MPN:       it.Mpn,
			SKU:       it.Sku,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
