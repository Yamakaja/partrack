package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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
		mpn, mfr := pf.MPN, pf.Manufacturer
		row, err := queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
			Mpn: &mpn, Manufacturer: &mfr, TeamID: teamID,
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
		result   *distributor.EnrichmentResult
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
				result, err := c.EnrichPart(enrichCtx, pf.MPN, json.RawMessage(cfg.ApiCredentials))
				dr := distResult{distID: cfg.DistributorID.String(), distName: cfg.DistributorName, result: result}
				if err != nil {
					dr.errMsg = err.Error()
					slog.Debug("part resolve: secondary distributor error", "item_id", itemIDStr, "distributor", cfg.DistributorName, "err", err)
				} else if result != nil {
					slog.Debug("part resolve: secondary distributor result", "item_id", itemIDStr, "distributor", cfg.DistributorName, "listings", len(result.Listings))
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
			e.Result = toResolveResult(primaryResult)
		} else {
			e.Error = "not found"
		}
		entries = append(entries, e)
	}
	for _, dr := range otherResults {
		e := resolveEnrichmentEntry{DistributorID: dr.distID, Name: dr.distName}
		if dr.result != nil {
			e.Result = toResolveResult(dr.result)
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
			if e.Result != nil {
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
			mpn, mfr := pf.MPN, pf.Manufacturer
			row, err := queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
				Mpn: &mpn, Manufacturer: &mfr, TeamID: teamID,
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
	q.publishPartResolveChanged(item, userIDStr, teamIDStr)
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
			result, err := c.EnrichPart(enrichCtx, pf.MPN, json.RawMessage(cfg.ApiCredentials))
			e := resolveEnrichmentEntry{DistributorID: cfg.DistributorID.String(), Name: cfg.DistributorName}
			if err != nil {
				e.Error = err.Error()
			} else if result != nil {
				e.Result = toResolveResult(result)
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
	queries.UpdatePartResolveItemEnrichmentData(ctx, dbgen.UpdatePartResolveItemEnrichmentDataParams{ //nolint:errcheck
		ID:             itemID,
		EnrichmentData: edJSON,
	})
	q.publishPartResolveChanged(item, userIDStr, teamIDStr)
}

var (
	ErrResolveItemNotReady    = errors.New("resolve item is not in enriched state")
	ErrResolveItemUnrecognized = errors.New("cannot commit an unrecognized item")
	ErrResolveItemConflict    = errors.New("resolve item was already committed")
	ErrResolveItemNoResult    = errors.New("primary distributor has no enrichment result")
)

// CommitResolveItem writes the enrichment results stored in a part_resolve_items row
// into the parts/listings/inventory tables without making any distributor API calls.
// primarySKU is used only to set preferred_listing_id when the original request
// included an explicit distributor_id (i.e. the caller chose a specific SKU).
func (q *Queue) CommitResolveItem(
	ctx context.Context,
	teamID pgtype.UUID,
	resolveItemID pgtype.UUID,
	primaryDistributorID string,
	primarySKU string,
	additionalDistributorIDs []string,
	userID string,
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
			if entry.Result == nil {
				continue
			}
			var distID pgtype.UUID
			if distID.Scan(entry.DistributorID) != nil {
				continue
			}
			result := fromResolveResult(entry.Result)
			if err := persistEnrichmentResult(ctx, qtx, partID, distID, result, false, false); err != nil {
				return pgtype.UUID{}, fmt.Errorf("persist listings for known part: %w", err)
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
		if primaryEntry == nil || primaryEntry.Result == nil {
			return pgtype.UUID{}, ErrResolveItemNoResult
		}

		primaryResult := fromResolveResult(primaryEntry.Result)
		partID, err = ApplyEnrichmentResult(ctx, q.db, queries, teamID, primaryDistUUID, primaryResult, nil, nil)
		if err != nil {
			return pgtype.UUID{}, fmt.Errorf("apply primary enrichment: %w", err)
		}

		// Persist additional distributors' listings without metadata authority.
		for _, addDistIDStr := range additionalDistributorIDs {
			if addDistIDStr == primaryDistributorID {
				continue
			}
			var addEntry *resolveEnrichmentEntry
			for i := range ed.Distributors {
				if ed.Distributors[i].DistributorID == addDistIDStr {
					addEntry = &ed.Distributors[i]
					break
				}
			}
			if addEntry == nil || addEntry.Result == nil {
				continue
			}
			var addDistID pgtype.UUID
			if addDistID.Scan(addDistIDStr) != nil {
				continue
			}
			addResult := fromResolveResult(addEntry.Result)
			// Reuse the same transaction-scoped queries.
			qtxForAdd := queries.WithTx(tx)
			if err := persistEnrichmentResult(ctx, qtxForAdd, partID, addDistID, addResult, false, false); err != nil {
				slog.Warn("part resolve commit: failed to persist additional distributor", "distributor_id", addDistIDStr, "err", err)
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
	_, err = queries.GetInventoryEntryByPartID(ctx, dbgen.GetInventoryEntryByPartIDParams{
		TeamID: teamID, PartID: partID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err = qtx.CreateInventoryEntry(ctx, dbgen.CreateInventoryEntryParams{
			TeamID:             teamID,
			PartID:             partID,
			PreferredListingID: preferredListingID,
		}); err != nil {
			return pgtype.UUID{}, fmt.Errorf("create inventory entry: %w", err)
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

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit tx: %w", err)
	}

	return partID, nil
}

func (q *Queue) publishPartResolveChanged(item dbgen.PartResolveItem, userID, teamID string) {
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
		Type:         "part_resolve_changed",
		TeamID:       teamID,
		TargetUserID: userID,
		Data:         data,
	})
}

func (q *Queue) setResolveItemFailed(ctx context.Context, queries *dbgen.Queries, itemID pgtype.UUID, userID, teamID string, item dbgen.PartResolveItem, msg string) {
	queries.UpdatePartResolveItemFailed(ctx, dbgen.UpdatePartResolveItemFailedParams{ //nolint:errcheck
		ID:           itemID,
		ErrorMessage: &msg,
	})
	q.publishPartResolveChanged(item, userID, teamID)
}
