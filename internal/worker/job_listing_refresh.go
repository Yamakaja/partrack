package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (q *Queue) handleListingRefresh(job Job) {
	ctx := context.Background()

	payload, ok := job.Payload.(map[string]string)
	if !ok {
		q.publishError(job, "invalid payload type")
		return
	}
	listingIDStr, ok := payload["listing_id"]
	if !ok {
		q.publishError(job, "missing listing_id in payload")
		return
	}
	var listingID pgtype.UUID
	if err := listingID.Scan(listingIDStr); err != nil {
		q.publishError(job, "invalid listing_id: "+listingIDStr)
		return
	}

	queries := dbgen.New(q.db)

	listing, err := queries.GetListingForRefresh(ctx, listingID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			q.publishError(job, fmt.Sprintf("listing %s not found", listingIDStr))
		} else {
			q.publishError(job, "failed to get listing: "+err.Error())
		}
		return
	}

	if listing.ApiType == nil {
		q.publishError(job, "distributor has no API integration")
		return
	}

	client := q.distributors.Lookup(*listing.ApiType)
	if client == nil {
		q.publishError(job, fmt.Sprintf("no client registered for api_type %s", *listing.ApiType))
		return
	}

	// Fetch credentials for this distributor+team if available.
	var credentials json.RawMessage
	if job.TeamID != "" {
		var teamID pgtype.UUID
		if err := teamID.Scan(job.TeamID); err == nil {
			configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
			if err == nil {
				for _, cfg := range configs {
					if cfg.DistributorID == listing.DistributorID {
						credentials = json.RawMessage(cfg.ApiCredentials)
						break
					}
				}
			}
		}
	}

	result, err := client.RefreshListing(ctx, listing.Sku, credentials)
	if err != nil {
		q.publishError(job, "refresh failed: "+err.Error())
		return
	}
	if result == nil {
		q.publishProgress(job, "listing_refresh_done", map[string]any{
			"status":     "unavailable",
			"listing_id": listingIDStr,
		})
		return
	}

	unitPrice, err := parseNumeric(result.UnitPrice)
	if err != nil {
		q.publishError(job, "invalid unit_price from client: "+result.UnitPrice)
		return
	}

	priceBreaksJSON, _ := json.Marshal(result.PriceBreaks)
	if err := queries.UpdateListingData(ctx, dbgen.UpdateListingDataParams{
		ID:             listingID,
		UnitPrice:      unitPrice,
		StockAvailable: result.StockAvailable,
		PriceBreaks:    priceBreaksJSON,
	}); err != nil {
		q.publishError(job, "failed to update listing: "+err.Error())
		return
	}

	q.publishProgress(job, "listing_refresh_done", map[string]any{
		"status":          "done",
		"listing_id":      listingIDStr,
		"stock_available": result.StockAvailable,
		"unit_price":      result.UnitPrice,
	})

	data, _ := json.Marshal(map[string]string{"part_id": payload["part_id"]})
	q.Publish(Event{Type: "part_changed", TeamID: job.TeamID, Data: data})
}
