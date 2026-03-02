// Package digikey implements a distributor.Client for the DigiKey ProductSearch v4 API.
// Authentication uses the OAuth2 client credentials flow (2-legged); tokens are cached
// in memory per client_id and proactively refreshed before expiry.
package digikey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// Credentials is the JSON structure stored in distributor_configs.api_credentials for DigiKey.
type Credentials struct {
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	LocaleSite     string `json:"locale_site"`     // default "US"
	LocaleCurrency string `json:"locale_currency"` // default "USD"
	LocaleLanguage string `json:"locale_language"` // default "en"
	AccountID      string `json:"account_id"`      // optional; required for quote/MyList operations
	Sandbox        bool   `json:"sandbox"`         // true → sandbox-api.digikey.com
}

// Client is the DigiKey distributor implementation.
type Client struct {
	mu          sync.Mutex
	tokens      map[string]*cachedToken // keyed by client_id
	httpClient  *http.Client
	debug       bool
	searchCache sync.Map // key: clientID+"/"+sku → searchCacheEntry
}

type searchCacheEntry struct {
	product   product
	expiresAt time.Time
}

type cachedToken struct {
	accessToken string
	expiry      time.Time
}

// New returns a new DigiKey Client ready for use.
func New() *Client {
	return &Client{
		tokens:     make(map[string]*cachedToken),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetDebug enables or disables verbose request/response logging for all DigiKey API calls.
// When enabled, each call logs the method, URL, request headers, request body,
// response status, and response body. Intended for debugging credential and API issues only.
func (c *Client) SetDebug(enabled bool) {
	c.debug = enabled
}

// ---- host helpers ----

func baseURL(creds Credentials) string {
	if creds.Sandbox {
		return "https://sandbox-api.digikey.com"
	}
	return "https://api.digikey.com"
}

func tokenURL(creds Credentials) string {
	return baseURL(creds) + "/v1/oauth2/token"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ---- token management ----

// getToken returns a valid bearer token, fetching a new one if the cached one expires soon.
func (c *Client) getToken(ctx context.Context, creds Credentials) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tok := c.tokens[creds.ClientID]
	if tok != nil && time.Now().Add(60*time.Second).Before(tok.expiry) {
		return tok.accessToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", creds.ClientID)
	form.Set("client_secret", creds.ClientSecret)

	tokenURLStr := tokenURL(creds)
	if c.debug {
		log.Printf("digikey [token] POST %s  client_id=%s sandbox=%v", tokenURLStr, creds.ClientID, creds.Sandbox)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURLStr, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("digikey: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("digikey: token request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if c.debug {
		log.Printf("digikey [token] response status=%d body=%s", resp.StatusCode, body)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("digikey: token endpoint %d: %s", resp.StatusCode, body)
	}

	var tokResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return "", fmt.Errorf("digikey: decode token response: %w", err)
	}

	c.tokens[creds.ClientID] = &cachedToken{
		accessToken: tokResp.AccessToken,
		expiry:      time.Now().Add(time.Duration(tokResp.ExpiresIn) * time.Second),
	}
	return tokResp.AccessToken, nil
}

func (c *Client) invalidateToken(clientID string) {
	c.mu.Lock()
	delete(c.tokens, clientID)
	c.mu.Unlock()
}

// ---- HTTP execution ----

// setHeaders applies the standard DigiKey auth and locale headers to a request.
func setHeaders(req *http.Request, creds Credentials, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-DIGIKEY-Client-Id", creds.ClientID)
	req.Header.Set("X-DIGIKEY-Locale-Site", orDefault(creds.LocaleSite, "US"))
	req.Header.Set("X-DIGIKEY-Locale-Currency", orDefault(creds.LocaleCurrency, "USD"))
	req.Header.Set("X-DIGIKEY-Locale-Language", orDefault(creds.LocaleLanguage, "en"))
	if creds.AccountID != "" {
		req.Header.Set("X-DIGIKEY-Account-Id", creds.AccountID)
	}
}

// doRequest performs an authenticated HTTP request, decoding a JSON response into out.
// For POST requests body is marshalled to JSON and Content-Type is set; for GET pass body=nil.
// Returns (true, nil) on 200, (false, nil) on 404, (false, err) otherwise.
// Retries once on 401 (token refresh) and once on 429 (Retry-After backoff).
func (c *Client) doRequest(ctx context.Context, method, rawURL string, body any, creds Credentials, out any) (bool, error) {
	token, err := c.getToken(ctx, creds)
	if err != nil {
		return false, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		var bodyReader io.Reader
		var encodedBody []byte
		if body != nil {
			var err error
			encodedBody, err = json.Marshal(body)
			if err != nil {
				return false, fmt.Errorf("digikey: encode request body: %w", err)
			}
			bodyReader = bytes.NewReader(encodedBody)
		}

		req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
		if err != nil {
			return false, fmt.Errorf("digikey: build request: %w", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		setHeaders(req, creds, token)

		if c.debug {
			log.Printf("digikey [request] attempt=%d %s %s", attempt+1, method, rawURL)
			for k, vs := range req.Header {
				log.Printf("digikey [request]   %s: %s", k, strings.Join(vs, ", "))
			}
			if encodedBody != nil {
				log.Printf("digikey [request] body=%s", encodedBody)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return false, fmt.Errorf("digikey: request: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if c.debug {
			log.Printf("digikey [response] status=%d body=%s", resp.StatusCode, respBody)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			if err := json.Unmarshal(respBody, out); err != nil {
				return false, fmt.Errorf("digikey: decode response: %w", err)
			}
			return true, nil

		case http.StatusNotFound:
			return false, nil

		case http.StatusBadRequest:
			return false, fmt.Errorf("digikey: 400 bad request: %s", respBody)

		case http.StatusUnauthorized:
			if attempt == 0 {
				c.invalidateToken(creds.ClientID)
				token, err = c.getToken(ctx, creds)
				if err != nil {
					return false, err
				}
				continue
			}
			return false, fmt.Errorf("digikey: 401 unauthorized after token refresh")

		case http.StatusTooManyRequests:
			if attempt == 0 {
				wait := 5 * time.Second
				if ra := resp.Header.Get("Retry-After"); ra != "" {
					if secs, err := strconv.Atoi(ra); err == nil {
						wait = time.Duration(secs) * time.Second
					}
				}
				select {
				case <-ctx.Done():
					return false, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return false, fmt.Errorf("digikey: 429 rate limited")

		default:
			return false, fmt.Errorf("digikey: unexpected status %d: %s", resp.StatusCode, respBody)
		}
	}
	return false, fmt.Errorf("digikey: max retries exceeded")
}

func (c *Client) doGet(ctx context.Context, rawURL string, creds Credentials, out any) (bool, error) {
	return c.doRequest(ctx, http.MethodGet, rawURL, nil, creds, out)
}

func (c *Client) doPostJSON(ctx context.Context, rawURL string, body any, creds Credentials, out any) (bool, error) {
	return c.doRequest(ctx, http.MethodPost, rawURL, body, creds, out)
}

// ---- DigiKey API response types ----

type productDetailsResponse struct {
	Product *product `json:"Product"`
}

type product struct {
	Manufacturer           nameObj            `json:"Manufacturer"`
	ManufacturerPartNumber string             `json:"ManufacturerProductNumber"`
	Description            descriptionObj     `json:"Description"`
	Category               nameObj            `json:"Category"`
	DatasheetUrl           string             `json:"DatasheetUrl"`
	Parameters             []parameterValue   `json:"Parameters"`
	ProductVariations      []productVariation `json:"ProductVariations"`
	ProductStatus          statusObj          `json:"ProductStatus"`
}

type nameObj struct {
	Name string `json:"Name"`
}

type descriptionObj struct {
	ProductDescription  string `json:"ProductDescription"`
	DetailedDescription string `json:"DetailedDescription"`
}

type statusObj struct {
	Status string `json:"Status"`
}

type parameterValue struct {
	ParameterText string `json:"ParameterText"`
	ParameterType string `json:"ParameterType"` // String|Integer|Double|UnitOfMeasure|...
	ValueText     string `json:"ValueText"`
}

type productVariation struct {
	DigiKeyProductNumber            string       `json:"DigiKeyProductNumber"`
	MarketPlace                     bool         `json:"MarketPlace"`
	PackageType                     nameObj      `json:"PackageType"`
	QuantityAvailableforPackageType int32        `json:"QuantityAvailableforPackageType"`
	MinimumOrderQuantity            int32        `json:"MinimumOrderQuantity"`
	StandardPricing                 []priceBreak `json:"StandardPricing"`
	MyPricing                       []priceBreak `json:"MyPricing"`
}

type priceBreak struct {
	BreakQuantity int32   `json:"BreakQuantity"`
	UnitPrice     float64 `json:"UnitPrice"`
}

type productPricingResponse struct {
	ProductPricings []productPricing `json:"ProductPricings"`
}

type productPricing struct {
	ProductVariations []pricingVariation `json:"ProductVariations"`
}

type pricingVariation struct {
	DigiKeyProductNumber            string       `json:"DigiKeyProductNumber"`
	MarketPlace                     bool         `json:"MarketPlace"`
	PackageType                     nameObj      `json:"PackageType"`
	QuantityAvailableforPackageType int32        `json:"QuantityAvailableforPackageType"`
	MinimumOrderQuantity            int32        `json:"MinimumOrderQuantity"`
	StandardPricing                 []priceBreak `json:"StandardPricing"`
	MyPricing                       []priceBreak `json:"MyPricing"`
}

// keywordRequest mirrors the subset of DigiKey's KeywordRequest we care about.
type keywordRequest struct {
	Keywords             string                `json:"Keywords"`
	Limit                int32                 `json:"Limit,omitempty"`
	Offset               int32                 `json:"Offset,omitempty"`
	FilterOptionsRequest *filterOptionsRequest `json:"FilterOptionsRequest,omitempty"`
	SortOptions          *sortOptions          `json:"SortOptions,omitempty"`
}

// keywordResponse mirrors the parts of KeywordResponse we need.
type keywordResponse struct {
	Products                    []product        `json:"Products"`
	ProductsCount               int32            `json:"ProductsCount"`
	ExactMatches                []product        `json:"ExactMatches"`
	FilterOptions               *filterOptions   `json:"FilterOptions,omitempty"`
	SearchLocaleUsed            *isoSearchLocale `json:"SearchLocaleUsed,omitempty"`
	AppliedParametricFiltersDto []parameterValue `json:"AppliedParametricFiltersDto,omitempty"`
}

// Placeholder types for fields we don't yet use but may in future.
type filterOptionsRequest struct{}
type sortOptions struct{}
type filterOptions struct{}
type isoSearchLocale struct{}

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

// mapProductToEnrichment converts a DigiKey product struct to an EnrichmentResult.
func mapProductToEnrichment(p *product) *distributor.EnrichmentResult {
	result := &distributor.EnrichmentResult{
		MPN:             p.ManufacturerPartNumber,
		Manufacturer:    p.Manufacturer.Name,
		Description:     p.Description.ProductDescription,
		Category:        p.Category.Name,
		DatasheetURL:    p.DatasheetUrl,
		Package:         extractPackage(p.Parameters),
		LifecycleStatus: mapProductStatus(p.ProductStatus.Status),
	}
	if result.Description == "" {
		result.Description = p.Description.DetailedDescription
	}

	for _, param := range p.Parameters {
		if param.ValueText == "" || param.ValueText == "-" {
			continue
		}
		p2 := distributor.Parameter{
			Name:  param.ParameterText,
			Value: param.ValueText,
		}
		// For UnitOfMeasure, split "1.5 A" into value="1.5" unit="A".
		if param.ParameterType == "UnitOfMeasure" {
			if idx := strings.LastIndex(param.ValueText, " "); idx >= 0 {
				right := param.ValueText[idx+1:]
				if !isNumeric(right) {
					p2.Value = param.ValueText[:idx]
					p2.Unit = right
				}
			}
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
			v.StandardPricing, v.MyPricing,
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
				v.StandardPricing, v.MyPricing,
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
			Category:     p.Category.Name,
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
func buildListing(sku, packagingType string, stock, moq int32, std, my []priceBreak) distributor.Listing {
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
		FixedOrderCost: "0.0000",
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
