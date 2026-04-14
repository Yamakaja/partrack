package lcsc

// searchRequest is the body sent to the JLCPCB parts search API.
type searchRequest struct {
	Keyword     string `json:"keyword"`
	CurrentPage int    `json:"currentPage"`
	PageSize    int    `json:"pageSize"`
}

// searchResponse is the top-level envelope from the JLCPCB API.
type searchResponse struct {
	Code    int        `json:"code"`
	Data    searchData `json:"data"`
	Message *string    `json:"message"`
}

type searchData struct {
	ComponentPageInfo componentPageInfo `json:"componentPageInfo"`
}

type componentPageInfo struct {
	Total   int         `json:"total"`
	List    []component `json:"list"`
	PageNum int         `json:"pageNum"`
	Pages   int         `json:"pages"`
}

// component holds the fields for a single LCSC component returned by the API.
type component struct {
	ComponentCode            string           `json:"componentCode"`
	ComponentModelEn         string           `json:"componentModelEn"`
	ComponentBrandEn         string           `json:"componentBrandEn"`
	ComponentName            string           `json:"componentName"`
	ComponentSpecificationEn string           `json:"componentSpecificationEn"`
	Describe                 string           `json:"describe"`
	StockCount               int32            `json:"stockCount"`
	MinPurchaseNum           int32            `json:"minPurchaseNum"`
	ComponentPrices          []componentPrice `json:"componentPrices"`
	LcscGoodsUrl             string           `json:"lcscGoodsUrl"`
	DataManualUrl            string           `json:"dataManualUrl"`
	ComponentImageUrl        string           `json:"componentImageUrl"`
	Attributes               []attribute      `json:"attributes"`
	FirstSortName            string           `json:"firstSortName"`
	FirstSortAccessId        string           `json:"firstSortAccessId"`
	SecondSortName           string           `json:"secondSortName"`
	SecondSortAccessId       string           `json:"secondSortAccessId"`
	AssemblyComponentFlag    bool             `json:"assemblyComponentFlag"`
	ComponentLibraryType     string           `json:"componentLibraryType"`
}

// componentPrice is one tier of the list-price schedule.
type componentPrice struct {
	StartNumber  int32   `json:"startNumber"`
	EndNumber    int32   `json:"endNumber"`
	ProductPrice float64 `json:"productPrice"`
}

// attribute is a key-value specification parameter.
type attribute struct {
	AttributeNameEn    string `json:"attribute_name_en"`
	AttributeValueName string `json:"attribute_value_name"`
}
