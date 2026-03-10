package digikey

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// ---- distributor.Client implementation ----

// CredentialSpec describes the fields required to configure a DigiKey API integration.
func (c *Client) CredentialSpec() []distributor.CredentialField {
	return []distributor.CredentialField{
		{Key: "client_id", Label: "Client ID", Required: true, Secret: false, Description: "DigiKey API application Client ID"},
		{Key: "client_secret", Label: "Client Secret", Required: true, Secret: true, Description: "DigiKey API application Client Secret"},
		{Key: "locale_site", Label: "Locale Site", Required: false, Secret: false, Placeholder: "US", Description: "DigiKey site (US, CA, UK, etc.). Defaults to US."},
		{Key: "locale_currency", Label: "Currency", Required: false, Secret: false, Placeholder: "USD"},
		{Key: "locale_language", Label: "Language", Required: false, Secret: false, Placeholder: "en"},
		{Key: "sandbox", Label: "Sandbox Mode", Required: false, Secret: false, Description: "Use DigiKey sandbox API (for testing)", Type: "bool"},
	}
}

// categoryChain walks the ChildCategories tree and returns the full ancestry
// ordered from most-specific (leaf, index 0) to least-specific (root, last).
// DigiKey nests the matched subcategory inside the top-level category object.
func categoryChain(c categoryObj) []distributor.DistributorCategory {
	var chain []distributor.DistributorCategory
	for {
		chain = append(chain, distributor.DistributorCategory{
			Key:  strconv.Itoa(int(c.CategoryId)),
			Name: c.Name,
		})
		if len(c.ChildCategories) == 0 {
			break
		}
		c = c.ChildCategories[0]
	}
	// Reverse so index 0 is the leaf (most specific).
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// mapProductToEnrichment converts a DigiKey product struct to an EnrichmentResult.
func mapProductToEnrichment(p *product) *distributor.EnrichmentResult {
	// Prefer the detailed description for the long description field; fall back to short.
	description := p.Description.DetailedDescription
	if description == "" {
		description = p.Description.ProductDescription
	}
	result := &distributor.EnrichmentResult{
		MPN:                      p.ManufacturerPartNumber,
		Manufacturer:             p.Manufacturer.Name,
		Description:              description,
		ShortDescription:         p.Description.ProductDescription,
		DistributorCategoryChain: categoryChain(p.Category),
		DatasheetURL:             p.DatasheetUrl,
		ProductURL:               p.ProductUrl,
		ImageURL:                 p.PhotoUrl,
		Aliases:                  p.OtherNames,
		Package:                  extractPackage(p.Parameters),
		LifecycleStatus:          mapProductStatus(p.ProductStatus.Status),
	}

	for _, param := range p.Parameters {
		if param.ValueText == "" || param.ValueText == "-" {
			continue
		}
		p2 := distributor.Parameter{
			DistributorParamID: strconv.Itoa(int(param.ParameterId)),
			Name:               param.ParameterText,
			Value:              param.ValueText,
		}
		result.Parameters = append(result.Parameters, p2)
	}

	// Build one Listing per non-marketplace ProductVariation.
	for _, v := range p.ProductVariations {
		if v.MarketPlace {
			continue
		}
		listing := buildListing(
			v.DigiKeyProductNumber, v.PackageType.Name,
			v.QuantityAvailableforPackageType, v.MinimumOrderQuantity,
			v.StandardPricing, v.MyPricing, v.DigiReelFee,
		)
		result.Listings = append(result.Listings, listing)
	}

	return result
}

// EnrichPart fetches full product metadata and listings for a specific DigiKey SKU.
// The SKU must be chosen unambiguously by the caller (e.g. selected from SearchParts results).
// Returns nil, nil if the SKU is not found.
func (c *Client) EnrichPart(ctx context.Context, sku string, rawCreds json.RawMessage) (*distributor.EnrichmentResult, error) {
	if rawCreds == nil {
		return nil, fmt.Errorf("digikey: credentials required")
	}
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}

	// Check search result cache before making an API call.
	cacheKey := creds.ClientID + "/" + sku
	if v, ok := c.searchCache.Load(cacheKey); ok {
		entry := v.(searchCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return mapProductToEnrichment(&entry.product), nil
		}
		c.searchCache.Delete(cacheKey)
	}

	endpoint := baseURL(creds) + "/products/v4/search/" + url.PathEscape(sku) + "/productdetails"
	var resp productDetailsResponse
	found, err := c.doGet(ctx, endpoint, creds, &resp)
	if err != nil {
		return nil, err
	}
	if !found || resp.Product == nil {
		return nil, nil
	}

	return mapProductToEnrichment(resp.Product), nil
}

// RefreshListing fetches current stock and price for sku from DigiKey.
// Returns nil, nil if the SKU is not found in the pricing response.
func (c *Client) RefreshListing(ctx context.Context, sku string, rawCreds json.RawMessage) (*distributor.Listing, error) {
	if rawCreds == nil {
		return nil, fmt.Errorf("digikey: credentials required")
	}
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}

	endpoint := baseURL(creds) + "/products/v4/search/" + url.PathEscape(sku) +
		"/pricing?limit=10&offset=0&excludeMarketplace=true"
	var resp productPricingResponse
	found, err := c.doGet(ctx, endpoint, creds, &resp)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	for _, pp := range resp.ProductPricings {
		for _, v := range pp.ProductVariations {
			if v.DigiKeyProductNumber != sku || v.MarketPlace {
				continue
			}
			listing := buildListing(
				v.DigiKeyProductNumber, v.PackageType.Name,
				v.QuantityAvailableforPackageType, v.MinimumOrderQuantity,
				v.StandardPricing, v.MyPricing, v.DigiReelFee,
			)
			return &listing, nil
		}
	}
	return nil, nil
}

// SearchParts performs a keyword / MPN / manufacturer search using DigiKey's
// ProductSearch v4 KeywordSearch endpoint and maps results into the
// distributor.SearchResult abstraction.
func (c *Client) SearchParts(ctx context.Context, q distributor.SearchQuery, rawCreds json.RawMessage) (*distributor.SearchResult, error) {
	if rawCreds == nil {
		return nil, fmt.Errorf("digikey: credentials required")
	}
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
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

	endpoint := baseURL(creds) + "/products/v4/search/keyword"
	body := keywordRequest{
		Keywords: keywords,
		Limit:    limit,
		Offset:   offset,
	}

	var resp keywordResponse
	found, err := c.doPostJSON(ctx, endpoint, &body, creds, &resp)
	if err != nil {
		return nil, err
	}
	if !found || len(resp.Products) == 0 {
		return nil, nil
	}

	// Map products to SearchProduct slice.
	products := make([]distributor.SearchProduct, 0, len(resp.Products))
	for _, p := range resp.Products {
		sp := distributor.SearchProduct{
			MPN:          p.ManufacturerPartNumber,
			Manufacturer: p.Manufacturer.Name,
			Description:  p.Description.ProductDescription,
			Category:     p.Category.Name, // display name for search results
			Package:      extractPackage(p.Parameters),
		}
		if sp.Description == "" {
			sp.Description = p.Description.DetailedDescription
		}

		// Choose a primary variation (first non-marketplace) for SKU/price/stock.
		for _, v := range p.ProductVariations {
			if v.MarketPlace {
				continue
			}
			sp.PrimarySKU = v.DigiKeyProductNumber
			if len(v.StandardPricing) > 0 {
				sp.UnitPrice = fmt.Sprintf("%.4f", v.StandardPricing[0].UnitPrice)
			}
			stock := v.QuantityAvailableforPackageType
			sp.StockAvailable = &stock
			break
		}

		products = append(products, sp)

		// Cache the full product by each variant SKU so EnrichPart can skip an API call.
		expiry := time.Now().Add(1 * time.Hour)
		for _, v := range p.ProductVariations {
			if v.MarketPlace {
				continue
			}
			c.searchCache.Store(creds.ClientID+"/"+v.DigiKeyProductNumber, searchCacheEntry{
				product:   p,
				expiresAt: expiry,
			})
		}
	}

	// Compute ExactMatchIndices by correlating ExactMatches with Products.
	exactIndices := make([]int, 0, len(resp.ExactMatches))
	if len(resp.ExactMatches) > 0 {
		for i, p := range resp.Products {
			for _, em := range resp.ExactMatches {
				if p.ManufacturerPartNumber == em.ManufacturerPartNumber &&
					p.Manufacturer.Name == em.Manufacturer.Name {
					exactIndices = append(exactIndices, i)
					break
				}
			}
		}
	}

	return &distributor.SearchResult{
		Products:          products,
		TotalCount:        resp.ProductsCount,
		ExactMatchIndices: exactIndices,
	}, nil
}

// ---- helpers ----

// extractPackage returns the best package string from a DigiKey parameter list.
// Prefers "Package / Case" over "Supplier Device Package"; skips empty or "-" values.
func extractPackage(params []parameterValue) string {
	var packageCase, supplierPackage string
	for _, p := range params {
		if p.ValueText == "" || p.ValueText == "-" {
			continue
		}
		switch p.ParameterText {
		case "Package / Case":
			packageCase = p.ValueText
		case "Supplier Device Package":
			supplierPackage = p.ValueText
		}
	}
	if packageCase != "" {
		return packageCase
	}
	return supplierPackage
}

// mapProductStatus maps a DigiKey ProductStatus.Status string to a distributor
// LifecycleStatus. Returns LifecycleStatusUnknown for unmapped values so the
// caller leaves the existing DB value unchanged.
func mapProductStatus(status string) distributor.LifecycleStatus {
	switch status {
	case "Active", "Preliminary", "Limited Supply":
		return distributor.LifecycleStatusActive
	case "Not For New Designs":
		return distributor.LifecycleStatusNRND
	case "Discontinued", "Obsolete", "Last Time Buy":
		return distributor.LifecycleStatusObsolete
	case "Unavailable", "No Longer Available":
		return distributor.LifecycleStatusNoLongerAvailable
	default:
		return distributor.LifecycleStatusUnknown
	}
}

// buildListing converts a DigiKey variation's fields into a distributor.Listing.
// MyPricing takes precedence over StandardPricing when non-empty (account-level pricing).
// Moq is clamped to a minimum of 1 to satisfy the DB CHECK constraint.
func buildListing(sku, packagingType string, stock, moq int32, std, my []priceBreak, reelFee float64) distributor.Listing {
	if moq < 1 {
		moq = 1
	}
	pricing := std
	if len(my) > 0 {
		pricing = my
	}
	breaks := make([]distributor.PriceBreak, 0, len(pricing))
	for _, pb := range pricing {
		breaks = append(breaks, distributor.PriceBreak{
			BreakQty:  pb.BreakQuantity,
			UnitPrice: fmt.Sprintf("%.4f", pb.UnitPrice),
		})
	}
	unitPrice := ""
	if len(breaks) > 0 {
		unitPrice = breaks[0].UnitPrice
	}
	stock32 := stock
	return distributor.Listing{
		SKU:            sku,
		PackagingType:  packagingType,
		UnitPrice:      unitPrice,
		StockAvailable: &stock32,
		Moq:            moq,
		FixedOrderCost: fmt.Sprintf("%.4f", reelFee),
		PriceBreaks:    breaks,
	}
}

// isNumeric reports whether s looks like a pure number (digits, dot, sign, exponent).
// Used to decide if the last token of a ValueText is a unit or part of the number.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' && r != 'e' && r != 'E' {
			return false
		}
	}
	return true
}
