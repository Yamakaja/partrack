package lcsc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// EnrichPart fetches full product metadata and listings for a specific LCSC component
// code (SKU, e.g. "C2977777"). credentials is ignored — LCSC requires no auth.
// Returns nil, nil if the SKU is not found.
func (c *Client) EnrichPart(ctx context.Context, sku string, _ json.RawMessage) (*distributor.EnrichmentResult, error) {
	// Check cache first.
	if v, ok := c.searchCache.Load(sku); ok {
		entry := v.(cacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return mapToEnrichment(&entry.comp), nil
		}
		c.searchCache.Delete(sku)
	}

	data, err := c.doPost(ctx, searchRequest{Keyword: sku, CurrentPage: 1, PageSize: 10})
	if err != nil {
		return nil, err
	}

	comp := findBySKU(data.ComponentPageInfo.List, sku)
	if comp == nil {
		return nil, nil
	}

	c.searchCache.Store(sku, cacheEntry{comp: *comp, expiresAt: time.Now().Add(time.Hour)})
	return mapToEnrichment(comp), nil
}

// RefreshListing fetches current price and stock for a specific LCSC SKU.
// Returns an empty slice when the SKU is not found.
func (c *Client) RefreshListing(ctx context.Context, sku string, _ json.RawMessage) ([]distributor.Listing, error) {
	data, err := c.doPost(ctx, searchRequest{Keyword: sku, CurrentPage: 1, PageSize: 10})
	if err != nil {
		return nil, err
	}

	comp := findBySKU(data.ComponentPageInfo.List, sku)
	if comp == nil {
		return []distributor.Listing{}, nil
	}

	c.searchCache.Store(sku, cacheEntry{comp: *comp, expiresAt: time.Now().Add(time.Hour)})
	return []distributor.Listing{mapListing(comp)}, nil
}

// SearchParts performs a keyword search across the LCSC/JLCPCB catalog.
func (c *Client) SearchParts(ctx context.Context, q distributor.SearchQuery, _ json.RawMessage) (*distributor.SearchResult, error) {
	keywords := strings.TrimSpace(q.Keywords)
	if keywords == "" {
		return nil, nil
	}

	limit := max(q.Limit, 20)
	limit = min(limit, 50)
	offset := max(q.Offset, 0)
	currentPage := int(offset/limit) + 1

	data, err := c.doPost(ctx, searchRequest{
		Keyword:     keywords,
		CurrentPage: currentPage,
		PageSize:    int(limit),
	})
	if err != nil {
		return nil, err
	}

	list := data.ComponentPageInfo.List
	products := make([]distributor.SearchProduct, 0, len(list))
	var exactIndices []int

	for i, comp := range list {
		sp := distributor.SearchProduct{
			MPN:          comp.ComponentModelEn,
			Manufacturer: comp.ComponentBrandEn,
			Description:  descriptionOf(&comp),
			Category:     comp.FirstSortName,
			PrimarySKU:   comp.ComponentCode,
			Package:      comp.ComponentSpecificationEn,
			StockAvailable: func() *int32 { s := comp.StockCount; return &s }(),
		}
		if len(comp.ComponentPrices) > 0 {
			sp.UnitPrice = formatPrice(comp.ComponentPrices[0].ProductPrice)
		}
		products = append(products, sp)

		// Mark as exact match when the LCSC code or MPN matches the query exactly.
		if comp.ComponentCode == keywords || comp.ComponentModelEn == keywords {
			exactIndices = append(exactIndices, i)
		}

		// Warm the EnrichPart cache.
		if comp.ComponentCode != "" {
			c.searchCache.Store(comp.ComponentCode, cacheEntry{
				comp:      comp,
				expiresAt: time.Now().Add(time.Hour),
			})
		}
	}

	return &distributor.SearchResult{
		Products:          products,
		TotalCount:        int32(data.ComponentPageInfo.Total),
		ExactMatchIndices: exactIndices,
	}, nil
}

// ---- mapping helpers ----

// findBySKU returns the first component in list whose ComponentCode matches sku,
// or nil if none is found.
func findBySKU(list []component, sku string) *component {
	for i := range list {
		if list[i].ComponentCode == sku {
			return &list[i]
		}
	}
	return nil
}

// mapToEnrichment converts an LCSC component to an EnrichmentResult.
func mapToEnrichment(c *component) *distributor.EnrichmentResult {
	result := &distributor.EnrichmentResult{
		MPN:              c.ComponentModelEn,
		Manufacturer:     c.ComponentBrandEn,
		ShortDescription: c.ComponentName,
		Description:      descriptionOf(c),
		Package:          c.ComponentSpecificationEn,
		ProductURL:       c.LcscGoodsUrl,
		DatasheetURL:     c.DataManualUrl,
		LifecycleStatus:  distributor.LifecycleStatusUnknown,
	}

	if c.ComponentImageUrl != "" {
		result.ImageURL = c.ComponentImageUrl
	}

	// Category chain — most-specific first.
	if c.FirstSortAccessId != "" && c.FirstSortName != "" {
		result.DistributorCategoryChain = append(result.DistributorCategoryChain,
			distributor.DistributorCategory{Key: c.FirstSortAccessId, Name: c.FirstSortName})
	}
	if c.SecondSortAccessId != "" && c.SecondSortName != "" {
		result.DistributorCategoryChain = append(result.DistributorCategoryChain,
			distributor.DistributorCategory{Key: c.SecondSortAccessId, Name: c.SecondSortName})
	}

	// Component attributes.
	for _, attr := range c.Attributes {
		if attr.AttributeNameEn == "" || attr.AttributeValueName == "" || attr.AttributeValueName == "-" {
			continue
		}
		result.Parameters = append(result.Parameters, distributor.Parameter{
			Name:  attr.AttributeNameEn,
			Value: attr.AttributeValueName,
		})
	}

	// JLC-specific parameters appended after component attributes.
	assemblyVal := "No"
	if c.AssemblyComponentFlag {
		assemblyVal = "Yes"
	}
	result.Parameters = append(result.Parameters, distributor.Parameter{
		Name:  "JLC Assembly",
		Value: assemblyVal,
	})

	if c.ComponentLibraryType != "" {
		libType := "Extended"
		if c.ComponentLibraryType == "base" {
			libType = "Basic"
		}
		result.Parameters = append(result.Parameters, distributor.Parameter{
			Name:  "JLC Library Type",
			Value: libType,
		})
	}

	result.Listings = []distributor.Listing{mapListing(c)}
	return result
}

// mapListing converts an LCSC component to a distributor.Listing.
func mapListing(c *component) distributor.Listing {
	moq := max(c.MinPurchaseNum, 1)

	breaks := makePriceBreaks(c.ComponentPrices)
	unitPrice := ""
	if len(breaks) > 0 {
		unitPrice = breaks[0].UnitPrice
	}

	stock := c.StockCount
	return distributor.Listing{
		SKU:            c.ComponentCode,
		PackagingType:  "Cut Tape",
		Moq:            moq,
		PriceBreaks:    breaks,
		UnitPrice:      unitPrice,
		StockAvailable: &stock,
		FixedOrderCost: "0.0000",
	}
}

// makePriceBreaks converts LCSC componentPrices to distributor.PriceBreak slice.
func makePriceBreaks(prices []componentPrice) []distributor.PriceBreak {
	out := make([]distributor.PriceBreak, 0, len(prices))
	for _, p := range prices {
		out = append(out, distributor.PriceBreak{
			BreakQty:  p.StartNumber,
			UnitPrice: formatPrice(p.ProductPrice),
		})
	}
	return out
}

// formatPrice formats a float64 price to a 4-decimal-place string.
func formatPrice(f float64) string {
	return fmt.Sprintf("%.4f", f)
}

// descriptionOf returns the full description, falling back to the component name
// when the describe field is empty.
func descriptionOf(c *component) string {
	if c.Describe != "" {
		return c.Describe
	}
	return c.ComponentName
}
