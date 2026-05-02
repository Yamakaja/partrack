package mouser

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// ---- distributor.Client implementation ----

// EnrichPart fetches full product metadata and listings for a specific Mouser part number (SKU).
// Returns nil, nil if the SKU is not found.
func (c *Client) EnrichPart(ctx context.Context, sku string, rawCreds json.RawMessage) (*distributor.EnrichmentResult, error) {
	creds, err := parseCredentials(rawCreds)
	if err != nil {
		return nil, err
	}

	// Check cache first.
	cacheKey := creds.APIKey + "/" + sku
	if v, ok := c.searchCache.Load(cacheKey); ok {
		entry := v.(cacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return mapPartToEnrichment(&entry.part), nil
		}
		c.searchCache.Delete(cacheKey)
	}

	var resp searchResponseRoot
	err = c.doPost(ctx, "/api/v1/search/partnumber", creds.APIKey, searchByPartRequestRoot{
		SearchByPartRequest: searchByPartRequest{
			MouserPartNumber:  sku,
			PartSearchOptions: "Exact",
		},
	}, &resp)
	if err != nil {
		return nil, err
	}
	if err := checkErrors(resp.Errors); err != nil {
		return nil, err
	}
	if len(resp.SearchResults.Parts) == 0 {
		return nil, nil
	}

	part := resp.SearchResults.Parts[0]
	c.searchCache.Store(cacheKey, cacheEntry{part: part, expiresAt: time.Now().Add(time.Hour)})
	return mapPartToEnrichment(&part), nil
}

// RefreshListing fetches current price and stock for a Mouser part number.
// Returns an empty slice when the SKU is not found.
func (c *Client) RefreshListing(ctx context.Context, sku string, rawCreds json.RawMessage) ([]distributor.Listing, error) {
	creds, err := parseCredentials(rawCreds)
	if err != nil {
		return nil, err
	}

	var resp searchResponseRoot
	err = c.doPost(ctx, "/api/v1/search/partnumber", creds.APIKey, searchByPartRequestRoot{
		SearchByPartRequest: searchByPartRequest{
			MouserPartNumber:  sku,
			PartSearchOptions: "Exact",
		},
	}, &resp)
	if err != nil {
		return nil, err
	}
	if err := checkErrors(resp.Errors); err != nil {
		return nil, err
	}
	if len(resp.SearchResults.Parts) == 0 {
		return []distributor.Listing{}, nil
	}

	return []distributor.Listing{mapListing(resp.SearchResults.Parts[0])}, nil
}

// SearchParts performs a keyword search using Mouser's keyword search endpoint.
func (c *Client) SearchParts(ctx context.Context, q distributor.SearchQuery, rawCreds json.RawMessage) (*distributor.SearchResult, error) {
	creds, err := parseCredentials(rawCreds)
	if err != nil {
		return nil, err
	}

	keywords := strings.TrimSpace(q.Keywords)
	if keywords == "" {
		return nil, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	var resp searchResponseRoot
	err = c.doPost(ctx, "/api/v1/search/keyword", creds.APIKey, searchByKeywordRequestRoot{
		SearchByKeywordRequest: searchByKeywordRequest{
			Keyword:        keywords,
			Records:        limit,
			StartingRecord: offset,
		},
	}, &resp)
	if err != nil {
		return nil, err
	}
	if err := checkErrors(resp.Errors); err != nil {
		return nil, err
	}

	products := make([]distributor.SearchProduct, 0, len(resp.SearchResults.Parts))
	for _, p := range resp.SearchResults.Parts {
		sp := distributor.SearchProduct{
			MPN:          p.ManufacturerPartNumber,
			Manufacturer: p.Manufacturer,
			Description:  p.Description,
			Category:     p.Category,
			PrimarySKU:   p.MouserPartNumber,
		}
		if len(p.PriceBreaks) > 0 {
			sp.UnitPrice = parseMouserPrice(p.PriceBreaks[0].Price)
		}
		stock := parseInt32(p.AvailabilityInStock)
		sp.StockAvailable = &stock

		products = append(products, sp)

		// Warm EnrichPart cache for each result.
		if p.MouserPartNumber != "" {
			key := creds.APIKey + "/" + p.MouserPartNumber
			c.searchCache.Store(key, cacheEntry{part: p, expiresAt: time.Now().Add(time.Hour)})
		}
	}

	return &distributor.SearchResult{
		Products:   products,
		TotalCount: int32(resp.SearchResults.NumberOfResult),
		// Mouser keyword search does not distinguish exact matches.
	}, nil
}

// ---- mapping helpers ----

// mapPartToEnrichment converts a mouserPart to an EnrichmentResult.
func mapPartToEnrichment(p *mouserPart) *distributor.EnrichmentResult {
	result := &distributor.EnrichmentResult{
		MPN:          p.ManufacturerPartNumber,
		Manufacturer: p.Manufacturer,
		Description:  p.Description,
		// ShortDescription intentionally left empty — Mouser has one description field.
		DatasheetURL: p.DataSheetUrl,
		ImageURL:     p.ImagePath,
		ProductURL:   p.ProductDetailUrl,
		LifecycleStatus: mapLifecycleStatus(p.LifecycleStatus, p.IsDiscontinued),
	}

	// Category chain: single level (Mouser provides a flat category string, no hierarchy).
	if p.Category != "" {
		result.DistributorCategoryChain = []distributor.DistributorCategory{
			{Key: normalizeCategory(p.Category), Name: p.Category},
		}
	}

	// Aliases from alternate packagings (MPNs only, no Mouser SKUs available here).
	for _, ap := range p.AlternatePackagings {
		if alias := strings.TrimSpace(ap.APMfrPN); alias != "" {
			result.Aliases = append(result.Aliases, alias)
		}
	}

	// Parameters from product attributes (stored as-is; names reflect account locale).
	for _, attr := range p.ProductAttributes {
		if attr.AttributeName != "" && attr.AttributeValue != "" {
			result.Parameters = append(result.Parameters, distributor.Parameter{
				Name:  attr.AttributeName,
				Value: attr.AttributeValue,
			})
		}
	}

	result.Listings = []distributor.Listing{mapListing(*p)}
	return result
}

// mapListing converts a mouserPart to a distributor.Listing.
// listingURL is the per-SKU product page URL, sourced from ProductDetailUrl.
func mapListing(p mouserPart) distributor.Listing {
	packagingType := "Tube"
	if p.Reeling {
		packagingType = "Tape & Reel"
	}

	moq := parseInt32(p.Min)
	if moq < 1 {
		moq = 1
	}

	breaks := parsePriceBreaks(p.PriceBreaks)
	unitPrice := ""
	if len(breaks) > 0 {
		unitPrice = breaks[0].UnitPrice
	}

	stock := parseInt32(p.AvailabilityInStock)
	sku := p.MouserPartNumber
	if sku == "N/A" {
		sku = ""
	}
	return distributor.Listing{
		SKU:            sku,
		PackagingType:  packagingType,
		Moq:            moq,
		PriceBreaks:    breaks,
		UnitPrice:      unitPrice,
		StockAvailable: &stock,
		FixedOrderCost: "0.0000",
		ListingURL:     p.ProductDetailUrl,
	}
}

// parsePriceBreaks converts Mouser price break structs to distributor.PriceBreak slice,
// skipping entries where the price cannot be parsed.
func parsePriceBreaks(pbs []mouserPricebreak) []distributor.PriceBreak {
	out := make([]distributor.PriceBreak, 0, len(pbs))
	for _, pb := range pbs {
		price := parseMouserPrice(pb.Price)
		if price == "" {
			continue
		}
		out = append(out, distributor.PriceBreak{
			BreakQty:  int32(pb.Quantity),
			UnitPrice: price,
		})
	}
	return out
}

// currencyRE matches currency symbols and alphabetic currency codes we want to strip.
var currencyRE = regexp.MustCompile(`[€$£¥₺₹₩฿₫₪₴₽]|[A-Za-z]+`)

// parseMouserPrice normalises a locale-formatted Mouser price string (e.g. "0,903 €", "$1.25")
// to a 4-decimal-place string (e.g. "0.9030", "1.2500"), or "" on parse failure.
func parseMouserPrice(s string) string {
	// Strip currency symbols, alphabetic codes, and whitespace.
	s = currencyRE.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")

	switch {
	case lastComma >= 0 && lastDot >= 0:
		// Both separators present — the last one is the decimal separator.
		if lastComma > lastDot {
			// EU format: "1.234,56" — remove dots, replace last comma with dot.
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			// US format: "1,234.56" — remove commas.
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastComma >= 0:
		// Only comma present → EU decimal separator.
		s = strings.ReplaceAll(s, ",", ".")
	// Only dot or no separator → already normalised.
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%.4f", f)
}

// normalizeCategory returns a stable, lowercase slug suitable as a DistributorCategory.Key.
// Returns "uncategorized" if the input is blank (Key must never be empty or "0").
var nonAlphanumRE = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeCategory(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphanumRE.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" || s == "0" {
		return "uncategorized"
	}
	return s
}

// mapLifecycleStatus maps a Mouser lifecycle status string to a distributor.LifecycleStatus.
func mapLifecycleStatus(status, isDiscontinued string) distributor.LifecycleStatus {
	switch status {
	case "Active":
		return distributor.LifecycleStatusActive
	case "Not Recommended for New Design", "NRND":
		return distributor.LifecycleStatusNRND
	case "Discontinued", "Obsolete", "End of Life":
		return distributor.LifecycleStatusObsolete
	case "No Longer Available", "Lifebuy":
		return distributor.LifecycleStatusNoLongerAvailable
	}
	// Fall back on IsDiscontinued flag when status is not mapped.
	if strings.EqualFold(isDiscontinued, "true") {
		return distributor.LifecycleStatusNoLongerAvailable
	}
	return distributor.LifecycleStatusUnknown
}

// parseInt32 parses a trimmed decimal string to int32, returning 0 on error.
func parseInt32(s string) int32 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0
	}
	return int32(n)
}
