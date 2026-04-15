package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Yamakaja/partrack/internal/distributor"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListingRefreshPayload is the job payload for JobListingRefresh.
// Set ListingID for a single-listing refresh (existing behaviour).
// Set ListingIDs for a batch refresh (e.g. cart refresh); ListingID takes precedence.
type ListingRefreshPayload struct {
	ListingID  string   // single listing
	ListingIDs []string // batch mode; used when ListingID is empty
	PartID     string   // used for part_changed event in single mode
	UserID     string
}

// listingRefreshGroup holds a set of listings that share the same distributor and part,
// allowing a single API call to cover all of them.
type listingRefreshGroup struct {
	apiType       string
	distributorID pgtype.UUID
	rows          []dbgen.GetListingsForRefreshBatchRow
}

func (q *Queue) handleListingRefresh(job Job) {
	ctx := context.Background()

	payload, ok := job.Payload.(ListingRefreshPayload)
	if !ok {
		q.publishError(job, "invalid payload type")
		return
	}

	queries := dbgen.New(q.db)

	// Fetch all distributor credentials for this team once upfront.
	credsByDist := map[pgtype.UUID]json.RawMessage{}
	if job.TeamID != "" {
		var teamID pgtype.UUID
		if err := teamID.Scan(job.TeamID); err == nil {
			configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
			if err == nil {
				for _, cfg := range configs {
					credsByDist[cfg.DistributorID] = json.RawMessage(cfg.ApiCredentials)
				}
			}
		}
	}

	if payload.ListingID != "" {
		// Single-listing mode: call API, find matching SKU in result, update that one listing.
		var listingID pgtype.UUID
		if err := listingID.Scan(payload.ListingID); err != nil {
			q.publishError(job, "invalid listing_id: "+payload.ListingID)
			return
		}
		listing, err := queries.GetListingForRefresh(ctx, listingID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				q.publishError(job, fmt.Sprintf("listing %s not found", payload.ListingID))
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
		results, err := client.RefreshListing(ctx, listing.Sku, credsByDist[listing.DistributorID])
		if err != nil {
			q.publishError(job, "refresh failed: "+err.Error())
			return
		}
		// Find the entry matching this specific SKU.
		var matched *distributor.Listing
		for i := range results {
			if results[i].SKU == listing.Sku {
				matched = &results[i]
				break
			}
		}
		if matched == nil {
			q.publishProgress(job, "listing_refresh_done", map[string]any{
				"status":     "unavailable",
				"listing_id": payload.ListingID,
			})
			return
		}
		if err := q.updateListingFromResult(ctx, queries, listingID, payload.ListingID, matched, job); err != nil {
			return
		}
		data, _ := json.Marshal(map[string]string{"part_id": payload.PartID})
		q.Publish(Event{Type: "part_changed", TeamID: job.TeamID, Data: data})
		return
	}

	// Batch mode: fetch all listing rows, group by (distributor_id, part_id),
	// make one API call per group, update all listings whose SKU appears in the response.

	// Parse all IDs upfront.
	ids := make([]pgtype.UUID, 0, len(payload.ListingIDs))
	for _, idStr := range payload.ListingIDs {
		var id pgtype.UUID
		if err := id.Scan(idStr); err != nil {
			q.publishProgress(job, "listing_refresh_done", map[string]any{
				"status":     "error",
				"listing_id": idStr,
				"error":      "invalid listing_id",
			})
			continue
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		data, _ := json.Marshal(map[string]string{})
		q.Publish(Event{Type: "cart_changed", TeamID: job.TeamID, Data: data})
		return
	}

	rows, err := queries.GetListingsForRefreshBatch(ctx, ids)
	if err != nil {
		q.publishError(job, "failed to fetch listings: "+err.Error())
		return
	}

	// Group by (distributor_id, part_id).
	type groupKey struct {
		distributorID pgtype.UUID
		partID        pgtype.UUID
	}
	groupMap := map[groupKey]*listingRefreshGroup{}
	groupOrder := []groupKey{} // preserve deterministic order
	for _, row := range rows {
		if row.ApiType == nil {
			q.publishProgress(job, "listing_refresh_done", map[string]any{
				"status":     "error",
				"listing_id": row.ID.String(),
				"error":      "distributor has no API integration",
			})
			continue
		}
		key := groupKey{distributorID: row.DistributorID, partID: row.PartID}
		if _, exists := groupMap[key]; !exists {
			groupMap[key] = &listingRefreshGroup{
				apiType:       *row.ApiType,
				distributorID: row.DistributorID,
			}
			groupOrder = append(groupOrder, key)
		}
		groupMap[key].rows = append(groupMap[key].rows, row)
	}

	// Process each group with a single API call.
	for _, key := range groupOrder {
		grp := groupMap[key]
		client := q.distributors.Lookup(grp.apiType)
		if client == nil {
			for _, row := range grp.rows {
				q.publishProgress(job, "listing_refresh_done", map[string]any{
					"status":     "error",
					"listing_id": row.ID.String(),
					"error":      fmt.Sprintf("no client registered for api_type %s", grp.apiType),
				})
			}
			continue
		}

		// One API call for any representative SKU — the response covers all variants.
		representativeSKU := grp.rows[0].Sku
		results, err := client.RefreshListing(ctx, representativeSKU, credsByDist[grp.distributorID])
		if err != nil {
			for _, row := range grp.rows {
				q.publishProgress(job, "listing_refresh_done", map[string]any{
					"status":     "error",
					"listing_id": row.ID.String(),
					"error":      "refresh failed: " + err.Error(),
				})
			}
			continue
		}

		// Build a sku→listing map from the response.
		resultBySKU := make(map[string]*distributor.Listing, len(results))
		for i := range results {
			resultBySKU[results[i].SKU] = &results[i]
		}

		// Update each listing in the group.
		for _, row := range grp.rows {
			matched := resultBySKU[row.Sku]
			if matched == nil {
				q.publishProgress(job, "listing_refresh_done", map[string]any{
					"status":     "unavailable",
					"listing_id": row.ID.String(),
				})
				continue
			}
			_ = q.updateListingFromResult(ctx, queries, row.ID, row.ID.String(), matched, job)
		}
	}

	// Publish part_changed for each unique part so other users' views refresh.
	seen := map[string]bool{}
	for _, key := range groupOrder {
		partIDStr := key.partID.String()
		if seen[partIDStr] {
			continue
		}
		seen[partIDStr] = true
		partData, _ := json.Marshal(map[string]string{"part_id": partIDStr})
		q.Publish(Event{Type: "part_changed", TeamID: job.TeamID, UserID: payload.UserID, Data: partData})
	}

	data, _ := json.Marshal(map[string]string{})
	q.Publish(Event{Type: "cart_changed", TeamID: job.TeamID, Data: data})
}

// updateListingFromResult writes refreshed price/stock data for one listing and
// publishes a listing_refresh_done progress event. Returns non-nil on DB error.
func (q *Queue) updateListingFromResult(
	ctx context.Context,
	queries *dbgen.Queries,
	listingID pgtype.UUID,
	listingIDStr string,
	result *distributor.Listing,
	job Job,
) error {
	unitPrice, err := parseNumeric(result.UnitPrice)
	if err != nil {
		q.publishProgress(job, "listing_refresh_done", map[string]any{
			"status":     "error",
			"listing_id": listingIDStr,
			"error":      "invalid unit_price from client: " + result.UnitPrice,
		})
		return err
	}

	priceBreaksJSON, _ := json.Marshal(result.PriceBreaks)
	if err := queries.UpdateListingData(ctx, dbgen.UpdateListingDataParams{
		ID:             listingID,
		UnitPrice:      unitPrice,
		StockAvailable: result.StockAvailable,
		PriceBreaks:    priceBreaksJSON,
	}); err != nil {
		q.publishProgress(job, "listing_refresh_done", map[string]any{
			"status":     "error",
			"listing_id": listingIDStr,
			"error":      "failed to update listing: " + err.Error(),
		})
		return err
	}

	q.publishProgress(job, "listing_refresh_done", map[string]any{
		"status":          "done",
		"listing_id":      listingIDStr,
		"stock_available": result.StockAvailable,
		"unit_price":      result.UnitPrice,
	})
	return nil
}
