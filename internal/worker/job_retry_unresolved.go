package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// retryUnresolvedPayload carries the parameters for a retry-unresolved job.
type retryUnresolvedPayload struct {
	OrderID string `json:"order_id"`
	UserID  string `json:"user_id"`
}

func (q *Queue) handleRetryUnresolved(job Job) {
	ctx := context.Background()

	raw, _ := json.Marshal(job.Payload)
	var p retryUnresolvedPayload
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

	queries := dbgen.New(q.db)

	// Load order to get distributor ID and integrations.
	order, err := queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: teamID})
	if err != nil {
		q.publishError(job, "order not found: "+err.Error())
		return
	}

	// Extract unresolved items from integrations JSONB.
	unresolvedItems, err := extractUnresolvedItems(order.Integrations)
	if err != nil {
		q.publishError(job, "failed to parse unresolved items: "+err.Error())
		return
	}
	if len(unresolvedItems) == 0 {
		q.publishProgress(job, "order_import_progress", map[string]any{
			"status":          "complete",
			"job_id":          job.ID,
			"order_id":        p.OrderID,
			"total":           0,
			"resolved":        0,
			"unresolved_skus": []string{},
		})
		return
	}

	// Load distributor credentials.
	cfg, err := queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        teamID,
		DistributorID: order.DistributorID,
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

	total := len(unresolvedItems)
	attempted := 0
	resolvedCount := 0
	var remaining []UnresolvedImportItem
	lastPublish := time.Now()

	publishProgress := func(status string) {
		skus := make([]string, len(remaining))
		for i, u := range remaining {
			skus[i] = u.SKU
		}
		q.publishProgress(job, "order_import_progress", map[string]any{
			"status":          status,
			"job_id":          job.ID,
			"order_id":        p.OrderID,
			"total":           total,
			"attempted":       attempted,
			"resolved":        resolvedCount,
			"unresolved_skus": skus,
		})
	}

	publishProgress("progress")

	for _, item := range unresolvedItems {
		var listingID pgtype.UUID
		var partID pgtype.UUID

		// Try direct lookup first.
		if row, err := queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
			Sku:           item.SKU,
			DistributorID: order.DistributorID,
		}); err == nil {
			listingID = row.ID
			partID = row.PartID
		}

		// Attempt enrichment if not found.
		if !listingID.Valid {
			enrichResult, enrichErr := client.EnrichPart(ctx, item.SKU, json.RawMessage(cfg.ApiCredentials))
			if enrichErr == nil && enrichResult != nil {
				mpn := item.MPN
				if pid, err := ApplyEnrichmentResult(ctx, q.db, queries, teamID, order.DistributorID, enrichResult, &mpn, nil, q.mfr); err == nil && pid.Valid {
					if row, err := queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
						Sku:           item.SKU,
						DistributorID: order.DistributorID,
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
				remaining = append(remaining, UnresolvedImportItem{
					SKU:               item.SKU,
					MPN:               item.MPN,
					UnitPrice:         item.UnitPrice,
					QuantityOrdered:   item.QuantityOrdered,
					QuantityShipped:   item.QuantityShipped,
					QuantityBackorder: item.QuantityBackorder,
					FailureReason:     reason,
				})
				attempted++
				if time.Since(lastPublish) >= 100*time.Millisecond {
					publishProgress("progress")
					lastPublish = time.Now()
				}
				continue
			}
		}

		// Create line item using stored price/qty. Treat unique constraint conflict as success.
		priceStr := fmt.Sprintf("%.4f", item.UnitPrice)
		var priceNum pgtype.Numeric
		_ = priceNum.Scan(priceStr)

		qtyOrdered := item.QuantityOrdered
		qtyShipped := item.QuantityShipped
		qtyBackorder := item.QuantityBackorder

		_, createErr := queries.CreateOrderLineItemForImport(ctx, dbgen.CreateOrderLineItemForImportParams{
			OrderID:                orderID,
			ListingID:              listingID,
			QuantityOrdered:        qtyOrdered,
			UnitPriceSnapshot:      priceNum,
			ExternalUnitPrice:      priceNum,
			ExternalQtyOrdered:     &qtyOrdered,
			ExternalQtyShipped:     &qtyShipped,
			ExternalQtyBackordered: &qtyBackorder,
		})
		if createErr != nil {
			var pgErr *pgconn.PgError
			if errors.As(createErr, &pgErr) && pgErr.Code == "23505" {
				// Unique constraint violation: listing already exists on this order.
				// A concurrent retry resolved it first — count as success.
			} else {
				remaining = append(remaining, UnresolvedImportItem{
					SKU:               item.SKU,
					MPN:               item.MPN,
					UnitPrice:         item.UnitPrice,
					QuantityOrdered:   item.QuantityOrdered,
					QuantityShipped:   item.QuantityShipped,
					QuantityBackorder: item.QuantityBackorder,
					FailureReason:     "enrichment_failed",
				})
				attempted++
				if time.Since(lastPublish) >= 100*time.Millisecond {
					publishProgress("progress")
					lastPublish = time.Now()
				}
				continue
			}
		}

		// Ensure inventory entry exists for the resolved part.
		if _, err := queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
			TeamID: teamID,
			PartID: partID,
		}); err == pgx.ErrNoRows {
			_, _ = queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
				TeamID: teamID,
				PartID: partID,
			})
		}

		resolvedCount++
		attempted++
		if time.Since(lastPublish) >= 100*time.Millisecond {
			publishProgress("progress")
			lastPublish = time.Now()
		}
	}

	// Persist remaining unresolved items (empty array clears the field).
	itemsJSON := []byte("[]")
	if len(remaining) > 0 {
		itemsJSON, _ = json.Marshal(remaining)
	}
	_ = queries.SetOrderUnresolvedItems(ctx, dbgen.SetOrderUnresolvedItemsParams{
		ID:              orderID,
		TeamID:          teamID,
		UnresolvedItems: itemsJSON,
	})

	publishProgress("complete")

	orderData, _ := json.Marshal(map[string]string{"order_id": p.OrderID})
	q.Publish(Event{Type: "order_changed", TeamID: job.TeamID, UserID: p.UserID, Data: orderData})
}

// extractUnresolvedItems parses orders.integrations JSONB and returns the
// digikey.unresolved_items array, or nil if absent.
func extractUnresolvedItems(integrations []byte) ([]UnresolvedImportItem, error) {
	if len(integrations) == 0 {
		return nil, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(integrations, &raw); err != nil {
		return nil, err
	}
	dkRaw, ok := raw["digikey"]
	if !ok {
		return nil, nil
	}
	var dk map[string]json.RawMessage
	if err := json.Unmarshal(dkRaw, &dk); err != nil {
		return nil, err
	}
	itemsRaw, ok := dk["unresolved_items"]
	if !ok {
		return nil, nil
	}
	var items []UnresolvedImportItem
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		return nil, err
	}
	return items, nil
}
