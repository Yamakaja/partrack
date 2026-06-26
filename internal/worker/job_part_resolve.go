package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/mfrmatch"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const enrichmentTimeout = 60 * time.Second

// enrichByMPN queries a distributor for products matching an MPN that also belong
// to the given manufacturer. It calls SearchParts to discover candidates, keeps only
// those whose MPN matches exactly AND whose manufacturer matches (so a generic MPN like
// "LM317" doesn't pull in same-MPN parts from unrelated manufacturers), then EnrichPart
// for each (up to maxResults). A single partrack part is unique on Manufacturer+MPN, so
// cross-distributor discovery must constrain on both.
//
// When manufacturer is empty (the target manufacturer is unknown) the manufacturer
// constraint is skipped, but the exact-MPN requirement still holds — there is no
// best-effort "top result" fallback, which previously surfaced unrelated MPNs.
func enrichByMPN(ctx context.Context, c distributor.Client, mpn, manufacturer string, creds json.RawMessage, maxResults int, mfr *mfrmatch.Resolver) ([]*distributor.EnrichmentResult, error) {
	distName := c.Name()
	slog.Debug("enrichByMPN: SearchParts", "distributor", distName, "mpn", mpn, "manufacturer", manufacturer)
	sr, err := c.SearchParts(ctx, distributor.SearchQuery{Keywords: mpn, Limit: 20}, creds)
	if err != nil {
		slog.Debug("enrichByMPN: SearchParts error, falling back to EnrichPart", "distributor", distName, "mpn", mpn, "err", err)
	} else if sr == nil || len(sr.Products) == 0 {
		slog.Debug("enrichByMPN: SearchParts returned no results, falling back to EnrichPart", "distributor", distName, "mpn", mpn)
	} else {
		slog.Debug("enrichByMPN: SearchParts returned results", "distributor", distName, "mpn", mpn, "total", sr.TotalCount, "page", len(sr.Products), "exact_matches", len(sr.ExactMatchIndices))

		// Prefer distributor-provided exact-match indices; fall back to MPN comparison.
		indices := sr.ExactMatchIndices
		if len(indices) == 0 {
			for i, p := range sr.Products {
				if strings.EqualFold(p.MPN, mpn) {
					indices = append(indices, i)
				}
			}
		}

		// Keep only candidates that match BOTH the exact MPN and the manufacturer.
		// No top-result fallback: an unrelated top hit is worse than no match here.
		var results []*distributor.EnrichmentResult
		for _, idx := range indices {
			if idx >= len(sr.Products) || len(results) >= maxResults {
				break
			}
			cand := sr.Products[idx]
			if !strings.EqualFold(cand.MPN, mpn) {
				continue
			}
			if !mfr.Matches(cand.Manufacturer, manufacturer) {
				slog.Debug("enrichByMPN: skipping manufacturer mismatch", "distributor", distName, "mpn", mpn, "want_mfr", manufacturer, "got_mfr", cand.Manufacturer)
				continue
			}
			if cand.PrimarySKU == "" {
				continue
			}
			slog.Debug("enrichByMPN: EnrichPart for candidate", "distributor", distName, "mpn", mpn, "candidate_mpn", cand.MPN, "sku", cand.PrimarySKU)
			r, err := c.EnrichPart(ctx, cand.PrimarySKU, creds)
			if err != nil {
				slog.Debug("enrichByMPN: EnrichPart error", "distributor", distName, "sku", cand.PrimarySKU, "err", err)
				continue
			}
			if r == nil {
				slog.Debug("enrichByMPN: EnrichPart returned nil", "distributor", distName, "sku", cand.PrimarySKU)
				continue
			}
			results = append(results, r)
		}
		// SearchParts succeeded: trust its catalogue. Whether or not we found a
		// manufacturer+MPN match, do not fall through to the EnrichPart(mpn) probe
		// (which would treat the MPN as a SKU and bypass the manufacturer filter).
		slog.Debug("enrichByMPN: done via SearchParts", "distributor", distName, "mpn", mpn, "products", len(results))
		return results, nil
	}

	// SearchParts is unsupported or errored: probe EnrichPart(mpn) directly (treats
	// the MPN as a SKU). Still enforce the manufacturer constraint on the result.
	slog.Debug("enrichByMPN: EnrichPart fallback", "distributor", distName, "mpn", mpn)
	r, err := c.EnrichPart(ctx, mpn, creds)
	if err != nil {
		return nil, err
	}
	if r == nil {
		slog.Debug("enrichByMPN: EnrichPart fallback returned nil", "distributor", distName, "mpn", mpn)
		return nil, nil
	}
	if !mfr.Matches(r.Manufacturer, manufacturer) {
		slog.Debug("enrichByMPN: EnrichPart fallback manufacturer mismatch", "distributor", distName, "mpn", mpn, "want_mfr", manufacturer, "got_mfr", r.Manufacturer)
		return nil, nil
	}
	slog.Debug("enrichByMPN: done via EnrichPart fallback", "distributor", distName, "mpn", mpn)
	return []*distributor.EnrichmentResult{r}, nil
}

// primaryManufacturer returns the manufacturer reported by the originally-requested
// distributor (the authoritative source for the part's identity), or "" if it has no
// stored result. Used to recover the target manufacturer for re-queries when the
// original request was SKU-only.
func primaryManufacturer(ed resolveEnrichmentData, primaryDistID string) string {
	if primaryDistID == "" {
		return ""
	}
	for _, e := range ed.Distributors {
		if e.DistributorID != primaryDistID {
			continue
		}
		for _, r := range e.Results {
			if r != nil && r.Manufacturer != "" {
				return r.Manufacturer
			}
		}
	}
	return ""
}

// AdditionalListing identifies a specific distributor listing (by distributor + SKU)
// to import alongside the primary listing at commit time.
type AdditionalListing struct {
	DistributorID string
	SKU           string
}

// resolveInputFields is the shape stored in part_resolve_items.input_fields.
type resolveInputFields struct {
	SKU           string `json:"sku"`
	DistributorID string `json:"distributor_id"`
	MPN           string `json:"mpn"`
	Manufacturer  string `json:"manufacturer"`
}

func (q *Queue) handlePartResolve(job Job) {
	ctx := context.Background()

	payload, ok := job.Payload.(map[string]string)
	if !ok {
		return
	}
	itemIDStr := payload["resolve_item_id"]
	teamIDStr := payload["team_id"]
	userIDStr := payload["user_id"]
	retryDistributorIDs := payload["retry_distributor_ids"]

	var itemID, teamID pgtype.UUID
	if itemID.Scan(itemIDStr) != nil || teamID.Scan(teamIDStr) != nil {
		return
	}

	if retryDistributorIDs != "" {
		q.handlePartResolveRetryDistributors(ctx, itemID, teamID, itemIDStr, userIDStr, teamIDStr, retryDistributorIDs)
		return
	}

	queries := dbgen.New(q.db)

	item, err := queries.GetPartResolveItem(ctx, itemID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("part resolve: item not found", "item_id", itemIDStr, "err", err)
		}
		return
	}
	if item.Status != "pending" {
		slog.Debug("part resolve: skipping non-pending item", "item_id", itemIDStr, "status", item.Status)
		return
	}

	var pf resolveInputFields
	if len(item.InputFields) > 0 {
		json.Unmarshal(item.InputFields, &pf) //nolint:errcheck
	}
	slog.Debug("part resolve: start", "item_id", itemIDStr, "sku", pf.SKU, "distributor_id", pf.DistributorID, "mpn", pf.MPN, "manufacturer", pf.Manufacturer)

	// ── Phase 1: synchronous DB lookup ────────────────────────────────────────

	var foundPartID pgtype.UUID
	var hasInventory bool

	if pf.SKU != "" && pf.DistributorID != "" {
		var distID pgtype.UUID
		if distID.Scan(pf.DistributorID) == nil {
			row, err := queries.GetPartBySKUAndDistributorIDWithInventory(ctx, dbgen.GetPartBySKUAndDistributorIDWithInventoryParams{
				TeamID: teamID, Sku: pf.SKU, DistributorID: distID,
			})
			if err == nil {
				foundPartID = row.ID
				hasInventory = row.InventoryEntryID.Valid
				slog.Debug("part resolve: found by SKU+distributor_id", "item_id", itemIDStr, "part_id", row.ID.String())
			}
		}
	}
	if !foundPartID.Valid && pf.SKU != "" {
		row, err := queries.GetPartBySKUWithInventory(ctx, dbgen.GetPartBySKUWithInventoryParams{
			Sku: pf.SKU, TeamID: teamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
			slog.Debug("part resolve: found by SKU", "item_id", itemIDStr, "part_id", row.ID.String())
		}
	}
	if !foundPartID.Valid && pf.MPN != "" && pf.Manufacturer != "" {
		mpn, mfrName := pf.MPN, q.canonicalManufacturer(pf.Manufacturer)
		row, err := queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
			Mpn: &mpn, Manufacturer: &mfrName, TeamID: teamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
			slog.Debug("part resolve: found by MPN+manufacturer", "item_id", itemIDStr, "part_id", row.ID.String())
		}
	}
	if !foundPartID.Valid && pf.MPN != "" {
		mpn := pf.MPN
		row, err := queries.GetPartByMPNWithInventory(ctx, dbgen.GetPartByMPNWithInventoryParams{
			Mpn: &mpn, TeamID: teamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
			slog.Debug("part resolve: found by MPN", "item_id", itemIDStr, "part_id", row.ID.String())
		}
	}

	if foundPartID.Valid {
		taskType := "known_part_create_entry"
		if hasInventory {
			taskType = "known_part_stock_adjustment"
		}
		slog.Debug("part resolve: known part, skipping API", "item_id", itemIDStr, "part_id", foundPartID.String(), "task_type", taskType)
		queries.UpdatePartResolveItemEnriched(ctx, dbgen.UpdatePartResolveItemEnrichedParams{ //nolint:errcheck
			ID:             itemID,
			TaskType:       &taskType,
			ResolvedPartID: foundPartID,
		})
		q.propagateTaskTypeToBulkScanItem(ctx, queries, item, taskType, foundPartID)
		q.finalizeBOMResolveEnriched(ctx, queries, itemID, teamID, userIDStr)
		q.publishPartResolveChanged(item, userIDStr, teamIDStr)
		return
	}

	// ── Phase 2: distributor API fan-out ─────────────────────────────────────

	if pf.SKU == "" && pf.MPN == "" {
		slog.Debug("part resolve: no identifiers, marking unrecognized", "item_id", itemIDStr)
		taskType := "unrecognized"
		queries.UpdatePartResolveItemEnriched(ctx, dbgen.UpdatePartResolveItemEnrichedParams{ //nolint:errcheck
			ID:       itemID,
			TaskType: &taskType,
		})
		q.propagateTaskTypeToBulkScanItem(ctx, queries, item, taskType, pgtype.UUID{})
		q.publishPartResolveChanged(item, userIDStr, teamIDStr)
		return
	}

	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil || len(configs) == 0 {
		slog.Debug("part resolve: no distributor configs", "item_id", itemIDStr, "err", err)
		q.setResolveItemFailed(ctx, queries, itemID, userIDStr, teamIDStr, item, "no distributor configs available")
		return
	}

	var primaryConfig *dbgen.ListEnabledDistributorConfigsForTeamRow
	for i := range configs {
		if pf.DistributorID != "" && configs[i].DistributorID.String() == pf.DistributorID {
			primaryConfig = &configs[i]
			break
		}
	}

	var primaryResult *distributor.EnrichmentResult
	var primaryDistID string
	var primaryDistName string

	if primaryConfig != nil && pf.SKU != "" && primaryConfig.ApiType != nil {
		client := q.distributors.Lookup(*primaryConfig.ApiType)
		if client != nil {
			slog.Debug("part resolve: calling primary distributor", "item_id", itemIDStr, "distributor", primaryConfig.DistributorName, "sku", pf.SKU)
			enrichCtx, cancel := context.WithTimeout(ctx, enrichmentTimeout)
			primaryResult, err = client.EnrichPart(enrichCtx, pf.SKU, json.RawMessage(primaryConfig.ApiCredentials))
			cancel()
			primaryDistID = primaryConfig.DistributorID.String()
			primaryDistName = primaryConfig.DistributorName
			if primaryResult != nil {
				if pf.MPN == "" {
					pf.MPN = primaryResult.MPN
				}
				if pf.Manufacturer == "" {
					pf.Manufacturer = primaryResult.Manufacturer
				}
				slog.Debug("part resolve: primary returned result", "item_id", itemIDStr, "mpn", primaryResult.MPN, "listings", len(primaryResult.Listings))
			} else {
				slog.Debug("part resolve: primary returned no result", "item_id", itemIDStr, "err", err)
			}
		}
	}

	type distResult struct {
		distID   string
		distName string
		results  []*distributor.EnrichmentResult
		errMsg   string
	}

	var otherResults []distResult

	if pf.MPN != "" {
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i := range configs {
			cfg := &configs[i]
			if (primaryDistID != "" && cfg.DistributorID.String() == primaryDistID) || cfg.ApiType == nil {
				continue
			}
			client := q.distributors.Lookup(*cfg.ApiType)
			if client == nil {
				continue
			}
			wg.Add(1)
			go func(cfg *dbgen.ListEnabledDistributorConfigsForTeamRow, c distributor.Client) {
				defer wg.Done()
				enrichCtx, cancel := context.WithTimeout(ctx, enrichmentTimeout)
				defer cancel()
				results, err := enrichByMPN(enrichCtx, c, pf.MPN, pf.Manufacturer, json.RawMessage(cfg.ApiCredentials), 5, q.mfr)
				dr := distResult{distID: cfg.DistributorID.String(), distName: cfg.DistributorName, results: results}
				if err != nil {
					dr.errMsg = err.Error()
					slog.Debug("part resolve: secondary distributor error", "item_id", itemIDStr, "distributor", cfg.DistributorName, "err", err)
				} else {
					slog.Debug("part resolve: secondary distributor result", "item_id", itemIDStr, "distributor", cfg.DistributorName, "products", len(results))
				}
				mu.Lock()
				otherResults = append(otherResults, dr)
				mu.Unlock()
			}(cfg, client)
		}
		wg.Wait()
	}

	var entries []resolveEnrichmentEntry

	if primaryDistID != "" {
		e := resolveEnrichmentEntry{DistributorID: primaryDistID, Name: primaryDistName}
		if primaryResult != nil {
			e.Results = []*resolveEnrichmentResult{toResolveResult(primaryResult)}
		} else {
			e.Error = "not found"
		}
		entries = append(entries, e)
	}
	for _, dr := range otherResults {
		e := resolveEnrichmentEntry{DistributorID: dr.distID, Name: dr.distName}
		for _, r := range dr.results {
			if r != nil {
				e.Results = append(e.Results, toResolveResult(r))
			}
		}
		if dr.errMsg != "" {
			e.Error = dr.errMsg
		}
		entries = append(entries, e)
	}

	allFailed := len(entries) == 0
	if !allFailed {
		allFailed = true
		for _, e := range entries {
			if len(e.Results) > 0 {
				allFailed = false
				break
			}
		}
	}
	if allFailed {
		slog.Debug("part resolve: all distributors failed", "item_id", itemIDStr)
		q.setResolveItemFailed(ctx, queries, itemID, userIDStr, teamIDStr, item,
			fmt.Sprintf("no distributor returned a result for %s", func() string {
				if pf.SKU != "" {
					return "SKU " + pf.SKU
				}
				return "MPN " + pf.MPN
			}()))
		return
	}

	// Post-API re-check: MPN may have been blank before enrichment resolved it.
	if pf.MPN != "" && !foundPartID.Valid {
		if pf.Manufacturer != "" {
			mpn, mfrName := pf.MPN, q.canonicalManufacturer(pf.Manufacturer)
			row, err := queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
				Mpn: &mpn, Manufacturer: &mfrName, TeamID: teamID,
			})
			if err == nil {
				foundPartID = row.ID
				hasInventory = row.InventoryEntryID.Valid
			}
		}
		if !foundPartID.Valid {
			mpn := pf.MPN
			row, err := queries.GetPartByMPNWithInventory(ctx, dbgen.GetPartByMPNWithInventoryParams{
				Mpn: &mpn, TeamID: teamID,
			})
			if err == nil {
				foundPartID = row.ID
				hasInventory = row.InventoryEntryID.Valid
			}
		}
	}

	taskType := "unknown_part_import"
	if foundPartID.Valid {
		taskType = "known_part_create_entry"
		if hasInventory {
			taskType = "known_part_stock_adjustment"
		}
		slog.Debug("part resolve: part matched via enriched MPN", "item_id", itemIDStr, "mpn", pf.MPN, "task_type", taskType)
	}

	edJSON, _ := json.Marshal(resolveEnrichmentData{Distributors: entries})
	slog.Debug("part resolve: complete", "item_id", itemIDStr, "task_type", taskType, "entries", len(entries))

	queries.UpdatePartResolveItemEnriched(ctx, dbgen.UpdatePartResolveItemEnrichedParams{ //nolint:errcheck
		ID:             itemID,
		TaskType:       &taskType,
		EnrichmentData: edJSON,
		ResolvedPartID: foundPartID,
	})
	q.propagateTaskTypeToBulkScanItem(ctx, queries, item, taskType, foundPartID)
	q.finalizeBOMResolveEnriched(ctx, queries, itemID, teamID, userIDStr)
	q.publishPartResolveChanged(item, userIDStr, teamIDStr)
}

// propagateTaskTypeToBulkScanItem writes the resolved task_type and part_id back
// to the bulk_scan_items row that initiated this resolve job, when applicable.
func (q *Queue) propagateTaskTypeToBulkScanItem(
	ctx context.Context,
	queries *dbgen.Queries,
	item dbgen.PartResolveItem,
	taskType string,
	partID pgtype.UUID,
) {
	if item.SourceType != "bulk_scan_item" || !item.SourceID.Valid {
		return
	}
	queries.UpdateBulkScanItemTaskType(ctx, dbgen.UpdateBulkScanItemTaskTypeParams{ //nolint:errcheck
		ID:       item.SourceID,
		TaskType: &taskType,
		PartID:   partID,
	})
}

func (q *Queue) handlePartResolveRetryDistributors(
	ctx context.Context,
	itemID pgtype.UUID,
	teamID pgtype.UUID,
	itemIDStr, userIDStr, teamIDStr string,
	retryDistributorIDs string,
) {
	slog.Debug("part resolve: partial retry start", "item_id", itemIDStr, "distributor_ids", retryDistributorIDs)

	retrySet := map[string]bool{}
	for _, id := range strings.Split(retryDistributorIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			retrySet[id] = true
		}
	}

	queries := dbgen.New(q.db)
	item, err := queries.GetPartResolveItem(ctx, itemID)
	if err != nil || (item.Status != "enriched" && item.Status != "pending") {
		return
	}

	var pf resolveInputFields
	if len(item.InputFields) > 0 {
		json.Unmarshal(item.InputFields, &pf) //nolint:errcheck
	}
	if pf.MPN == "" {
		slog.Debug("part resolve: partial retry skipped — no MPN", "item_id", itemIDStr)
		return
	}

	var existing resolveEnrichmentData
	if len(item.EnrichmentData) > 0 {
		json.Unmarshal(item.EnrichmentData, &existing) //nolint:errcheck
	}

	// Determine the target manufacturer so the re-query stays constrained to the
	// same Manufacturer+MPN. The original request may have been SKU-only (no
	// manufacturer in input_fields); in that case recover it from the primary
	// distributor's stored result.
	targetMfr := pf.Manufacturer
	if targetMfr == "" {
		targetMfr = primaryManufacturer(existing, pf.DistributorID)
	}

	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil {
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	updated := map[string]resolveEnrichmentEntry{}

	for i := range configs {
		cfg := &configs[i]
		if !retrySet[cfg.DistributorID.String()] || cfg.ApiType == nil {
			continue
		}
		client := q.distributors.Lookup(*cfg.ApiType)
		if client == nil {
			continue
		}
		wg.Add(1)
		go func(cfg *dbgen.ListEnabledDistributorConfigsForTeamRow, c distributor.Client) {
			defer wg.Done()
			enrichCtx, cancel := context.WithTimeout(ctx, enrichmentTimeout)
			defer cancel()
			results, err := enrichByMPN(enrichCtx, c, pf.MPN, targetMfr, json.RawMessage(cfg.ApiCredentials), 5, q.mfr)
			e := resolveEnrichmentEntry{DistributorID: cfg.DistributorID.String(), Name: cfg.DistributorName}
			if err != nil {
				e.Error = err.Error()
			} else if len(results) > 0 {
				for _, r := range results {
					e.Results = append(e.Results, toResolveResult(r))
				}
			} else {
				e.Error = "not found"
			}
			mu.Lock()
			updated[cfg.DistributorID.String()] = e
			mu.Unlock()
		}(cfg, client)
	}
	wg.Wait()

	merged := make([]resolveEnrichmentEntry, 0, len(existing.Distributors))
	for _, e := range existing.Distributors {
		if u, ok := updated[e.DistributorID]; ok {
			merged = append(merged, u)
			delete(updated, e.DistributorID)
		} else {
			merged = append(merged, e)
		}
	}
	for _, u := range updated {
		merged = append(merged, u)
	}

	edJSON, _ := json.Marshal(resolveEnrichmentData{Distributors: merged})
	queries.UpdatePartResolveItemEnriched(ctx, dbgen.UpdatePartResolveItemEnrichedParams{ //nolint:errcheck
		ID:             itemID,
		TaskType:       item.TaskType,
		EnrichmentData: edJSON,
		ResolvedPartID: item.ResolvedPartID,
	})
	q.publishPartResolveChanged(item, userIDStr, teamIDStr)
}

var (
	ErrResolveItemNotReady     = errors.New("resolve item is not in enriched state")
	ErrResolveItemUnrecognized = errors.New("cannot commit an unrecognized item")
	ErrResolveItemConflict     = errors.New("resolve item was already committed")
	ErrResolveItemNoResult     = errors.New("primary distributor has no enrichment result")
)

// CommitResolveItem writes the enrichment results stored in a part_resolve_items row
// into the parts/listings/inventory tables without making any distributor API calls.
// primarySKU is used only to set preferred_listing_id when the original request
// included an explicit distributor_id (i.e. the caller chose a specific SKU).
// initialQty, initialQtyReason, and storageLocationID are optional commit-time
// parameters that are never stored on the resolve item itself.
func (q *Queue) CommitResolveItem(
	ctx context.Context,
	teamID pgtype.UUID,
	resolveItemID pgtype.UUID,
	primaryDistributorID string,
	primarySKU string,
	additionalListings []AdditionalListing,
	userID string,
	initialQty *int32,
	initialQtyReason string,
	storageLocationID pgtype.UUID,
) (pgtype.UUID, error) {
	queries := dbgen.New(q.db)

	item, err := queries.GetPartResolveItem(ctx, resolveItemID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("get resolve item: %w", err)
	}
	if item.TeamID != teamID {
		return pgtype.UUID{}, fmt.Errorf("resolve item belongs to a different team")
	}
	if item.Status == "committed" {
		return pgtype.UUID{}, ErrResolveItemConflict
	}
	if item.Status != "enriched" {
		return pgtype.UUID{}, ErrResolveItemNotReady
	}
	if item.TaskType != nil && *item.TaskType == "unrecognized" {
		return pgtype.UUID{}, ErrResolveItemUnrecognized
	}

	var ed resolveEnrichmentData
	if len(item.EnrichmentData) > 0 {
		if err := json.Unmarshal(item.EnrichmentData, &ed); err != nil {
			return pgtype.UUID{}, fmt.Errorf("parse enrichment_data: %w", err)
		}
	}

	var primaryDistUUID pgtype.UUID
	if err := primaryDistUUID.Scan(primaryDistributorID); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid primary_distributor_id: %w", err)
	}

	tx, err := q.db.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := queries.WithTx(tx)

	var partID pgtype.UUID

	if item.ResolvedPartID.Valid {
		// Known part found in Phase 1 — no API was called, enrichment_data is nil.
		// Persist any available listing data without overriding metadata/parameters.
		partID = item.ResolvedPartID
		for _, entry := range ed.Distributors {
			if len(entry.Results) == 0 {
				continue
			}
			var distID pgtype.UUID
			if distID.Scan(entry.DistributorID) != nil {
				continue
			}
			for _, r := range entry.Results {
				if err := persistEnrichmentResult(ctx, qtx, partID, distID, fromResolveResult(r), false, false); err != nil {
					return pgtype.UUID{}, fmt.Errorf("persist listings for known part: %w", err)
				}
			}
		}
	} else {
		// Unknown part — find primary entry and apply it authoritatively.
		var primaryEntry *resolveEnrichmentEntry
		for i := range ed.Distributors {
			if ed.Distributors[i].DistributorID == primaryDistributorID {
				primaryEntry = &ed.Distributors[i]
				break
			}
		}
		if primaryEntry == nil || len(primaryEntry.Results) == 0 {
			return pgtype.UUID{}, ErrResolveItemNoResult
		}

		// Collect selected SKUs from the primary distributor (primary SKU + any additional
		// listings from the same distributor that the user also included).
		primaryDistSKUs := map[string]bool{primarySKU: true}
		for _, al := range additionalListings {
			if al.DistributorID == primaryDistributorID {
				primaryDistSKUs[al.SKU] = true
			}
		}

		// Find the product within the primary distributor's results that contains the
		// primary SKU. Fall back to first result if not found (shouldn't happen).
		var primaryResultEntry *resolveEnrichmentResult
		for _, r := range primaryEntry.Results {
			for _, l := range r.Listings {
				if l.SKU == primarySKU {
					primaryResultEntry = r
					break
				}
			}
			if primaryResultEntry != nil {
				break
			}
		}
		if primaryResultEntry == nil {
			primaryResultEntry = primaryEntry.Results[0]
		}

		// Filter listings from the selected product to only those the user included.
		filteredPrimary := *primaryResultEntry
		var filteredPrimaryListings []resolveListing
		for _, l := range primaryResultEntry.Listings {
			if primaryDistSKUs[l.SKU] {
				filteredPrimaryListings = append(filteredPrimaryListings, l)
				delete(primaryDistSKUs, l.SKU)
			}
		}
		filteredPrimary.Listings = filteredPrimaryListings

		primaryResult := fromResolveResult(&filteredPrimary)
		partID, err = ApplyEnrichmentResult(ctx, q.db, queries, teamID, primaryDistUUID, primaryResult, nil, nil, q.mfr)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("apply primary enrichment: %w", err)
		}

		// Handle any remaining selected SKUs from other products within the primary
		// distributor (e.g. user included a listing from a different candidate product).
		for _, r := range primaryEntry.Results {
			if r == primaryResultEntry || len(primaryDistSKUs) == 0 {
				continue
			}
			var extra []resolveListing
			for _, l := range r.Listings {
				if primaryDistSKUs[l.SKU] {
					extra = append(extra, l)
					delete(primaryDistSKUs, l.SKU)
				}
			}
			if len(extra) == 0 {
				continue
			}
			fr := *r
			fr.Listings = extra
			if err := persistEnrichmentResult(ctx, qtx, partID, primaryDistUUID, fromResolveResult(&fr), false, false); err != nil {
				slog.Warn("part resolve commit: failed to persist extra primary-dist listing", "err", err)
			}
		}

		// Group additional listings by other distributors.
		type skuSet = map[string]bool
		additionalByDist := map[string]skuSet{}
		for _, al := range additionalListings {
			if al.DistributorID == primaryDistributorID {
				continue
			}
			if additionalByDist[al.DistributorID] == nil {
				additionalByDist[al.DistributorID] = skuSet{}
			}
			additionalByDist[al.DistributorID][al.SKU] = true
		}

		// Persist each additional distributor's selected listings without metadata authority.
		// Listings may span multiple candidate products within the distributor's results.
		for addDistIDStr, skus := range additionalByDist {
			var addEntry *resolveEnrichmentEntry
			for i := range ed.Distributors {
				if ed.Distributors[i].DistributorID == addDistIDStr {
					addEntry = &ed.Distributors[i]
					break
				}
			}
			if addEntry == nil || len(addEntry.Results) == 0 {
				continue
			}
			remaining := make(skuSet, len(skus))
			for k := range skus {
				remaining[k] = true
			}
			for _, r := range addEntry.Results {
				var filtered []resolveListing
				for _, l := range r.Listings {
					if remaining[l.SKU] {
						filtered = append(filtered, l)
						delete(remaining, l.SKU)
					}
				}
				if len(filtered) == 0 {
					continue
				}
				fr := *r
				fr.Listings = filtered
				var addDistID pgtype.UUID
				if addDistID.Scan(addDistIDStr) != nil {
					continue
				}
				if err := persistEnrichmentResult(ctx, queries.WithTx(tx), partID, addDistID, fromResolveResult(&fr), false, false); err != nil {
					slog.Warn("part resolve commit: failed to persist additional listing", "distributor_id", addDistIDStr, "err", err)
				}
			}
		}
	}

	// Determine preferred_listing_id: set only when the caller provided an explicit
	// distributor_id in the original request (meaning the SKU choice was intentional).
	var preferredListingID pgtype.UUID
	var pf resolveInputFields
	if len(item.InputFields) > 0 {
		json.Unmarshal(item.InputFields, &pf) //nolint:errcheck
	}
	if pf.DistributorID != "" && primarySKU != "" {
		listingID, err := queries.GetListingIDByPartAndSKU(ctx, dbgen.GetListingIDByPartAndSKUParams{
			PartID:        partID,
			DistributorID: primaryDistUUID,
			Sku:           primarySKU,
		})
		if err == nil {
			preferredListingID = listingID
		}
	}

	// Ensure an inventory entry exists.
	var entryID pgtype.UUID
	existingEntry, err := queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID, PartID: partID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		newEntry, createErr := qtx.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
			TeamID:             teamID,
			PartID:             partID,
			StorageLocationID:  storageLocationID,
			PreferredListingID: preferredListingID,
		})
		if createErr != nil {
			return pgtype.UUID{}, fmt.Errorf("create inventory entry: %w", createErr)
		}
		entryID = newEntry.ID
	} else if err != nil {
		return pgtype.UUID{}, fmt.Errorf("get inventory entry: %w", err)
	} else {
		entryID = existingEntry.ID
	}

	// Apply optional initial stock adjustment.
	if initialQty != nil && *initialQty != 0 {
		var userUUID pgtype.UUID
		userUUID.Scan(userID) //nolint:errcheck
		reason := initialQtyReason
		if reason == "" {
			reason = "initial_count"
		}
		if _, err := qtx.CreateStockAdjustment(ctx, dbgen.CreateStockAdjustmentParams{
			InventoryEntryID: entryID,
			Delta:            *initialQty,
			Reason:           &reason,
			CreatedBy:        userUUID,
		}); err != nil {
			return pgtype.UUID{}, fmt.Errorf("create stock adjustment: %w", err)
		}
	}

	// Atomic commit: only succeeds if status is still 'enriched'.
	committed, err := qtx.CommitPartResolveItem(ctx, dbgen.CommitPartResolveItemParams{
		ID:             resolveItemID,
		ResolvedPartID: partID,
	})
	if err != nil || !committed.ID.Valid {
		return pgtype.UUID{}, ErrResolveItemConflict
	}

	// BOM linkage: if this resolve item was created for a BOM line item, resolve
	// the line item to the new part and inherit substitutes — all within this tx
	// so the import is atomic. Covers both worker auto-commit and review commit.
	if item.SourceType == "bom_line_item" && item.SourceID.Valid {
		state := "auto_import"
		if _, err := qtx.LinkBOMLineItemPart(ctx, dbgen.LinkBOMLineItemPartParams{
			ResolvedPartID:  partID,
			ResolutionState: &state,
			ID:              item.SourceID,
		}); err != nil {
			return pgtype.UUID{}, fmt.Errorf("link bom line item: %w", err)
		}
		if c, err := qtx.GetBOMLineItemContext(ctx, item.SourceID); err == nil {
			if prior, err := qtx.GetImmediatelyPriorRevision(ctx, dbgen.GetImmediatelyPriorRevisionParams{
				ProjectID:         c.ProjectID,
				CurrentRevisionID: c.BomRevisionID,
			}); err == nil {
				qtx.ImportSubstitutesFromRevision(ctx, dbgen.ImportSubstitutesFromRevisionParams{ //nolint:errcheck
					BomLineItemID: item.SourceID,
					BomRevisionID: prior,
				})
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit tx: %w", err)
	}

	if item.SourceType == "bom_line_item" && item.SourceID.Valid {
		q.publishBOMRevisionChanged(ctx, queries, item.SourceID, teamID.String())
	}

	return partID, nil
}

// publishPartResolveChanged fans out a resolve-state change to the whole team.
// Bulk scanning is inherently cross-user (a phone captures, a desktop reviews),
// so this must NOT be scoped to the enqueuing user — the reviewer is usually a
// different user. (The _ param is the historical actor id, intentionally unused.)
func (q *Queue) publishPartResolveChanged(item dbgen.PartResolveItem, _, teamID string) {
	data, _ := json.Marshal(map[string]any{
		"resolve_item_id": item.ID.String(),
		"source_type":     item.SourceType,
		"source_id": func() any {
			if item.SourceID.Valid {
				return item.SourceID.String()
			}
			return nil
		}(),
	})
	q.Publish(Event{
		Type:   "part_resolve_changed",
		TeamID: teamID,
		Data:   data,
	})
}

func (q *Queue) setResolveItemFailed(ctx context.Context, queries *dbgen.Queries, itemID pgtype.UUID, userID, teamID string, item dbgen.PartResolveItem, msg string) {
	queries.UpdatePartResolveItemFailed(ctx, dbgen.UpdatePartResolveItemFailedParams{ //nolint:errcheck
		ID:           itemID,
		ErrorMessage: &msg,
	})
	// A failed BOM-sourced item can't auto-import; flag it for manual review.
	if item.SourceType == "bom_line_item" && item.SourceID.Valid {
		state := "import_no_result"
		queries.SetBOMLineItemResolutionState(ctx, dbgen.SetBOMLineItemResolutionStateParams{ //nolint:errcheck
			ResolutionState: &state,
			ID:              item.SourceID,
		})
		q.publishBOMRevisionChanged(ctx, queries, item.SourceID, teamID)
	}
	q.publishPartResolveChanged(item, userID, teamID)
}

// finalizeBOMResolveEnriched runs after a resolve item reaches the 'enriched'
// state. For BOM-sourced items it either auto-commits an unambiguous result
// (linking the line item) or flags the line item for manual review.
func (q *Queue) finalizeBOMResolveEnriched(ctx context.Context, queries *dbgen.Queries, itemID, teamID pgtype.UUID, userID string) {
	item, err := queries.GetPartResolveItem(ctx, itemID)
	if err != nil || item.SourceType != "bom_line_item" || !item.SourceID.Valid || item.Status != "enriched" {
		return
	}

	// Known part found in a DB phase: link directly, no part creation needed.
	if item.ResolvedPartID.Valid {
		q.linkBOMKnownPart(ctx, queries, item, teamID)
		return
	}

	var pf resolveInputFields
	if len(item.InputFields) > 0 {
		json.Unmarshal(item.InputFields, &pf) //nolint:errcheck
	}
	var ed resolveEnrichmentData
	if len(item.EnrichmentData) > 0 {
		json.Unmarshal(item.EnrichmentData, &ed) //nolint:errcheck
	}

	primaryDistID, primarySKU, additional, ambiguous := analyzeBOMResolveEnrichment(ed, pf)
	if ambiguous || primarySKU == "" {
		state := "import_ambiguous"
		queries.SetBOMLineItemResolutionState(ctx, dbgen.SetBOMLineItemResolutionStateParams{ //nolint:errcheck
			ResolutionState: &state,
			ID:              item.SourceID,
		})
		q.publishBOMRevisionChanged(ctx, queries, item.SourceID, teamID.String())
		return
	}

	// Unambiguous → auto-commit (creates the part + links the line item).
	if _, err := q.CommitResolveItem(ctx, teamID, itemID, primaryDistID, primarySKU, additional, userID, nil, "", pgtype.UUID{}); err != nil {
		slog.Warn("bom auto-import: commit failed, flagging for review", "resolve_item_id", itemID.String(), "err", err)
		state := "import_ambiguous"
		queries.SetBOMLineItemResolutionState(ctx, dbgen.SetBOMLineItemResolutionStateParams{ //nolint:errcheck
			ResolutionState: &state,
			ID:              item.SourceID,
		})
		q.publishBOMRevisionChanged(ctx, queries, item.SourceID, teamID.String())
	}
}

// linkBOMKnownPart resolves a BOM line item to an already-known part (no part
// creation), ensures an inventory entry, inherits substitutes, and marks the
// resolve item committed.
func (q *Queue) linkBOMKnownPart(ctx context.Context, queries *dbgen.Queries, item dbgen.PartResolveItem, teamID pgtype.UUID) {
	state := "auto_import"
	if _, err := queries.LinkBOMLineItemPart(ctx, dbgen.LinkBOMLineItemPartParams{
		ResolvedPartID:  item.ResolvedPartID,
		ResolutionState: &state,
		ID:              item.SourceID,
	}); err != nil {
		return
	}
	if _, err := queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID, PartID: item.ResolvedPartID,
	}); errors.Is(err, pgx.ErrNoRows) {
		queries.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{ //nolint:errcheck
			TeamID: teamID, PartID: item.ResolvedPartID,
		})
	}
	if c, err := queries.GetBOMLineItemContext(ctx, item.SourceID); err == nil {
		if prior, err := queries.GetImmediatelyPriorRevision(ctx, dbgen.GetImmediatelyPriorRevisionParams{
			ProjectID:         c.ProjectID,
			CurrentRevisionID: c.BomRevisionID,
		}); err == nil {
			queries.ImportSubstitutesFromRevision(ctx, dbgen.ImportSubstitutesFromRevisionParams{ //nolint:errcheck
				BomLineItemID: item.SourceID,
				BomRevisionID: prior,
			})
		}
	}
	queries.CommitPartResolveItem(ctx, dbgen.CommitPartResolveItemParams{ //nolint:errcheck
		ID:             item.ID,
		ResolvedPartID: item.ResolvedPartID,
	})
	q.publishBOMRevisionChanged(ctx, queries, item.SourceID, teamID.String())
}

// analyzeBOMResolveEnrichment decides whether an enriched (new-part) result can
// be auto-committed. When the BOM specified an exact SKU+distributor that listing
// is the primary; otherwise a single distinct candidate product is required.
// Returns the chosen primary listing, all other listings as additional, and an
// ambiguous flag (true → needs manual review).
func analyzeBOMResolveEnrichment(ed resolveEnrichmentData, pf resolveInputFields) (primaryDistID, primarySKU string, additional []AdditionalListing, ambiguous bool) {
	type lst struct{ distID, sku string }
	var all []lst
	products := map[string]bool{}
	for _, e := range ed.Distributors {
		for _, r := range e.Results {
			if r == nil || len(r.Listings) == 0 {
				continue
			}
			products[strings.ToLower(r.MPN+"|"+r.Manufacturer)] = true
			for _, l := range r.Listings {
				all = append(all, lst{e.DistributorID, l.SKU})
			}
		}
	}
	if len(all) == 0 {
		return "", "", nil, true
	}

	// BOM gave an explicit listing → use it as primary if present in the results.
	if pf.SKU != "" && pf.DistributorID != "" {
		for _, x := range all {
			if x.distID == pf.DistributorID && x.sku == pf.SKU {
				primaryDistID, primarySKU = x.distID, x.sku
				break
			}
		}
	}
	// MPN-only (or explicit SKU not found): require a single distinct product.
	if primarySKU == "" {
		if len(products) > 1 {
			return "", "", nil, true
		}
		primaryDistID, primarySKU = all[0].distID, all[0].sku
	}

	for _, x := range all {
		if x.distID == primaryDistID && x.sku == primarySKU {
			continue
		}
		additional = append(additional, AdditionalListing{DistributorID: x.distID, SKU: x.sku})
	}
	return primaryDistID, primarySKU, additional, false
}

// publishBOMRevisionChanged emits a revision_changed event for the revision that
// owns the given line item, so the revision page refreshes its progress bar and
// review surface as resolve items commit.
func (q *Queue) publishBOMRevisionChanged(ctx context.Context, queries *dbgen.Queries, lineItemID pgtype.UUID, teamID string) {
	c, err := queries.GetBOMLineItemContext(ctx, lineItemID)
	if err != nil {
		return
	}
	data, _ := json.Marshal(map[string]string{
		"project_id":  c.ProjectID.String(),
		"revision_id": c.BomRevisionID.String(),
	})
	q.Publish(Event{Type: "revision_changed", TeamID: teamID, Data: data})
}
