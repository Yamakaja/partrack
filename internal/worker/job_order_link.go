package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// orderLinkPayload carries the parameters for a link-existing-order job.
type orderLinkPayload struct {
	SalesOrderID  string `json:"sales_order_id"`
	OrderID       string `json:"order_id"`
	DistributorID string `json:"distributor_id"`
	UserID        string `json:"user_id"`
	// SkipReconcile is set by the LinkCommit handler for Path B (allocation conflicts
	// resolved interactively). Line item qty updates were already done synchronously;
	// this job only needs to enrich extra DigiKey SKUs not in partrack, and skip
	// the LinkOrderExternalReference call (also already done).
	SkipReconcile bool `json:"skip_reconcile"`
}

func (q *Queue) handleOrderLink(job Job) {
	ctx := context.Background()

	raw, _ := json.Marshal(job.Payload)
	var p orderLinkPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		q.publishError(job, "invalid payload: "+err.Error())
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(job.TeamID); err != nil {
		q.publishError(job, "invalid team_id: "+job.TeamID)
		return
	}
	var orderID pgtype.UUID
	if err := orderID.Scan(p.OrderID); err != nil {
		q.publishError(job, "invalid order_id: "+p.OrderID)
		return
	}
	var distributorID pgtype.UUID
	if err := distributorID.Scan(p.DistributorID); err != nil {
		q.publishError(job, "invalid distributor_id: "+p.DistributorID)
		return
	}
	var userID pgtype.UUID
	if err := userID.Scan(p.UserID); err != nil {
		q.publishError(job, "invalid user_id: "+p.UserID)
		return
	}

	queries := dbgen.New(q.db)

	// Get distributor credentials.
	cfg, err := queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        teamID,
		DistributorID: distributorID,
	})
	if err != nil {
		q.publishError(job, "distributor config not found: "+err.Error())
		return
	}
	if cfg.ApiType == nil {
		q.publishError(job, "distributor has no api_type")
		return
	}

	client := q.distributors.Lookup(*cfg.ApiType)
	if client == nil {
		q.publishError(job, "no client for api_type: "+*cfg.ApiType)
		return
	}
	oc, ok := client.(distributor.OrderingClient)
	if !ok {
		q.publishError(job, "distributor does not support ordering API")
		return
	}

	// Fetch authoritative order data from DigiKey.
	orderStatus, err := oc.GetOrderStatus(ctx, p.SalesOrderID, json.RawMessage(cfg.ApiCredentials))
	if err != nil {
		q.publishError(job, "failed to fetch DigiKey order: "+err.Error())
		return
	}

	// Load partrack line items and build a SKU → line item map.
	lineItems, err := queries.GetOrderLineItems(ctx, orderID)
	if err != nil {
		q.publishError(job, "failed to get order line items: "+err.Error())
		return
	}
	skuToItem := make(map[string]dbgen.GetOrderLineItemsRow, len(lineItems))
	for _, li := range lineItems {
		skuToItem[li.Sku] = li
	}

	total := len(orderStatus.LineItems)
	publishProgress := func(status string, processed int, items []UnresolvedImportItem, extraAdded int) {
		skus := make([]string, len(items))
		for i, u := range items {
			skus[i] = u.SKU
		}
		q.publishProgress(job, "order_import_progress", map[string]any{
			"status":            status,
			"job_id":            job.ID,
			"order_id":          p.OrderID,
			"total":             total,
			"processed":         processed,
			"unresolved_skus":   skus,
			"extra_items_added": extraAdded,
		})
	}

	publishProgress("progress", 0, nil, 0)

	var unresolvedItems []UnresolvedImportItem
	extraAdded := 0
	processed := 0
	lastPublish := time.Now()

	for _, dkItem := range orderStatus.LineItems {
		priceStr := fmt.Sprintf("%.4f", dkItem.UnitPrice)
		var priceNum pgtype.Numeric
		_ = priceNum.Scan(priceStr)

		qtyOrdered := dkItem.QuantityOrdered
		qtyShipped := dkItem.QuantityShipped
		qtyBackorder := dkItem.QuantityBackOrder

		if ptItem, matched := skuToItem[dkItem.SKU]; matched {
			if !p.SkipReconcile {
				// Path A: update line item quantities and external data from DigiKey.
				_ = queries.UpdateOrderLineItemExternalData(ctx, dbgen.UpdateOrderLineItemExternalDataParams{
					ID:                     ptItem.ID,
					UnitPriceSnapshot:      priceNum,
					ExternalUnitPrice:      priceNum,
					ExternalQtyOrdered:     &qtyOrdered,
					ExternalQtyShipped:     &qtyShipped,
					ExternalQtyBackordered: &qtyBackorder,
				})
				// Sync quantity_ordered to DigiKey's confirmed value.
				effectiveQty := qtyOrdered
				if effectiveQty < ptItem.QuantityReceived {
					effectiveQty = ptItem.QuantityReceived
				}
				if effectiveQty < 1 {
					effectiveQty = 1
				}
				_ = queries.SetOrderLineItemQuantityOrdered(ctx, dbgen.SetOrderLineItemQuantityOrderedParams{
					QuantityOrdered: effectiveQty,
					ID:              ptItem.ID,
				})
			}
			// Path B (skip_reconcile): qty updates were done synchronously in LinkCommit.
		} else {
			// Extra item from DigiKey not in partrack order — try to enrich and add.
			var listingID pgtype.UUID
			var partID pgtype.UUID

			if row, err := queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
				Sku:           dkItem.SKU,
				DistributorID: distributorID,
			}); err == nil {
				listingID = row.ID
				partID = row.PartID
			}

			if !listingID.Valid {
				enrichResult, enrichErr := client.EnrichPart(ctx, dkItem.SKU, json.RawMessage(cfg.ApiCredentials))
				if enrichErr == nil && enrichResult != nil {
					mpn := dkItem.MPN
					if pid, err := ApplyEnrichmentResult(ctx, q.db, queries, teamID, distributorID, enrichResult, &mpn, nil, q.mfr); err == nil && pid.Valid {
						if row, err := queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
							Sku:           dkItem.SKU,
							DistributorID: distributorID,
						}); err == nil {
							listingID = row.ID
							partID = row.PartID
						}
					}
				}

				if !listingID.Valid {
					reason := "listing_not_found"
					if enrichErr != nil {
						reason = "enrichment_failed"
					} else if enrichResult == nil {
						reason = "enrichment_nil"
					}
					unresolvedItems = append(unresolvedItems, UnresolvedImportItem{
						SKU:               dkItem.SKU,
						MPN:               dkItem.MPN,
						UnitPrice:         dkItem.UnitPrice,
						QuantityOrdered:   dkItem.QuantityOrdered,
						QuantityShipped:   dkItem.QuantityShipped,
						QuantityBackorder: dkItem.QuantityBackOrder,
						FailureReason:     reason,
					})
				}
			}

			if listingID.Valid {
				if _, err := queries.CreateOrderLineItemForImport(ctx, dbgen.CreateOrderLineItemForImportParams{
					OrderID:                orderID,
					ListingID:              listingID,
					QuantityOrdered:        qtyOrdered,
					UnitPriceSnapshot:      priceNum,
					ExternalUnitPrice:      priceNum,
					ExternalQtyOrdered:     &qtyOrdered,
					ExternalQtyShipped:     &qtyShipped,
					ExternalQtyBackordered: &qtyBackorder,
				}); err == nil {
					extraAdded++
					// Ensure inventory entry exists.
					if _, err := queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
						TeamID: teamID,
						PartID: partID,
					}); err == pgx.ErrNoRows {
						_, _ = queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
							TeamID: teamID,
							PartID: partID,
						})
					}
				}
			}
		}

		processed++
		if time.Since(lastPublish) >= 100*time.Millisecond {
			publishProgress("progress", processed, unresolvedItems, extraAdded)
			lastPublish = time.Now()
		}
	}

	if !p.SkipReconcile {
		// Path A: transition order to placed and set purchase order number.
		if _, err := queries.LinkOrderExternalReference(ctx, dbgen.LinkOrderExternalReferenceParams{
			ExternalReference: &p.SalesOrderID,
			ID:                orderID,
			TeamID:            teamID,
		}); err != nil {
			q.publishError(job, "failed to link external reference: "+err.Error())
			return
		}
		if orderStatus.PurchaseOrder != "" {
			_, _ = queries.UpdateOrderPurchaseOrderNumber(ctx, dbgen.UpdateOrderPurchaseOrderNumberParams{
				PurchaseOrderNumber: &orderStatus.PurchaseOrder,
				ID:                  orderID,
				TeamID:              teamID,
			})
		}
	} else {
		// Path B: order was already linked synchronously in LinkCommit.
		// Still update purchase order number if DigiKey provided one.
		if orderStatus.PurchaseOrder != "" {
			_, _ = queries.UpdateOrderPurchaseOrderNumber(ctx, dbgen.UpdateOrderPurchaseOrderNumberParams{
				PurchaseOrderNumber: &orderStatus.PurchaseOrder,
				ID:                  orderID,
				TeamID:              teamID,
			})
		}
	}

	// Persist unresolved items so the user can retry later.
	if len(unresolvedItems) > 0 {
		itemsJSON, _ := json.Marshal(unresolvedItems)
		_ = queries.SetOrderUnresolvedItems(ctx, dbgen.SetOrderUnresolvedItemsParams{
			ID:              orderID,
			TeamID:          teamID,
			UnresolvedItems: itemsJSON,
		})
	}

	skus := make([]string, len(unresolvedItems))
	for i, u := range unresolvedItems {
		skus[i] = u.SKU
	}
	q.publishProgress(job, "order_import_progress", map[string]any{
		"status":            "complete",
		"job_id":            job.ID,
		"order_id":          p.OrderID,
		"total":             total,
		"processed":         processed,
		"unresolved_skus":   skus,
		"extra_items_added": extraAdded,
	})

	orderData, _ := json.Marshal(map[string]string{"order_id": p.OrderID})
	q.Publish(Event{Type: "order_changed", TeamID: job.TeamID, UserID: p.UserID, Data: orderData})

	_ = userID // user_id not stored on orders but kept for publish
}
