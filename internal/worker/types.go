package worker

// UnresolvedImportItem captures everything needed to retry enrichment for a
// DigiKey line item that failed during import or link. Serialised into
// orders.integrations["digikey"]["unresolved_items"].
//
// Failure reason codes stored in FailureReason:
//
//	"enrichment_failed"  — EnrichPart API call errored (429, 500, timeout, etc.)
//	"enrichment_nil"     — EnrichPart returned no result (SKU not in DigiKey catalog)
//	"listing_not_found"  — Enrichment succeeded but listing still absent from DB afterwards
type UnresolvedImportItem struct {
	SKU               string  `json:"sku"`
	MPN               string  `json:"mpn,omitempty"`
	UnitPrice         float64 `json:"unit_price,omitempty"`
	QuantityOrdered   int32   `json:"quantity_ordered"`
	QuantityShipped   int32   `json:"quantity_shipped"`
	QuantityBackorder int32   `json:"quantity_backordered"`
	FailureReason     string  `json:"failure_reason,omitempty"`
}
