package mouser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
)

// ---- distributor.OrderingClient implementation ----

// CreateList implements distributor.OrderingClient by inserting items into the Mouser
// shopping cart for the account associated with the API key.
// listName is accepted but not forwarded to Mouser (no named-list concept).
// Returns (result, cartKey, err); cartKey is populated even on partial failure.
func (c *Client) CreateList(ctx context.Context, listName string, items []distributor.OrderItem, rawCreds json.RawMessage) (*distributor.PartsListResult, string, error) {
	creds, err := parseCredentials(rawCreds)
	if err != nil {
		return nil, "", err
	}

	cartItems := make([]cartItemRequest, 0, len(items))
	for _, item := range items {
		cartItems = append(cartItems, cartItemRequest{
			MouserPartNumber:   item.SKU,
			Quantity:           item.Quantity,
			CustomerPartNumber: item.CustomerReference,
		})
	}

	var resp cartResponseRoot
	if err := c.doPost(ctx, "/api/v1/cart/items/insert", creds.APIKey, cartItemRequestRoot{
		CartItems: cartItems,
	}, &resp); err != nil {
		return nil, "", fmt.Errorf("mouser: insert cart items: %w", err)
	}
	if err := checkErrors(resp.Errors); err != nil {
		return nil, resp.CartKey, err
	}

	result := &distributor.PartsListResult{
		ListID:  resp.CartKey,
		ListURL: "https://www.mouser.com/store/cart",
	}
	return result, resp.CartKey, nil
}

// RequestQuote implements distributor.OrderingClient.
// Mouser does not expose a quote API; use CreateList to push items to the cart instead.
func (c *Client) RequestQuote(_ context.Context, _ string, _ []distributor.OrderItem, _ json.RawMessage) (*distributor.QuoteResult, error) {
	return nil, fmt.Errorf("mouser: quote API not supported; use CreateList to push items to cart")
}

// GetOrderStatus implements distributor.OrderingClient.
// Fetches order details using the Mouser order history API by sales order number.
func (c *Client) GetOrderStatus(ctx context.Context, salesOrderID string, rawCreds json.RawMessage) (*distributor.OrderStatusResult, error) {
	creds, err := parseCredentials(rawCreds)
	if err != nil {
		return nil, err
	}

	path := "/api/v1/orderhistory/salesOrderNumber?salesOrderNumber=" + url.QueryEscape(salesOrderID)
	var resp orderDetail
	if err := c.doGet(ctx, path, creds.APIKey, &resp); err != nil {
		return nil, fmt.Errorf("mouser: get order status: %w", err)
	}
	if err := checkErrors(resp.Errors); err != nil {
		return nil, err
	}
	if resp.SalesOrderId == "" {
		return nil, fmt.Errorf("mouser: order %s not found", salesOrderID)
	}

	dateEntered, _ := parseOrderDate(resp.OrderDate)
	totalPrice := parseOrderTotal(resp.SummaryDetail.OrderTotal)

	lineItems := make([]distributor.OrderStatusLineItem, 0, len(resp.OrderLines))
	for _, li := range resp.OrderLines {
		shipments := make([]distributor.OrderStatusShipment, 0, len(li.Activities))
		for _, act := range li.Activities {
			s := distributor.OrderStatusShipment{
				InvoiceID: act.InvoiceNumber,
			}
			if t, err := parseOrderDate(act.Date); err == nil {
				s.ShippedDate = &t
			}
			shipments = append(shipments, s)
		}
		lineItems = append(lineItems, distributor.OrderStatusLineItem{
			SKU:             li.ProductInfo.MouserPartNumber,
			MPN:             li.ProductInfo.ManufacturerPartNumber,
			UnitPrice:       li.UnitPrice,
			QuantityOrdered: li.Quantity,
			// QuantityShipped and QuantityBackOrder are not available per-line
			// from the Mouser order history endpoint.
			Shipments: shipments,
		})
	}

	result := &distributor.OrderStatusResult{
		SalesOrderID:  resp.SalesOrderId,
		Status:        resp.OrderStatusName,
		PurchaseOrder: resp.PaymentDetail.PoNumber,
		DateEntered:   dateEntered,
		LineItems:     lineItems,
		LastCheckedAt: time.Now(),
		OrderNumber:   resp.WebOrderId,
		Currency:      resp.CurrencyCode,
		TotalPrice:    totalPrice,
		ShipMethod:    resp.DeliveryDetail.ShippingMethodName,
	}

	addr := resp.ShippingAddress
	if addr.AddressOne != "" || addr.City != "" {
		result.ShippingAddress = &distributor.OrderStatusAddress{
			Line1:       addr.AddressOne,
			Line2:       addr.AddressTwo,
			City:        addr.City,
			State:       addr.StateOrProvince,
			ZipCode:     addr.PostalCode,
			CompanyName: addr.CompanyName,
			IsoCode:     addr.CountryCode,
		}
	}

	return result, nil
}

// ---- helpers ----

// orderDateFormats lists candidate date formats Mouser may use in its API responses.
var orderDateFormats = []string{
	"1/2/2006 3:04:05 PM",
	"1/2/2006 15:04:05",
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// parseOrderDate attempts to parse a Mouser date string using known formats.
func parseOrderDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	for _, layout := range orderDateFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("mouser: unrecognised date format: %q", s)
}

// parseOrderTotal parses the Mouser order total string (which may include currency
// symbols or locale formatting) and returns a float64.
func parseOrderTotal(s string) float64 {
	parsed := parseMouserPrice(s)
	if parsed == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(parsed, "%f", &f)
	return f
}
