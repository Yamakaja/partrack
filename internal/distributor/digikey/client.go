// Package digikey implements a distributor.Client for the DigiKey ProductSearch v4 API.
// Authentication uses the OAuth2 client credentials flow (2-legged); tokens are cached
// in memory per client_id and proactively refreshed before expiry.
package digikey

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	mu         sync.Mutex
	tokens     map[string]*cachedToken // keyed by client_id
	httpClient *http.Client
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL(creds), strings.NewReader(form.Encode()))
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

// doGet performs an authenticated GET, decoding a JSON response into out.
// Returns (true, nil) on 200, (false, nil) on 404, (false, err) otherwise.
// Handles 401 by re-fetching the token and retrying once; 429 by honouring Retry-After and retrying once.
func (c *Client) doGet(ctx context.Context, rawURL string, creds Credentials, out any) (found bool, err error) {
	token, err := c.getToken(ctx, creds)
	if err != nil {
		return false, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return false, fmt.Errorf("digikey: build request: %w", err)
		}
		setHeaders(req, creds, token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return false, fmt.Errorf("digikey: request: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusOK:
			if err := json.Unmarshal(body, out); err != nil {
				return false, fmt.Errorf("digikey: decode response: %w", err)
			}
			return true, nil

		case http.StatusNotFound:
			return false, nil

		case http.StatusBadRequest:
			return false, fmt.Errorf("digikey: 400 bad request: %s", body)

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
			return false, fmt.Errorf("digikey: unexpected status %d: %s", resp.StatusCode, body)
		}
	}
	return false, fmt.Errorf("digikey: max retries exceeded")
}

// ---- DigiKey API response types ----

type productDetailsResponse struct {
	Product *product `json:"Product"`
}

type product struct {
	Manufacturer      nameObj            `json:"Manufacturer"`
	Description       descriptionObj     `json:"Description"`
	Category          nameObj            `json:"Category"`
	DatasheetUrl      string             `json:"DatasheetUrl"`
	Parameters        []parameterValue   `json:"Parameters"`
	ProductVariations []productVariation `json:"ProductVariations"`
	ProductStatus     statusObj          `json:"ProductStatus"`
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
	DigiKeyProductNumber           string       `json:"DigiKeyProductNumber"`
	MarketPlace                    bool         `json:"MarketPlace"`
	PackageType                    nameObj      `json:"PackageType"`
	QuantityAvailableforPackageType int32       `json:"QuantityAvailableforPackageType"`
	MinimumOrderQuantity           int32        `json:"MinimumOrderQuantity"`
	StandardPricing                []priceBreak `json:"StandardPricing"`
	MyPricing                      []priceBreak `json:"MyPricing"`
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
	DigiKeyProductNumber           string       `json:"DigiKeyProductNumber"`
	MarketPlace                    bool         `json:"MarketPlace"`
	PackageType                    nameObj      `json:"PackageType"`
	QuantityAvailableforPackageType int32       `json:"QuantityAvailableforPackageType"`
	MinimumOrderQuantity           int32        `json:"MinimumOrderQuantity"`
	StandardPricing                []priceBreak `json:"StandardPricing"`
	MyPricing                      []priceBreak `json:"MyPricing"`
}

// ---- distributor.Client implementation ----

// EnrichPart fetches full product metadata and listings for mpn from DigiKey.
// Returns nil, nil if the MPN is not found.
func (c *Client) EnrichPart(ctx context.Context, mpn string, rawCreds json.RawMessage) (*distributor.EnrichmentResult, error) {
	if rawCreds == nil {
		return nil, fmt.Errorf("digikey: credentials required")
	}
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}

	endpoint := baseURL(creds) + "/products/v4/search/" + url.PathEscape(mpn) + "/productdetails"
	var resp productDetailsResponse
	found, err := c.doGet(ctx, endpoint, creds, &resp)
	if err != nil {
		return nil, err
	}
	if !found || resp.Product == nil {
		return nil, nil
	}

	p := resp.Product
	result := &distributor.EnrichmentResult{
		Manufacturer: p.Manufacturer.Name,
		Description:  p.Description.ProductDescription,
		Category:     p.Category.Name,
		DatasheetURL: p.DatasheetUrl,
	}
	if result.Description == "" {
		result.Description = p.Description.DetailedDescription
	}

	// Extract parameters; also capture package while iterating.
	var packageCase, supplierPackage string
	for _, param := range p.Parameters {
		if param.ValueText == "" || param.ValueText == "-" {
			continue
		}
		switch param.ParameterText {
		case "Package / Case":
			packageCase = param.ValueText
		case "Supplier Device Package":
			supplierPackage = param.ValueText
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

	if packageCase != "" {
		result.Package = packageCase
	} else if supplierPackage != "" {
		result.Package = supplierPackage
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

	return result, nil
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

// ---- helpers ----

// buildListing converts a DigiKey variation's fields into a distributor.Listing.
// MyPricing takes precedence over StandardPricing when non-empty (account-level pricing).
func buildListing(sku, packagingType string, stock, moq int32, std, my []priceBreak) distributor.Listing {
	pricing := std
	if len(my) > 0 {
		pricing = my
	}
	unitPrice := ""
	if len(pricing) > 0 {
		unitPrice = fmt.Sprintf("%.4f", pricing[0].UnitPrice)
	}
	stock32 := stock
	return distributor.Listing{
		SKU:            sku,
		PackagingType:  packagingType,
		UnitPrice:      unitPrice,
		StockAvailable: &stock32,
		Moq:            moq,
		FixedOrderCost: "0.0000",
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
