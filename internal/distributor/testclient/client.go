// Package testclient implements a fake distributor client for development and testing.
// It only "stocks" LM317 voltage regulators and always reports stock_available = 0.
package testclient

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

const (
	testSKU        = "LM317-TEST-TUBE"
	simulateDelays = true
)

// Client is the test distributor implementation.
type Client struct{}

// New returns a new test Client.
func New() *Client {
	return &Client{}
}

// EnrichPart returns enrichment data for the LM317-TEST-TUBE SKU (and any SKU that
// starts with "LM317" for convenience in tests). Returns nil for all other SKUs.
func (c *Client) EnrichPart(_ context.Context, sku string, _ json.RawMessage) (*distributor.EnrichmentResult, error) {
	if simulateDelays {
		time.Sleep(100 * time.Millisecond)
	}

	if !strings.HasPrefix(strings.ToUpper(sku), "LM317") {
		return nil, nil
	}

	stockZero := int32(0)
	return &distributor.EnrichmentResult{
		MPN:             "LM317TEST",
		Manufacturer:    "Texas Instruments",
		Description:     "1.5A Adjustable Voltage Regulator",
		Category:        "Linear Regulators",
		Package:         "TO-220AB",
		DatasheetURL:    "",
		LifecycleStatus: distributor.LifecycleStatusActive,
		Parameters: []distributor.Parameter{
			{Name: "output_current_max", Value: "1.5", Unit: "A"},
			{Name: "output_voltage_min", Value: "1.25", Unit: "V"},
			{Name: "output_voltage_max", Value: "37", Unit: "V"},
		},
		Listings: []distributor.Listing{
			{
				SKU:            testSKU,
				PackagingType:  "tube",
				UnitPrice:      "0.5000",
				StockAvailable: &stockZero,
				Moq:            1,
				FixedOrderCost: "0.0000",
				PriceBreaks: []distributor.PriceBreak{
					{BreakQty: 1, UnitPrice: "0.5000"},
				},
			},
		},
	}, nil
}

// RefreshListing returns updated stock data for the LM317 test SKU.
// Returns nil for any other SKU.
func (c *Client) RefreshListing(_ context.Context, sku string, _ json.RawMessage) (*distributor.Listing, error) {
	if simulateDelays {
		time.Sleep(100 * time.Millisecond)
	}

	if sku != testSKU {
		return nil, nil
	}

	stockZero := int32(0)
	return &distributor.Listing{
		SKU:            testSKU,
		PackagingType:  "tube",
		UnitPrice:      "0.5000",
		StockAvailable: &stockZero,
		Moq:            1,
		FixedOrderCost: "0.0000",
		PriceBreaks: []distributor.PriceBreak{
			{BreakQty: 1, UnitPrice: "0.5000"},
		},
	}, nil
}

// SearchParts returns mock search results for testing. It is intentionally
// simple and deterministic: queries starting with "LM317" yield a single
// LM317-style result; queries containing "REGULATOR" (but not LM317)
// return a small fixed set; all others return no results.
func (c *Client) SearchParts(_ context.Context, q distributor.SearchQuery, _ json.RawMessage) (*distributor.SearchResult, error) {
	keywords := strings.ToUpper(strings.TrimSpace(q.Keywords))
	if keywords == "" {
		return nil, nil
	}

	if simulateDelays {
		time.Sleep(500 * time.Millisecond)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	// Single LM317 result for LM317-prefixed queries.
	if strings.HasPrefix(keywords, "LM317") {
		stockZero := int32(0)
		product := distributor.SearchProduct{
			MPN:            "LM317TEST",
			Manufacturer:   "Texas Instruments",
			Description:    "1.5A Adjustable Voltage Regulator",
			Category:       "Linear Regulators",
			PrimarySKU:     testSKU,
			Package:        "TO-220AB",
			UnitPrice:      "0.5000",
			StockAvailable: &stockZero,
		}
		return &distributor.SearchResult{
			Products:          []distributor.SearchProduct{product},
			TotalCount:        1,
			ExactMatchIndices: []int{0},
		}, nil
	}

	// For generic regulator queries, return up to 3 canned results.
	if strings.Contains(keywords, "REGULATOR") {
		all := []distributor.SearchProduct{
			{
				MPN:          "REG-TEST-1",
				Manufacturer: "Partrack Test",
				Description:  "Mock linear regulator 1",
				Category:     "Linear Regulators",
				PrimarySKU:   "REG-TEST-1-SKU",
				Package:      "SOT-223",
				UnitPrice:    "0.1000",
			},
			{
				MPN:          "REG-TEST-2",
				Manufacturer: "Partrack Test",
				Description:  "Mock switching regulator",
				Category:     "Switching Regulators",
				PrimarySKU:   "REG-TEST-2-SKU",
				Package:      "SOIC-8",
				UnitPrice:    "0.2000",
			},
			{
				MPN:          "REG-TEST-3",
				Manufacturer: "Partrack Test",
				Description:  "Mock LDO regulator",
				Category:     "LDO Regulators",
				PrimarySKU:   "REG-TEST-3-SKU",
				Package:      "SOT-23-5",
				UnitPrice:    "0.1500",
			},
		}

		// Apply a simple limit/offset slice.
		start := q.Offset
		if start < 0 {
			start = 0
		}
		if int(start) >= len(all) {
			return nil, nil
		}
		end := int(start + limit)
		if end > len(all) {
			end = len(all)
		}

		return &distributor.SearchResult{
			Products:   all[start:end],
			TotalCount: int32(len(all)),
		}, nil
	}

	// Everything else: no results.
	return &distributor.SearchResult{
		Products:   []distributor.SearchProduct{},
		TotalCount: 0,
	}, nil
}
