package mouser

// ---- Search request types ----

type searchByKeywordRequestRoot struct {
	SearchByKeywordRequest searchByKeywordRequest `json:"SearchByKeywordRequest"`
}

type searchByKeywordRequest struct {
	Keyword        string `json:"keyword"`
	Records        int32  `json:"records"`
	StartingRecord int32  `json:"startingRecord"`
	SearchOptions  string `json:"searchOptions,omitempty"`
}

type searchByPartRequestRoot struct {
	SearchByPartRequest searchByPartRequest `json:"SearchByPartRequest"`
}

type searchByPartRequest struct {
	MouserPartNumber  string `json:"mouserPartNumber"`
	PartSearchOptions string `json:"partSearchOptions,omitempty"` // "Exact" for EnrichPart/RefreshListing
}

// ---- Search response types ----

type searchResponseRoot struct {
	Errors        []errorEntity  `json:"Errors"`
	SearchResults searchResponse `json:"SearchResults"`
}

type searchResponse struct {
	NumberOfResult int          `json:"NumberOfResult"`
	Parts          []mouserPart `json:"Parts"`
}

type errorEntity struct {
	Id      int    `json:"Id"`
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

type mouserPart struct {
	MouserPartNumber       string               `json:"MouserPartNumber"`
	ManufacturerPartNumber string               `json:"ManufacturerPartNumber"`
	Manufacturer           string               `json:"Manufacturer"`
	Description            string               `json:"Description"`
	DataSheetUrl           string               `json:"DataSheetUrl"`
	ImagePath              string               `json:"ImagePath"`
	Category               string               `json:"Category"`
	ProductDetailUrl       string               `json:"ProductDetailUrl"`
	AvailabilityInStock    string               `json:"AvailabilityInStock"`
	Min                    string               `json:"Min"`
	Mult                   string               `json:"Mult"` // TODO: order step, not yet mapped
	Reeling                bool                 `json:"Reeling"`
	LifecycleStatus        string               `json:"LifecycleStatus"`
	IsDiscontinued         string               `json:"IsDiscontinued"`
	PriceBreaks            []mouserPricebreak   `json:"PriceBreaks"`
	ProductAttributes      []productAttribute   `json:"ProductAttributes"`
	AlternatePackagings    []alternatePackaging `json:"AlternatePackagings"`
}

type mouserPricebreak struct {
	Quantity int    `json:"Quantity"`
	Price    string `json:"Price"`
	Currency string `json:"Currency"`
}

type productAttribute struct {
	AttributeName  string `json:"AttributeName"`
	AttributeValue string `json:"AttributeValue"`
}

type alternatePackaging struct {
	APMfrPN string `json:"APMfrPN"`
}

// ---- Cart request types ----

type cartItemRequestRoot struct {
	CartKey   string            `json:"CartKey,omitempty"`
	CartItems []cartItemRequest `json:"CartItems"`
}

type cartItemRequest struct {
	MouserPartNumber   string `json:"MouserPartNumber"`
	Quantity           int32  `json:"Quantity"`
	CustomerPartNumber string `json:"CustomerPartNumber,omitempty"`
}

// ---- Cart response types ----

type cartResponseRoot struct {
	Errors    []errorEntity   `json:"Errors"`
	CartKey   string          `json:"CartKey"`
	CartItems []cartOrderLine `json:"CartItems"`
}

type cartOrderLine struct {
	MouserPartNumber       string  `json:"MouserPartNumber"`
	MfrPartNumber          string  `json:"MfrPartNumber"`
	Quantity               int32   `json:"Quantity"`
	UnitPrice              float64 `json:"UnitPrice"`
	CartItemCustPartNumber string  `json:"CartItemCustPartNumber"`
}

// ---- Order detail types ----
// Used by GET /api/v1/orderhistory/salesOrderNumber

type orderDetail struct {
	Errors          []errorEntity      `json:"Errors"`
	SalesOrderId    string             `json:"SalesOrderId"`
	WebOrderId      int64              `json:"WebOrderId"`
	OrderStatus     int                `json:"OrderStatus"`
	OrderStatusName string             `json:"OrderStatusName"`
	OrderDate       string             `json:"OrderDate"`
	CurrencyCode    string             `json:"CurrencyCode"`
	IsPendingOrder  bool               `json:"IsPendingOrder"`
	BuyerName       string             `json:"BuyerName"`
	PaymentDetail   orderPaymentDetail `json:"PaymentDetail"`
	DeliveryDetail  orderDelivery      `json:"DeliveryDetail"`
	ShippingAddress orderAddress       `json:"ShippingAddress"`
	SummaryDetail   orderSummary       `json:"SummaryDetail"`
	OrderLines      []orderLineItem    `json:"OrderLines"`
}

type orderPaymentDetail struct {
	PoNumber          string `json:"PoNumber"`
	PaymentMethodName string `json:"PaymentMethodName"`
}

type orderDelivery struct {
	ShippingMethodName string           `json:"ShippingMethodName"`
	TrackingDetails    []trackingDetail `json:"TrackingDetails"`
}

type trackingDetail struct {
	Number string `json:"Number"`
	Link   string `json:"Link"`
}

type orderAddress struct {
	AttentionLine   string `json:"AttentionLine"`
	CompanyName     string `json:"CompanyName"`
	AddressOne      string `json:"AddressOne"`
	AddressTwo      string `json:"AddressTwo"`
	City            string `json:"City"`
	StateOrProvince string `json:"StateOrProvince"`
	PostalCode      string `json:"PostalCode"`
	CountryCode     string `json:"CountryCode"`
}

type orderSummary struct {
	MerchandiseTotal    string `json:"MerchandiseTotal"`
	OrderTotal          string `json:"OrderTotal"`
	AdditionalFeesTotal string `json:"AdditionalFeesTotal"`
}

type orderLineItem struct {
	Quantity    int32            `json:"Quantity"`
	UnitPrice   float64          `json:"UnitPrice"`
	ExtPrice    float64          `json:"ExtPrice"`
	ProductInfo orderLineProduct `json:"ProductInfo"`
	Activities  []orderActivity  `json:"Activities"`
}

type orderLineProduct struct {
	MouserPartNumber       string `json:"MouserPartNumber"`
	ManufacturerPartNumber string `json:"ManufacturerPartNumber"`
	PartDescription        string `json:"PartDescription"`
}

type orderActivity struct {
	InvoiceNumber int64  `json:"InvoiceNumber"`
	Date          string `json:"Date"`
}
