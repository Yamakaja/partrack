// Package testclient implements a fake distributor client for development and testing.
// It only "stocks" LM317 voltage regulators and always reports stock_available = 0.
package testclient

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Yamakaja/partrack/internal/distributor"
)

const (
	testSKU = "LM317-TEST-TUBE"
)

// Client is the test distributor implementation.
type Client struct{}

// New returns a new test Client.
func New() *Client {
	return &Client{}
}

// EnrichPart returns enrichment data for any LM317 variant (case-insensitive prefix match).
// Returns nil for all other MPNs.
func (c *Client) EnrichPart(_ context.Context, mpn string, _ json.RawMessage) (*distributor.EnrichmentResult, error) {
	if !strings.HasPrefix(strings.ToUpper(mpn), "LM317") {
		return nil, nil
	}

	stockZero := int32(0)
	return &distributor.EnrichmentResult{
		Manufacturer: "Texas Instruments",
		Description:  "1.5A Adjustable Voltage Regulator",
		Category:     "Linear Regulators",
		Package:      "TO-220AB",
		DatasheetURL: "",
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
			},
		},
	}, nil
}

// RefreshListing returns updated stock data for the LM317 test SKU.
// Returns nil for any other SKU.
func (c *Client) RefreshListing(_ context.Context, sku string, _ json.RawMessage) (*distributor.Listing, error) {
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
	}, nil
}
