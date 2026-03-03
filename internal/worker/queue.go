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
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
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
	UserID string          `json:"-"` // skip delivery to this user's channel; empty = send to all
}

// subscription pairs an SSE channel with the user who owns the connection.
type subscription struct {
	userID string
	ch     chan Event
}

// Queue is a channel-based job dispatcher with an integrated SSE hub.
// Jobs are processed by a pool of worker goroutines. Progress events are
// fanned out to all active SSE subscribers for the relevant team.
type Queue struct {
	jobs         chan Job
	subscribers  map[string][]subscription // keyed by team ID
	mu           sync.RWMutex
	db           *pgxpool.Pool
	distributors distributor.Registry
}

// New creates a Queue and starts n worker goroutines.
func New(workers int, db *pgxpool.Pool, reg distributor.Registry) *Queue {
	q := &Queue{
		jobs:         make(chan Job, 64),
		subscribers:  make(map[string][]subscription),
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
// userID identifies the connected user so that Publish can skip sending
// entity-change events back to the user who caused them.
// The returned cancel function must be called when the SSE connection closes.
func (q *Queue) Subscribe(teamID, userID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	sub := subscription{userID: userID, ch: ch}
	q.mu.Lock()
	q.subscribers[teamID] = append(q.subscribers[teamID], sub)
	q.mu.Unlock()

	cancel := func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		subs := q.subscribers[teamID]
		for i, s := range subs {
			if s.ch == ch {
				q.subscribers[teamID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}
	return ch, cancel
}

// Publish sends an event to all SSE subscribers of the event's team.
// If ev.UserID is non-empty, the subscriber whose userID matches is skipped
// (the acting user's own mutations are handled by client-side cache invalidation).
func (q *Queue) Publish(ev Event) {
	q.mu.RLock()
	subs := q.subscribers[ev.TeamID]
	q.mu.RUnlock()
	for _, sub := range subs {
		if ev.UserID != "" && sub.userID == ev.UserID {
			continue
		}
		select {
		case sub.ch <- ev:
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

	// Pre-fetch distributor configs for step 2 (distributor API fallback).
	distCfgByID := make(map[pgtype.UUID]*dbgen.ListEnabledDistributorConfigsForTeamRow)
	if cfgs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID); err == nil {
		for i := range cfgs {
			distCfgByID[cfgs[i].DistributorID] = &cfgs[i]
		}
	}

	items, err := queries.ListBOMLineItems(ctx, revID)
	if err != nil {
		q.publishError(job, "failed to list BOM line items: "+err.Error())
		return
	}

	projectID, err := queries.GetRevisionProjectID(ctx, revID)
	if err != nil {
		q.publishError(job, "failed to get revision project: "+err.Error())
		return
	}

	// Count items already resolved before this run.
	resolvedSKU, resolvedMPN, resolvedPrior, resolvedDistributor, ambiguousCount := 0, 0, 0, 0, 0
	for _, it := range items {
		switch {
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_sku":
			resolvedSKU++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_distributor":
			resolvedDistributor++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_mpn":
			resolvedMPN++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_prior_revision":
			resolvedPrior++
		case it.ResolutionState != nil && *it.ResolutionState == "ambiguous":
			ambiguousCount++
		}
	}

	q.publishProgress(job, "bom_import_progress", map[string]any{
		"status":                  "progress",
		"resolved":                resolvedSKU + resolvedDistributor + resolvedMPN + resolvedPrior,
		"resolved_sku":            resolvedSKU,
		"resolved_distributor":    resolvedDistributor,
		"resolved_mpn":            resolvedMPN,
		"resolved_prior_revision": resolvedPrior,
		"ambiguous":               ambiguousCount,
		"no_match":                0,
		"total":                   len(items),
		"job_id":                  job.ID,
	})

	strPtr := func(s string) *string { return &s }

	noMatchCount := 0
	lastPublish := time.Now()

	publishProgress := func(status string) {
		q.publishProgress(job, "bom_import_progress", map[string]any{
			"status":                  status,
			"resolved":                resolvedSKU + resolvedDistributor + resolvedMPN + resolvedPrior,
			"resolved_sku":            resolvedSKU,
			"resolved_distributor":    resolvedDistributor,
			"resolved_mpn":            resolvedMPN,
			"resolved_prior_revision": resolvedPrior,
			"ambiguous":               ambiguousCount,
			"no_match":                noMatchCount,
			"total":                   len(items),
			"job_id":                  job.ID,
		})
	}

	for _, item := range items {
		if item.ResolvedPartID.Valid {
			continue // already resolved in a previous run
		}
		if item.ResolutionState != nil {
			continue // already classified (e.g. ambiguous, ignored)
		}

		var resolvedPartID pgtype.UUID
		var resolutionState *string

		// 1. SKU-based resolution: only if a resolved distributor ID was explicitly chosen.
		if item.Sku != nil && *item.Sku != "" && item.ResolvedDistributorID.Valid {
			if listing, err := queries.GetListingBySKUAndDistributorID(ctx, dbgen.GetListingBySKUAndDistributorIDParams{
				Sku:           *item.Sku,
				DistributorID: item.ResolvedDistributorID,
			}); err == nil {
				resolvedPartID = listing.PartID
				resolutionState = strPtr("auto_sku")
			}
		}

		// 2. Distributor API fallback: SKU is known but not yet in our listings table.
		// Query the distributor to find or create the part and attach the listing.
		if resolutionState == nil && item.Sku != nil && *item.Sku != "" && item.ResolvedDistributorID.Valid {
			if cfg, ok := distCfgByID[item.ResolvedDistributorID]; ok && cfg.ApiType != nil {
				if client := q.distributors.Lookup(*cfg.ApiType); client != nil {
					if result, err := client.EnrichPart(ctx, *item.Sku, json.RawMessage(cfg.ApiCredentials)); err == nil && result != nil {
						if partID, err := ApplyEnrichmentResult(ctx, q.db, queries, teamID, cfg.DistributorID, result, item.Mpn, item.Manufacturer); err == nil {
							resolvedPartID = partID
							resolutionState = strPtr("auto_distributor")
						}
					}
				}
			}
		}

		// 3a. MPN + Manufacturer (preferred — avoids false ambiguity from same MPN across manufacturers).
		if resolutionState == nil && item.Mpn != nil && *item.Mpn != "" &&
			item.Manufacturer != nil && *item.Manufacturer != "" {
			matched, err := queries.ListPartsByMPNAndManufacturer(ctx, dbgen.ListPartsByMPNAndManufacturerParams{
				Mpn:          item.Mpn,
				Manufacturer: item.Manufacturer,
				TeamID:       teamID,
			})
			switch {
			case err == nil && len(matched) == 1:
				resolvedPartID = matched[0].ID
				resolutionState = strPtr("auto_mpn")
			case err == nil && len(matched) > 1:
				resolutionState = strPtr("ambiguous")
			}
		}

		// 3b. MPN only (fallback when manufacturer is absent or 3a found nothing).
		if resolutionState == nil && item.Mpn != nil && *item.Mpn != "" {
			matched, err := queries.ListPartsByMPN(ctx, dbgen.ListPartsByMPNParams{
				Mpn:    item.Mpn,
				TeamID: teamID,
			})
			switch {
			case err == nil && len(matched) == 1:
				resolvedPartID = matched[0].ID
				resolutionState = strPtr("auto_mpn")
			case err == nil && len(matched) > 1:
				resolutionState = strPtr("ambiguous")
			}
		}

		// 4. Prior-revision fallback: if automatic resolution found nothing or was ambiguous,
		// check whether a previous revision of this project already resolved this item.
		if resolutionState == nil || *resolutionState == "ambiguous" {
			var priorPartID pgtype.UUID

			// 4a. MPN-based: prefer MPN+manufacturer for precision, fall back to MPN-only.
			if item.Mpn != nil && *item.Mpn != "" {
				if item.Manufacturer != nil && *item.Manufacturer != "" {
					if id, err := queries.GetPriorRevisionPartByMPNAndManufacturer(ctx, dbgen.GetPriorRevisionPartByMPNAndManufacturerParams{
						ProjectID:         projectID,
						CurrentRevisionID: revID,
						Mpn:               item.Mpn,
						Manufacturer:      item.Manufacturer,
					}); err == nil {
						priorPartID = id
					}
				}
			}

			// 4b. SKU+distributor fallback: covers items with no MPN.
			if !priorPartID.Valid && item.Sku != nil && *item.Sku != "" && item.ResolvedDistributorID.Valid {
				if id, err := queries.GetPriorRevisionPartBySKU(ctx, dbgen.GetPriorRevisionPartBySKUParams{
					ProjectID:         projectID,
					CurrentRevisionID: revID,
					Sku:               item.Sku,
					DistributorID:     item.ResolvedDistributorID,
				}); err == nil {
					priorPartID = id
				}
			}

			if priorPartID.Valid {
				resolvedPartID = priorPartID
				resolutionState = strPtr("auto_prior_revision")
			}
		}

		if resolutionState == nil {
			// No match found — count and fall through to rate-limited progress update.
			noMatchCount++
		} else {
			if _, err := queries.UpdateBOMLineItem(ctx, dbgen.UpdateBOMLineItemParams{
				ResolvedPartID:  resolvedPartID,
				ResolutionState: resolutionState,
				ID:              item.ID,
				BomRevisionID:   revID,
			}); err != nil {
				continue // non-fatal; skip progress update for this item
			}

			switch *resolutionState {
			case "auto_sku":
				resolvedSKU++
			case "auto_distributor":
				resolvedDistributor++
			case "auto_mpn":
				resolvedMPN++
			case "auto_prior_revision":
				resolvedPrior++
			case "ambiguous":
				ambiguousCount++
			}

			if resolvedPartID.Valid {
				// Ensure team has an inventory entry for this part.
				_, err = queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
					TeamID: teamID,
					PartID: resolvedPartID,
				})
				if errors.Is(err, pgx.ErrNoRows) {
					_, _ = queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
						TeamID: teamID,
						PartID: resolvedPartID,
					})
				}
			}
		}

		if time.Since(lastPublish) >= 100*time.Millisecond {
			publishProgress("progress")
			lastPublish = time.Now()
		}
	}

	publishProgress("complete")

	projData, _ := json.Marshal(map[string]string{"project_id": payload["project_id"]})
	q.Publish(Event{Type: "project_changed", TeamID: job.TeamID, UserID: payload["user_id"], Data: projData})
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
	sku := payload["sku"]
	if sku == "" {
		q.publishError(job, "missing sku in payload")
		return
	}
	distIDStr := payload["distributor_id"]
	if distIDStr == "" {
		q.publishError(job, "missing distributor_id in payload")
		return
	}

	var partID pgtype.UUID
	if err := partID.Scan(partIDStr); err != nil {
		q.publishError(job, "invalid part_id: "+partIDStr)
		return
	}
	var distributorID pgtype.UUID
	if err := distributorID.Scan(distIDStr); err != nil {
		q.publishError(job, "invalid distributor_id: "+distIDStr)
		return
	}
	var teamID pgtype.UUID
	if err := teamID.Scan(job.TeamID); err != nil {
		q.publishError(job, "invalid team_id: "+job.TeamID)
		return
	}

	queries := dbgen.New(q.db)

	if _, err := queries.GetPart(ctx, partID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			q.publishError(job, fmt.Sprintf("part %s not found", partIDStr))
		} else {
			q.publishError(job, "failed to get part: "+err.Error())
		}
		return
	}

	// Find the specific distributor config for this team + distributor.
	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil {
		q.publishError(job, "failed to list distributor configs: "+err.Error())
		return
	}
	var cfg *dbgen.ListEnabledDistributorConfigsForTeamRow
	for i := range configs {
		if configs[i].DistributorID == distributorID {
			cfg = &configs[i]
			break
		}
	}
	if cfg == nil {
		q.publishError(job, fmt.Sprintf("distributor %s not configured or not enabled for this team", distIDStr))
		return
	}
	if cfg.ApiType == nil {
		q.publishError(job, "distributor has no API integration")
		return
	}
	client := q.distributors.Lookup(*cfg.ApiType)
	if client == nil {
		q.publishError(job, fmt.Sprintf("no client registered for api_type %s", *cfg.ApiType))
		return
	}

	result, err := client.EnrichPart(ctx, sku, json.RawMessage(cfg.ApiCredentials))
	if err != nil {
		q.publishError(job, "enrichment failed: "+err.Error())
		return
	}
	if result == nil {
		q.publishProgress(job, "part_enrichment_progress", map[string]any{
			"status":         "not_found",
			"distributor_id": distIDStr,
		})
		return
	}

	tx, err := q.db.Begin(ctx)
	if err != nil {
		q.publishError(job, "failed to begin transaction: "+err.Error())
		return
	}
	qtx := queries.WithTx(tx)

	if err := qtx.DeleteDistributorParameters(ctx, dbgen.DeleteDistributorParametersParams{
		PartID:              partID,
		SourceDistributorID: cfg.DistributorID,
	}); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		q.publishError(job, "failed to delete parameters: "+err.Error())
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
			q.publishError(job, "failed to insert parameter: "+err.Error())
			return
		}
	}

	for _, l := range result.Listings {
		unitPrice, err := parseNumeric(l.UnitPrice)
		if err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			q.publishError(job, "invalid unit_price: "+l.UnitPrice)
			return
		}
		fixedCost, err := parseNumeric(l.FixedOrderCost)
		if err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			q.publishError(job, "invalid fixed_order_cost: "+l.FixedOrderCost)
			return
		}
		priceBreaksJSON, _ := json.Marshal(l.PriceBreaks)
		if _, err := qtx.UpsertListing(ctx, dbgen.UpsertListingParams{
			PartID:         partID,
			DistributorID:  cfg.DistributorID,
			Sku:            l.SKU,
			PackagingType:  l.PackagingType,
			UnitPrice:      unitPrice,
			StockAvailable: l.StockAvailable,
			Moq:            l.Moq,
			FixedOrderCost: fixedCost,
			PriceBreaks:    priceBreaksJSON,
		}); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			q.publishError(job, "failed to upsert listing: "+err.Error())
			return
		}
	}

	var lifecycleStatus dbgen.NullPartLifecycleStatus
	if result.LifecycleStatus != "" {
		lifecycleStatus = dbgen.NullPartLifecycleStatus{
			PartLifecycleStatus: dbgen.PartLifecycleStatus(result.LifecycleStatus),
			Valid:               true,
		}
	}
	var mpn *string
	if result.MPN != "" {
		s := result.MPN
		mpn = &s
	}
	if err := qtx.SetPartEnriched(ctx, dbgen.SetPartEnrichedParams{
		ID:                 partID,
		EnrichmentSourceID: cfg.DistributorID,
		LifecycleStatus:    lifecycleStatus,
		Mpn:                mpn,
	}); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		q.publishError(job, "failed to set enriched: "+err.Error())
		return
	}

	if err := tx.Commit(ctx); err != nil {
		q.publishError(job, "failed to commit: "+err.Error())
		return
	}

	q.publishProgress(job, "part_enrichment_progress", map[string]any{
		"status":         "complete",
		"job_id":         job.ID,
		"distributor_id": distIDStr,
		"listings":       len(result.Listings),
		"parameters":     len(result.Parameters),
	})

	data, _ := json.Marshal(map[string]string{"part_id": partIDStr})
	q.Publish(Event{Type: "part_changed", TeamID: job.TeamID, Data: data})
}

// applyEnrichmentResult finds or creates a part matching the enrichment result and
// applies distributor data (listings, parameters, metadata) in a single transaction.
//
// If the part is found by MPN+manufacturer lookup it is reused; only its listings and
// enrichment metadata are updated — existing parameters are left intact to avoid
// overwriting previously curated data.
// If no matching part exists in the catalog a new global-scoped part is created and
// fully enriched (parameters + listings + metadata).
//
// fallbackMPN and fallbackManufacturer are used when the enrichment result itself
// omits those fields. Returns an invalid pgtype.UUID and a non-nil error on failure.
// ApplyEnrichmentResult finds or creates a part matching result, applies parameters
// (for newly created parts only, to preserve curated data on existing parts), upserts all
// distributor listings, and marks the part enriched. Manages its own database transaction.
// Returns the resolved part ID.
func ApplyEnrichmentResult(
	ctx context.Context,
	db *pgxpool.Pool,
	queries *dbgen.Queries,
	teamID pgtype.UUID,
	distributorID pgtype.UUID,
	result *distributor.EnrichmentResult,
	fallbackMPN, fallbackManufacturer *string,
) (pgtype.UUID, error) {
	strOrNil := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}

	mpn := strOrNil(result.MPN)
	if mpn == nil {
		mpn = fallbackMPN
	}
	manufacturer := strOrNil(result.Manufacturer)
	if manufacturer == nil {
		manufacturer = fallbackManufacturer
	}

	// 1. Try to find an existing part.
	var partID pgtype.UUID
	partIsNew := false

	if mpn != nil && manufacturer != nil {
		matched, err := queries.ListPartsByMPNAndManufacturer(ctx, dbgen.ListPartsByMPNAndManufacturerParams{
			Mpn:          mpn,
			Manufacturer: manufacturer,
			TeamID:       teamID,
		})
		switch {
		case err == nil && len(matched) == 1:
			partID = matched[0].ID
		case err == nil && len(matched) > 1:
			return pgtype.UUID{}, fmt.Errorf("ambiguous: multiple parts match MPN %q manufacturer %q", *mpn, *manufacturer)
		}
	}
	if !partID.Valid && mpn != nil {
		matched, err := queries.ListPartsByMPN(ctx, dbgen.ListPartsByMPNParams{
			Mpn:    mpn,
			TeamID: teamID,
		})
		switch {
		case err == nil && len(matched) == 1:
			partID = matched[0].ID
		case err == nil && len(matched) > 1:
			return pgtype.UUID{}, fmt.Errorf("ambiguous: multiple parts match MPN %q", *mpn)
		}
	}

	// 2. Create a new global-scoped part if none found.
	if !partID.Valid {
		lifecycleStatus := dbgen.PartLifecycleStatusActive
		if result.LifecycleStatus != "" {
			lifecycleStatus = dbgen.PartLifecycleStatus(result.LifecycleStatus)
		}
		part, err := queries.CreatePart(ctx, dbgen.CreatePartParams{
			Scope:           dbgen.PartScopeGlobal,
			TeamID:          pgtype.UUID{}, // NULL for global parts
			Mpn:             mpn,
			Manufacturer:    manufacturer,
			Description:     strOrNil(result.Description),
			Category:        strOrNil(result.Category),
			Package:         strOrNil(result.Package),
			DatasheetUrl:    strOrNil(result.DatasheetURL),
			LifecycleStatus: lifecycleStatus,
		})
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("create part: %w", err)
		}
		partID = part.ID
		partIsNew = true
	}

	// 3. Apply enrichment data in a transaction.
	tx, err := db.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin tx: %w", err)
	}
	qtx := queries.WithTx(tx)

	// For new parts only: insert parameters (plain INSERT; safe because the part has
	// no prior rows). Existing parts keep their parameters to preserve curated data.
	if partIsNew {
		for _, p := range result.Parameters {
			unit := strOrNil(p.Unit)
			if _, err := qtx.InsertDistributorParameter(ctx, dbgen.InsertDistributorParameterParams{
				PartID:              partID,
				Name:                p.Name,
				Value:               p.Value,
				Unit:                unit,
				SourceDistributorID: distributorID,
			}); err != nil {
				tx.Rollback(ctx) //nolint:errcheck
				return pgtype.UUID{}, fmt.Errorf("insert parameter %q: %w", p.Name, err)
			}
		}
	}

	// Upsert listings — ON CONFLICT (distributor_id, sku) DO UPDATE, so existing
	// listings for other SKUs or distributors are untouched.
	for _, l := range result.Listings {
		unitPrice, err := parseNumeric(l.UnitPrice)
		if err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return pgtype.UUID{}, fmt.Errorf("invalid unit_price %q: %w", l.UnitPrice, err)
		}
		fixedCost, err := parseNumeric(l.FixedOrderCost)
		if err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return pgtype.UUID{}, fmt.Errorf("invalid fixed_order_cost %q: %w", l.FixedOrderCost, err)
		}
		priceBreaksJSON, _ := json.Marshal(l.PriceBreaks)
		if _, err := qtx.UpsertListing(ctx, dbgen.UpsertListingParams{
			PartID:         partID,
			DistributorID:  distributorID,
			Sku:            l.SKU,
			PackagingType:  l.PackagingType,
			UnitPrice:      unitPrice,
			StockAvailable: l.StockAvailable,
			Moq:            l.Moq,
			FixedOrderCost: fixedCost,
			PriceBreaks:    priceBreaksJSON,
		}); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return pgtype.UUID{}, fmt.Errorf("upsert listing %q: %w", l.SKU, err)
		}
	}

	var enrichedLifecycle dbgen.NullPartLifecycleStatus
	if result.LifecycleStatus != "" {
		enrichedLifecycle = dbgen.NullPartLifecycleStatus{
			PartLifecycleStatus: dbgen.PartLifecycleStatus(result.LifecycleStatus),
			Valid:               true,
		}
	}
	if err := qtx.SetPartEnriched(ctx, dbgen.SetPartEnrichedParams{
		ID:                 partID,
		EnrichmentSourceID: distributorID,
		LifecycleStatus:    enrichedLifecycle,
		Mpn:                mpn,
	}); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		return pgtype.UUID{}, fmt.Errorf("set enriched: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit: %w", err)
	}

	return partID, nil
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
