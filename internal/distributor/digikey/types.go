package digikey

import (
	"fmt"
	"strings"
	"time"
)

// ---- DigiKey API response types ----

type productDetailsResponse struct {
	Product *product `json:"Product"`
}

type product struct {
	Manufacturer           nameObj            `json:"Manufacturer"`
	ManufacturerPartNumber string             `json:"ManufacturerProductNumber"`
	Description            descriptionObj     `json:"Description"`
	Category               categoryObj        `json:"Category"`
	DatasheetUrl           string             `json:"DatasheetUrl"`
	ProductUrl             string             `json:"ProductUrl"`
	PhotoUrl               string             `json:"PhotoUrl"`
	OtherNames             []string           `json:"OtherNames"`
	Parameters             []parameterValue   `json:"Parameters"`
	ProductVariations      []productVariation `json:"ProductVariations"`
	ProductStatus          statusObj          `json:"ProductStatus"`
}

type nameObj struct {
	Name string `json:"Name"`
}

type categoryObj struct {
	CategoryId      int32         `json:"CategoryId"`
	ParentId        int32         `json:"ParentId"`
	Name            string        `json:"Name"`
	ChildCategories []categoryObj `json:"ChildCategories"`
}

type descriptionObj struct {
	ProductDescription  string `json:"ProductDescription"`
	DetailedDescription string `json:"DetailedDescription"`
}

type statusObj struct {
	Status string `json:"Status"`
}

type parameterValue struct {
	ParameterId   int32  `json:"ParameterId"`
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
	DigiReelFee                     float64      `json:"DigiReelFee"`
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
	DigiReelFee                     float64      `json:"DigiReelFee"`
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

// ---- MyLists API structs ----

type createListRequest struct {
	ListName string `json:"ListName"`
	Source   string `json:"Source"`
}

type createListResponse struct {
	Id string `json:"Id"`
}

type requestedQuantity struct {
	Quantity int32 `json:"Quantity"`
}

type requestedPart struct {
	RequestedPartNumber string              `json:"RequestedPartNumber"`
	CustomerReference   string              `json:"CustomerReference"`
	Quantities          []requestedQuantity `json:"Quantities"`
}

// ---- Quote API structs ----

type createQuoteRequest struct {
	QuoteName string `json:"QuoteName"`
}

type createQuoteResponse struct {
	Quote struct {
		QuoteId   int64  `json:"QuoteId"`
		QuoteName string `json:"QuoteName"`
	} `json:"Quote"`
}

type addProductToQuote struct {
	ProductNumber     string  `json:"ProductNumber"`
	CustomerReference string  `json:"CustomerReference"`
	Quantities        []int32 `json:"Quantities"`
}

type quoteProductQuantity struct {
	Quantity      int32   `json:"Quantity"`
	UnitPrice     float64 `json:"UnitPrice"`
	ExtendedPrice float64 `json:"ExtendedPrice"`
}

type quoteProduct struct {
	DigikeyProductNumber string                 `json:"DigikeyProductNumber"`
	CustomerReference    string                 `json:"CustomerReference"`
	QuantityAvailable    int32                  `json:"QuantityAvailable"`
	IsObsolete           bool                   `json:"IsObsolete"`
	IsDiscontinued       bool                   `json:"IsDiscontinued"`
	ExpirationDate       *time.Time             `json:"ExpirationDate"`
	Quantities           []quoteProductQuantity `json:"Quantities"`
}

type productsFromQuoteResponse struct {
	Products []quoteProduct `json:"Products"`
}

// ---- OrderStatus API structs ----

type salesOrderStatusInfo struct {
	SalesOrderStatus string `json:"SalesOrderStatus"`
	ShortDescription string `json:"ShortDescription"`
}

type salesOrderLineItem struct {
	DigiKeyProductNumber string `json:"DigiKeyProductNumber"`
	QuantityOrdered      int32  `json:"QuantityOrdered"`
	QuantityShipped      int32  `json:"QuantityShipped"`
	QuantityBackOrder    int32  `json:"QuantityBackOrder"`
}

type salesOrderResponse struct {
	SalesOrderId int32                `json:"SalesOrderId"`
	Status       salesOrderStatusInfo `json:"Status"`
	LineItems    []salesOrderLineItem `json:"LineItems"`
}

// ---- locale → domain map ----

var localeSiteDomain = map[string]string{
	"US": "digikey.com", "CA": "digikey.ca", "UK": "digikey.co.uk",
	"DE": "digikey.de", "AT": "digikey.at", "BE": "digikey.be",
	"DK": "digikey.dk", "FI": "digikey.fi", "IE": "digikey.ie",
	"IT": "digikey.it", "LU": "digikey.lu", "NL": "digikey.nl",
	"NO": "digikey.no", "PL": "digikey.pl", "PT": "digikey.pt",
	"ES": "digikey.es", "SE": "digikey.se", "CH": "digikey.ch",
	"AU": "digikey.com.au", "NZ": "digikey.co.nz", "SG": "digikey.sg",
	"HK": "digikey.hk", "JP": "digikey.jp", "CN": "digikey.cn",
	"KR": "digikey.kr", "TW": "digikey.tw", "IN": "digikey.in",
	"MX": "digikey.com.mx", "BR": "digikey.com.br",
}

func listURL(creds Credentials, listID string) string {
	domain := localeSiteDomain[strings.ToUpper(orDefault(creds.LocaleSite, "US"))]
	if domain == "" {
		domain = "digikey.com"
	}
	return fmt.Sprintf("https://www.%s/en/mylists/list/%s", domain, listID)
}
