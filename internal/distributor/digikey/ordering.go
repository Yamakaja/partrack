package digikey

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// ---- OrderingClient implementation ----

// CreateList implements distributor.OrderingClient.
// Creates a DigiKey MyList and adds the supplied items.
// Returns (result, listID, err); listID is set even when parts-add fails.
func (c *Client) CreateList(ctx context.Context, listName string, items []distributor.OrderItem, rawCreds json.RawMessage) (*distributor.PartsListResult, string, error) {
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, "", fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}

	// Create the list.
	createURL := baseURL(creds) + "/mylists/v1/lists"
	var createResp createListResponse
	ok, err := c.doPostJSON(ctx, createURL, createListRequest{ListName: listName, Source: "partrack"}, creds, &createResp)
	if err != nil {
		return nil, "", fmt.Errorf("digikey: create list: %w", err)
	}
	if !ok || createResp.Id == "" {
		return nil, "", fmt.Errorf("digikey: create list returned no ID")
	}
	listID := createResp.Id

	// Add parts.
	parts := make([]requestedPart, 0, len(items))
	for _, item := range items {
		parts = append(parts, requestedPart{
			RequestedPartNumber: item.SKU,
			CustomerReference:   item.CustomerReference,
			Quantities:          []requestedQuantity{{Quantity: item.Quantity}},
		})
	}
	addURL := fmt.Sprintf("%s/mylists/v1/lists/%s/parts", baseURL(creds), listID)
	_, err = c.doPostJSON(ctx, addURL, parts, creds, &struct{}{})
	if err != nil {
		return nil, listID, fmt.Errorf("digikey: add parts to list %s: %w", listID, err)
	}

	u := listURL(creds, listID)
	return &distributor.PartsListResult{ListID: listID, ListURL: u}, listID, nil
}

// RequestQuote implements distributor.OrderingClient.
// Creates a quote, adds items, then fetches confirmed pricing.
func (c *Client) RequestQuote(ctx context.Context, quoteName string, items []distributor.OrderItem, rawCreds json.RawMessage) (*distributor.QuoteResult, error) {
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}
	base := baseURL(creds)

	// Create quote.
	var createResp createQuoteResponse
	ok, err := c.doPostJSON(ctx, base+"/quoting/v4/quotes", createQuoteRequest{QuoteName: quoteName}, creds, &createResp)
	if err != nil {
		return nil, fmt.Errorf("digikey: create quote: %w", err)
	}
	if !ok || createResp.Quote.QuoteId == 0 {
		return nil, fmt.Errorf("digikey: create quote returned no ID")
	}
	quoteID := createResp.Quote.QuoteId
	quoteIDStr := strconv.FormatInt(quoteID, 10)

	// Add items to quote.
	products := make([]addProductToQuote, 0, len(items))
	for _, item := range items {
		products = append(products, addProductToQuote{
			ProductNumber:     item.SKU,
			CustomerReference: item.CustomerReference,
			Quantities:        []int32{item.Quantity},
		})
	}
	addURL := fmt.Sprintf("%s/quoting/v4/quotes/%s/details", base, quoteIDStr)
	_, err = c.doPostJSON(ctx, addURL, products, creds, &struct{}{})
	if err != nil {
		return nil, fmt.Errorf("digikey: add products to quote %d: %w", quoteID, err)
	}

	// Fetch confirmed pricing.
	getURL := fmt.Sprintf("%s/quoting/v4/quotes/%s/details", base, quoteIDStr)
	var detailsResp productsFromQuoteResponse
	ok, err = c.doGet(ctx, getURL, creds, &detailsResp)
	if err != nil {
		return nil, fmt.Errorf("digikey: get quote details %d: %w", quoteID, err)
	}
	if !ok {
		return nil, fmt.Errorf("digikey: quote %d not found after creation", quoteID)
	}

	resultItems := make([]distributor.QuoteLineItem, 0, len(detailsResp.Products))
	for _, p := range detailsResp.Products {
		item := distributor.QuoteLineItem{
			SKU:               p.DigikeyProductNumber,
			CustomerReference: p.CustomerReference,
			QuantityAvailable: p.QuantityAvailable,
			IsObsolete:        p.IsObsolete,
			IsDiscontinued:    p.IsDiscontinued,
			ExpirationDate:    p.ExpirationDate,
		}
		if len(p.Quantities) > 0 {
			item.UnitPrice = fmt.Sprintf("%.4f", p.Quantities[0].UnitPrice)
		}
		resultItems = append(resultItems, item)
	}

	return &distributor.QuoteResult{
		QuoteID:   quoteID,
		QuoteName: createResp.Quote.QuoteName,
		Items:     resultItems,
	}, nil
}

// GetOrderStatus implements distributor.OrderingClient.
// Fetches live order status using 2-legged credentials (no 3-legged token needed).
func (c *Client) GetOrderStatus(ctx context.Context, salesOrderID string, rawCreds json.RawMessage) (*distributor.OrderStatusResult, error) {
	var creds Credentials
	if err := json.Unmarshal(rawCreds, &creds); err != nil {
		return nil, fmt.Errorf("digikey: unmarshal credentials: %w", err)
	}

	endpoint := fmt.Sprintf("%s/orderstatus/v4/salesorder/%s", baseURL(creds), url.PathEscape(salesOrderID))
	var resp salesOrderResponse
	ok, err := c.doGet(ctx, endpoint, creds, &resp)
	if err != nil {
		return nil, fmt.Errorf("digikey: get order status: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("digikey: order %s not found", salesOrderID)
	}

	lineItems := make([]distributor.OrderStatusLineItem, 0, len(resp.LineItems))
	for _, li := range resp.LineItems {
		lineItems = append(lineItems, distributor.OrderStatusLineItem{
			SKU:               li.DigiKeyProductNumber,
			QuantityOrdered:   li.QuantityOrdered,
			QuantityShipped:   li.QuantityShipped,
			QuantityBackOrder: li.QuantityBackOrder,
		})
	}

	return &distributor.OrderStatusResult{
		SalesOrderID:  salesOrderID,
		Status:        resp.Status.SalesOrderStatus,
		StatusDetail:  resp.Status.ShortDescription,
		LineItems:     lineItems,
		LastCheckedAt: time.Now(),
	}, nil
}
