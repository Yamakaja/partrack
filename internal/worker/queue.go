// Package worker provides a channel-based background job queue and a
// per-team SSE (Server-Sent Events) hub for pushing job progress to
// connected frontend clients.
package worker

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/Yamakaja/partrack/internal/distributor"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobType identifies the kind of background work to perform.
type JobType string

const (
	JobBOMImport      JobType = "bom_import"
	JobPartEnrich     JobType = "part_enrich"
	JobListingRefresh JobType = "listing_refresh"
)

// Job is a unit of work submitted to the Queue.
type Job struct {
	ID      string
	Type    JobType
	TeamID  string
	Payload any
}

// Event is a progress notification sent to SSE subscribers.
type Event struct {
	Type   string          `json:"event"`
	Data   json.RawMessage `json:"data"`
	JobID  string          `json:"job_id,omitempty"`
	TeamID string          `json:"-"` // routing only; not serialised
}

// Queue is a channel-based job dispatcher with an integrated SSE hub.
// Jobs are processed by a pool of worker goroutines. Progress events are
// fanned out to all active SSE subscribers for the relevant team.
type Queue struct {
	jobs         chan Job
	subscribers  map[string][]chan Event // keyed by team ID
	mu           sync.RWMutex
	db           *pgxpool.Pool
	distributors distributor.Registry
}

// New creates a Queue and starts n worker goroutines.
func New(workers int, db *pgxpool.Pool, reg distributor.Registry) *Queue {
	q := &Queue{
		jobs:         make(chan Job, 64),
		subscribers:  make(map[string][]chan Event),
		db:           db,
		distributors: reg,
	}
	for i := 0; i < workers; i++ {
		go q.run()
	}
	return q
}

// newUUID returns a random UUID v4 string.
func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Enqueue submits a job and returns its ID.
func (q *Queue) Enqueue(jobType JobType, teamID string, payload any) string {
	id := newUUID()
	q.jobs <- Job{ID: id, Type: jobType, TeamID: teamID, Payload: payload}
	return id
}

// Subscribe registers a channel to receive SSE events for a team.
// The returned cancel function must be called when the SSE connection closes.
func (q *Queue) Subscribe(teamID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	q.mu.Lock()
	q.subscribers[teamID] = append(q.subscribers[teamID], ch)
	q.mu.Unlock()

	cancel := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		subs := q.subscribers[teamID]
		for i, s := range subs {
			if s == ch {
				q.subscribers[teamID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}
	return ch, cancel
}

// Publish sends an event to all SSE subscribers of the event's team.
func (q *Queue) Publish(ev Event) {
	q.mu.RLock()
	subs := q.subscribers[ev.TeamID]
	q.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// drop if subscriber is too slow
		}
	}
}

// run is the worker goroutine loop.
func (q *Queue) run() {
	for job := range q.jobs {
		q.process(job)
	}
}

// process dispatches a job to the appropriate handler.
func (q *Queue) process(job Job) {
	var handler func(Job)
	switch job.Type {
	case JobBOMImport:
		handler = q.handleBOMImport
	case JobPartEnrich:
		handler = q.handlePartEnrich
	case JobListingRefresh:
		handler = q.handleListingRefresh
	default:
		handler = q.handleUnknown
	}
	handler(job)
}

func (q *Queue) publishError(job Job, msg string) {
	data, _ := json.Marshal(map[string]any{
		"job_id": job.ID,
		"status": "error",
		"error":  msg,
	})
	q.Publish(Event{
		Type:   string(job.Type),
		Data:   data,
		JobID:  job.ID,
		TeamID: job.TeamID,
	})
}

func (q *Queue) publishProgress(job Job, eventType string, data any) {
	b, _ := json.Marshal(data)
	q.Publish(Event{Type: eventType, Data: b, JobID: job.ID, TeamID: job.TeamID})
}

func (q *Queue) handleBOMImport(job Job) {
	ctx := context.Background()

	payload, ok := job.Payload.(map[string]string)
	if !ok {
		q.publishError(job, "invalid payload type")
		return
	}
	revIDStr, ok := payload["bom_revision_id"]
	if !ok {
		q.publishError(job, "missing bom_revision_id in payload")
		return
	}
	var revID pgtype.UUID
	if err := revID.Scan(revIDStr); err != nil {
		q.publishError(job, "invalid bom_revision_id: "+revIDStr)
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(job.TeamID); err != nil {
		q.publishError(job, "invalid team_id: "+job.TeamID)
		return
	}

	queries := dbgen.New(q.db)

	items, err := queries.ListBOMLineItems(ctx, revID)
	if err != nil {
		q.publishError(job, "failed to list BOM line items: "+err.Error())
		return
	}

	alreadyResolved := 0
	for _, it := range items {
		if it.ResolvedPartID.Valid {
			alreadyResolved++
		}
	}

	q.publishProgress(job, "bom_import_progress", map[string]any{
		"status":   "progress",
		"resolved": alreadyResolved,
		"total":    len(items),
		"job_id":   job.ID,
	})

	resolvedCount := alreadyResolved
	for _, item := range items {
		if item.ResolvedPartID.Valid {
			continue // already resolved
		}
		if item.RawDescription == nil || *item.RawDescription == "" {
			continue // nothing to match against
		}

		part, err := queries.GetPartByMPN(ctx, dbgen.GetPartByMPNParams{
			Mpn:    item.RawDescription,
			TeamID: teamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // not in catalog; not an error
			}
			continue // other DB error; skip item, non-fatal
		}

		if _, err := queries.UpdateBOMLineItem(ctx, dbgen.UpdateBOMLineItemParams{
			ResolvedPartID: part.ID,
			ID:             item.ID,
			BomRevisionID:  revID,
		}); err != nil {
			continue // non-fatal
		}

		// Ensure team has an inventory entry for this part.
		_, err = queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
			TeamID: teamID,
			PartID: part.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			_, _ = queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
				TeamID: teamID,
				PartID: part.ID,
			})
		}

		resolvedCount++
		q.publishProgress(job, "bom_import_progress", map[string]any{
			"status":   "progress",
			"resolved": resolvedCount,
			"total":    len(items),
			"job_id":   job.ID,
		})
	}

	q.publishProgress(job, "bom_import_progress", map[string]any{
		"status":   "complete",
		"resolved": resolvedCount,
		"total":    len(items),
		"job_id":   job.ID,
	})
}

func (q *Queue) handlePartEnrich(job Job) {
	ctx := context.Background()

	payload, ok := job.Payload.(map[string]string)
	if !ok {
		q.publishError(job, "invalid payload type")
		return
	}
	partIDStr, ok := payload["part_id"]
	if !ok {
		q.publishError(job, "missing part_id in payload")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(partIDStr); err != nil {
		q.publishError(job, "invalid part_id: "+partIDStr)
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(job.TeamID); err != nil {
		q.publishError(job, "invalid team_id: "+job.TeamID)
		return
	}

	queries := dbgen.New(q.db)

	part, err := queries.GetPart(ctx, partID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			q.publishError(job, fmt.Sprintf("part %s not found", partIDStr))
		} else {
			q.publishError(job, "failed to get part: "+err.Error())
		}
		return
	}
	if part.Mpn == nil {
		q.publishError(job, "part has no MPN, cannot enrich")
		return
	}

	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil {
		q.publishError(job, "failed to list distributor configs: "+err.Error())
		return
	}

	for _, cfg := range configs {
		if cfg.ApiType == nil {
			continue
		}
		client := q.distributors.Lookup(*cfg.ApiType)
		if client == nil {
			continue // no client registered for this api_type
		}

		result, err := client.EnrichPart(ctx, *part.Mpn, json.RawMessage(cfg.ApiCredentials))
		if err != nil {
			q.publishProgress(job, "part_enrichment_progress", map[string]any{
				"status":         "error",
				"distributor_id": cfg.DistributorID.String(),
				"error":          err.Error(),
			})
			continue
		}
		if result == nil {
			q.publishProgress(job, "part_enrichment_progress", map[string]any{
				"status":         "not_found",
				"distributor_id": cfg.DistributorID.String(),
			})
			continue
		}

		tx, err := q.db.Begin(ctx)
		if err != nil {
			q.publishProgress(job, "part_enrichment_progress", map[string]any{
				"status":         "error",
				"distributor_id": cfg.DistributorID.String(),
				"error":          "failed to begin transaction: " + err.Error(),
			})
			return
		}
		qtx := queries.WithTx(tx)

		if err := qtx.DeleteDistributorParameters(ctx, dbgen.DeleteDistributorParametersParams{
			PartID:              partID,
			SourceDistributorID: cfg.DistributorID,
		}); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			q.publishProgress(job, "part_enrichment_progress", map[string]any{
				"status":         "error",
				"distributor_id": cfg.DistributorID.String(),
				"error":          "failed to delete parameters: " + err.Error(),
			})
			return
		}

		for _, p := range result.Parameters {
			var unit *string
			if p.Unit != "" {
				u := p.Unit
				unit = &u
			}
			if _, err := qtx.InsertDistributorParameter(ctx, dbgen.InsertDistributorParameterParams{
				PartID:              partID,
				Name:                p.Name,
				Value:               p.Value,
				Unit:                unit,
				SourceDistributorID: cfg.DistributorID,
			}); err != nil {
				tx.Rollback(ctx) //nolint:errcheck
				q.publishProgress(job, "part_enrichment_progress", map[string]any{
					"status":         "error",
					"distributor_id": cfg.DistributorID.String(),
					"error":          "failed to insert parameter: " + err.Error(),
				})
				return
			}
		}

		for _, l := range result.Listings {
			unitPrice, err := parseNumeric(l.UnitPrice)
			if err != nil {
				tx.Rollback(ctx) //nolint:errcheck
				q.publishProgress(job, "part_enrichment_progress", map[string]any{
					"status":         "error",
					"distributor_id": cfg.DistributorID.String(),
					"error":          "invalid unit_price: " + l.UnitPrice,
				})
				return
			}
			fixedCost, err := parseNumeric(l.FixedOrderCost)
			if err != nil {
				tx.Rollback(ctx) //nolint:errcheck
				q.publishProgress(job, "part_enrichment_progress", map[string]any{
					"status":         "error",
					"distributor_id": cfg.DistributorID.String(),
					"error":          "invalid fixed_order_cost: " + l.FixedOrderCost,
				})
				return
			}
			if _, err := qtx.UpsertListing(ctx, dbgen.UpsertListingParams{
				PartID:         partID,
				DistributorID:  cfg.DistributorID,
				Sku:            l.SKU,
				PackagingType:  l.PackagingType,
				UnitPrice:      unitPrice,
				StockAvailable: l.StockAvailable,
				Moq:            l.Moq,
				FixedOrderCost: fixedCost,
			}); err != nil {
				tx.Rollback(ctx) //nolint:errcheck
				q.publishProgress(job, "part_enrichment_progress", map[string]any{
					"status":         "error",
					"distributor_id": cfg.DistributorID.String(),
					"error":          "failed to upsert listing: " + err.Error(),
				})
				return
			}
		}

		if err := qtx.SetPartEnriched(ctx, dbgen.SetPartEnrichedParams{
			ID:                 partID,
			EnrichmentSourceID: cfg.DistributorID,
		}); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			q.publishProgress(job, "part_enrichment_progress", map[string]any{
				"status":         "error",
				"distributor_id": cfg.DistributorID.String(),
				"error":          "failed to set enriched: " + err.Error(),
			})
			return
		}

		if err := tx.Commit(ctx); err != nil {
			q.publishProgress(job, "part_enrichment_progress", map[string]any{
				"status":         "error",
				"distributor_id": cfg.DistributorID.String(),
				"error":          "failed to commit: " + err.Error(),
			})
			return
		}

		q.publishProgress(job, "part_enrichment_progress", map[string]any{
			"status":         "done",
			"distributor_id": cfg.DistributorID.String(),
			"listings":       len(result.Listings),
			"parameters":     len(result.Parameters),
		})
	}

	q.publishProgress(job, "part_enrichment_progress", map[string]any{
		"status": "complete",
		"job_id": job.ID,
	})
}

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

	if err := queries.UpdateListingData(ctx, dbgen.UpdateListingDataParams{
		ID:             listingID,
		UnitPrice:      unitPrice,
		StockAvailable: result.StockAvailable,
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
}

func (q *Queue) handleUnknown(job Job) {
	q.publishError(job, fmt.Sprintf("unknown job type: %s", job.Type))
}

// parseNumeric converts a decimal string to pgtype.Numeric.
// An empty string returns a zero (null) Numeric.
func parseNumeric(s string) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if s == "" {
		return n, nil
	}
	err := n.Scan(s)
	return n, err
}
