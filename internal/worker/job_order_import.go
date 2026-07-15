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

// orderImportPayload carries the parameters for a new-order-from-DigiKey job.
type orderImportPayload struct {
	SalesOrderID  string `json:"sales_order_id"`
	DistributorID string `json:"distributor_id"`
	UserID        string `json:"user_id"`
}

func (q *Queue) handleOrderImport(job Job) {
	ctx := context.Background()

	raw, _ := json.Marshal(job.Payload)
	var p orderImportPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		q.publishError(job, "invalid payload: "+err.Error())
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(job.TeamID); err != nil {
		q.publishError(job, "invalid team_id: "+job.TeamID)
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

	// Duplicate check.
	if _, err := queries.GetOrderByExternalReference(ctx, dbgen.GetOrderByExternalReferenceParams{
		TeamID:            teamID,
		ExternalReference: &p.SalesOrderID,
	}); err == nil {
		q.publishError(job, "order already imported: "+p.SalesOrderID)
		return
	}

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

	orderStatus, err := oc.GetOrderStatus(ctx, p.SalesOrderID, json.RawMessage(cfg.ApiCredentials))
	if err != nil {
		q.publishError(job, "failed to fetch DigiKey order: "+err.Error())
		return
	}

	total := len(orderStatus.LineItems)

	publishProgress := func(status string, resolved int, items []UnresolvedImportItem) {
		skus := make([]string, len(items))
		for i, u := range items {
			skus[i] = u.SKU
		}
		q.publishProgress(job, "order_import_progress", map[string]any{
			"status":          status,
			"job_id":          job.ID,
			"total":           total,
			"resolved":        resolved,
			"unresolved_skus": skus,
		})
	}

	publishProgress("progress", 0, nil)

	// Resolve each DigiKey line item to a listing.
	//
	// DigiKey can split a single ordered part across multiple line items — e.g.
	// when one reel runs out mid-pick and the remainder ships from a fresh reel,
	// often at a different (worse) quantity-break price. Those split lines carry
	// the same SKU and therefore resolve to the same listing, so we must merge
	// them into one order line item: the (order_id, listing_id) uniqueness
	// constraint allows only one row per listing. We sum the quantities and
	// accumulate the extended price so the blended unit price preserves total cost.
	type resolvedItem struct {
		listingID    pgtype.UUID
		partID       pgtype.UUID
		extPrice     float64 // Σ unitPrice·qtyOrdered across merged DigiKey lines
		qtyOrdered   int32
		qtyShipped   int32
		qtyBackorder int32
	}

	var resolved []*resolvedItem
	byListing := make(map[[16]byte]*resolvedItem)
	resolvedCount := 0 // DigiKey line items resolved (pre-merge), for progress reporting
	var unresolvedItems []UnresolvedImportItem
	lastPublish := time.Now()

	for _, li := range orderStatus.LineItems {
		var listingID pgtype.UUID
		var partID pgtype.UUID

		// Try direct lookup.
		if row, err := queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
			Sku:           li.SKU,
			DistributorID: distributorID,
		}); err == nil {
			listingID = row.ID
			partID = row.PartID
		}

		// Enrich if not found — capture reason separately.
		if !listingID.Valid {
			enrichResult, enrichErr := client.EnrichPart(ctx, li.SKU, json.RawMessage(cfg.ApiCredentials))
			if enrichErr == nil && enrichResult != nil {
				mpn := li.MPN
				if pid, err := ApplyEnrichmentResult(ctx, q.db, queries, teamID, distributorID, enrichResult, &mpn, nil, q.mfr); err == nil && pid.Valid {
					if row, err := queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
						Sku:           li.SKU,
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
					SKU:               li.SKU,
					MPN:               li.MPN,
					UnitPrice:         li.UnitPrice,
					QuantityOrdered:   li.QuantityOrdered,
					QuantityShipped:   li.QuantityShipped,
					QuantityBackorder: li.QuantityBackOrder,
					FailureReason:     reason,
				})
			}
		}

		if listingID.Valid {
			resolvedCount++
			if existing, ok := byListing[listingID.Bytes]; ok {
				// Split line for a listing we've already seen — accumulate.
				existing.extPrice += li.UnitPrice * float64(li.QuantityOrdered)
				existing.qtyOrdered += li.QuantityOrdered
				existing.qtyShipped += li.QuantityShipped
				existing.qtyBackorder += li.QuantityBackOrder
			} else {
				item := &resolvedItem{
					listingID:    listingID,
					partID:       partID,
					extPrice:     li.UnitPrice * float64(li.QuantityOrdered),
					qtyOrdered:   li.QuantityOrdered,
					qtyShipped:   li.QuantityShipped,
					qtyBackorder: li.QuantityBackOrder,
				}
				byListing[listingID.Bytes] = item
				resolved = append(resolved, item)
			}
		}

		if time.Since(lastPublish) >= 100*time.Millisecond {
			publishProgress("progress", resolvedCount, unresolvedItems)
			lastPublish = time.Now()
		}
	}

	// Create order in a transaction.
	tx, err := q.db.Begin(ctx)
	if err != nil {
		q.publishError(job, "failed to begin transaction: "+err.Error())
		return
	}
	qtx := queries.WithTx(tx)

	order, err := qtx.CreateOrder(ctx, dbgen.CreateOrderParams{
		TeamID:        teamID,
		DistributorID: distributorID,
		CreatedBy:     userID,
	})
	if err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		q.publishError(job, "failed to create order: "+err.Error())
		return
	}

	for _, item := range resolved {
		// Blended unit price = total extended price / total quantity, so merged
		// split lines with differing break prices report the correct total cost.
		unitPrice := item.extPrice
		if item.qtyOrdered != 0 {
			unitPrice = item.extPrice / float64(item.qtyOrdered)
		}
		priceStr := fmt.Sprintf("%.4f", unitPrice)
		var priceNum pgtype.Numeric
		if err := priceNum.Scan(priceStr); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			q.publishError(job, "invalid unit price: "+priceStr)
			return
		}
		if _, err := qtx.CreateOrderLineItemForImport(ctx, dbgen.CreateOrderLineItemForImportParams{
			OrderID:                order.ID,
			ListingID:              item.listingID,
			QuantityOrdered:        item.qtyOrdered,
			UnitPriceSnapshot:      priceNum,
			ExternalUnitPrice:      priceNum,
			ExternalQtyOrdered:     &item.qtyOrdered,
			ExternalQtyShipped:     &item.qtyShipped,
			ExternalQtyBackordered: &item.qtyBackorder,
		}); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			q.publishError(job, "failed to create line item: "+err.Error())
			return
		}
	}

	if _, err := qtx.LinkOrderExternalReference(ctx, dbgen.LinkOrderExternalReferenceParams{
		ExternalReference: &p.SalesOrderID,
		ID:                order.ID,
		TeamID:            teamID,
	}); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		q.publishError(job, "failed to link external reference: "+err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		q.publishError(job, "failed to commit: "+err.Error())
		return
	}

	// Set purchase order number if DigiKey provided one.
	if orderStatus.PurchaseOrder != "" {
		_, _ = queries.UpdateOrderPurchaseOrderNumber(ctx, dbgen.UpdateOrderPurchaseOrderNumberParams{
			PurchaseOrderNumber: &orderStatus.PurchaseOrder,
			ID:                  order.ID,
			TeamID:              teamID,
		})
	}

	// Ensure inventory entries exist for all resolved parts.
	for _, item := range resolved {
		if _, err := queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
			TeamID: teamID,
			PartID: item.partID,
		}); err == pgx.ErrNoRows {
			_, _ = queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
				TeamID: teamID,
				PartID: item.partID,
			})
		}
	}

	// Persist unresolved items so the user can retry later.
	if len(unresolvedItems) > 0 {
		itemsJSON, _ := json.Marshal(unresolvedItems)
		_ = queries.SetOrderUnresolvedItems(ctx, dbgen.SetOrderUnresolvedItemsParams{
			ID:              order.ID,
			TeamID:          teamID,
			UnresolvedItems: itemsJSON,
		})
	}

	skus := make([]string, len(unresolvedItems))
	for i, u := range unresolvedItems {
		skus[i] = u.SKU
	}
	q.publishProgress(job, "order_import_progress", map[string]any{
		"status":          "complete",
		"job_id":          job.ID,
		"order_id":        order.ID.String(),
		"total":           total,
		"resolved":        resolvedCount,
		"unresolved_skus": skus,
	})

	orderData, _ := json.Marshal(map[string]string{"order_id": order.ID.String()})
	q.Publish(Event{Type: "order_changed", TeamID: job.TeamID, UserID: p.UserID, Data: orderData})
}
