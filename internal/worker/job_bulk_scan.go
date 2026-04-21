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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const enrichmentTimeout = 30 * time.Second

// enrichmentDataEntry is one distributor's result stored in bulk_scan_items.enrichment_data.
type enrichmentDataEntry struct {
	DistributorID string                    `json:"distributor_id"`
	Name          string                    `json:"name"`
	Result        *serialisableEnrichment   `json:"result,omitempty"`
	Error         string                    `json:"error,omitempty"`
}

// serialisableEnrichment is a JSON-friendly projection of distributor.EnrichmentResult.
type serialisableEnrichment struct {
	MPN              string                       `json:"mpn"`
	Manufacturer     string                       `json:"manufacturer"`
	Description      string                       `json:"description"`
	ShortDescription string                       `json:"short_description"`
	Package          string                       `json:"package"`
	DatasheetURL     string                       `json:"datasheet_url"`
	ProductURL       string                       `json:"product_url"`
	ImageURL         string                       `json:"image_url"`
	LifecycleStatus  string                       `json:"lifecycle_status"`
	Listings         []serialisableListing        `json:"listings"`
}

type serialisableListing struct {
	SKU            string                      `json:"sku"`
	PackagingType  string                      `json:"packaging_type"`
	UnitPrice      string                      `json:"unit_price"`
	StockAvailable *int32                      `json:"stock_available"`
	Moq            int32                       `json:"moq"`
	PriceBreaks    []distributor.PriceBreak    `json:"price_breaks"`
}

func toSerialisableEnrichment(r *distributor.EnrichmentResult) *serialisableEnrichment {
	if r == nil {
		return nil
	}
	se := &serialisableEnrichment{
		MPN:              r.MPN,
		Manufacturer:     r.Manufacturer,
		Description:      r.Description,
		ShortDescription: r.ShortDescription,
		Package:          r.Package,
		DatasheetURL:     r.DatasheetURL,
		ProductURL:       r.ProductURL,
		ImageURL:         r.ImageURL,
		LifecycleStatus:  string(r.LifecycleStatus),
		Listings:         make([]serialisableListing, 0, len(r.Listings)),
	}
	for _, l := range r.Listings {
		se.Listings = append(se.Listings, serialisableListing{
			SKU:            l.SKU,
			PackagingType:  l.PackagingType,
			UnitPrice:      l.UnitPrice,
			StockAvailable: l.StockAvailable,
			Moq:            l.Moq,
			PriceBreaks:    l.PriceBreaks,
		})
	}
	return se
}

func (q *Queue) handleBulkScanEnrich(job Job) {
	ctx := context.Background()

	payload, ok := job.Payload.(map[string]string)
	if !ok {
		return
	}
	itemIDStr := payload["item_id"]
	sessionIDStr := payload["session_id"]
	userIDStr := payload["user_id"]
	teamIDStr := payload["team_id"]

	// Optional: comma-separated distributor UUIDs to retry selectively.
	retryDistributorIDs := payload["retry_distributor_ids"]

	var itemID, sessionID, teamID pgtype.UUID
	if itemID.Scan(itemIDStr) != nil || sessionID.Scan(sessionIDStr) != nil || teamID.Scan(teamIDStr) != nil {
		return
	}

	if retryDistributorIDs != "" {
		q.handleBulkScanRetryDistributors(ctx, itemID, sessionIDStr, userIDStr, teamIDStr, retryDistributorIDs)
		return
	}

	queries := dbgen.New(q.db)

	item, err := queries.GetBulkScanItem(ctx, itemID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			q.setBulkItemError(ctx, queries, itemID, sessionIDStr, userIDStr, teamIDStr, "item not found")
		}
		return
	}

	// Only process pending items (skip if already enriched/reviewed/dismissed).
	if item.Status != "pending_enrichment" {
		slog.Debug("bulk enrich: skipping non-pending item", "item_id", itemIDStr, "status", item.Status)
		return
	}

	// Parse stored parsed_fields to recover barcode identifiers.
	var pf struct {
		Distributor   string `json:"distributor"`
		DistributorID string `json:"distributor_id"`
		SKU           string `json:"sku"`
		MPN           string `json:"mpn"`
		Manufacturer  string `json:"manufacturer"`
	}
	if len(item.ParsedFields) > 0 {
		json.Unmarshal(item.ParsedFields, &pf) //nolint:errcheck
	}
	slog.Debug("bulk enrich: start", "item_id", itemIDStr, "session_id", sessionIDStr, "distributor", pf.Distributor, "distributor_id", pf.DistributorID, "sku", pf.SKU, "mpn", pf.MPN, "manufacturer", pf.Manufacturer)

	// ── Phase 1: synchronous DB lookup ────────────────────────────────────────

	var foundPartID pgtype.UUID
	var hasInventory bool

	if pf.SKU != "" && pf.Distributor != "" {
		row, err := queries.GetPartBySKUAndDistributorWithInventory(ctx, dbgen.GetPartBySKUAndDistributorWithInventoryParams{
			Sku: pf.SKU, TeamID: teamID, DistributorName: pf.Distributor,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
			slog.Debug("bulk enrich: found by SKU+distributor", "item_id", itemIDStr, "sku", pf.SKU, "distributor", pf.Distributor, "part_id", row.ID.String(), "has_inventory", hasInventory)
		}
	}
	if !foundPartID.Valid && pf.SKU != "" {
		row, err := queries.GetPartBySKUWithInventory(ctx, dbgen.GetPartBySKUWithInventoryParams{
			Sku: pf.SKU, TeamID: teamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
			slog.Debug("bulk enrich: found by SKU", "item_id", itemIDStr, "sku", pf.SKU, "part_id", row.ID.String(), "has_inventory", hasInventory)
		}
	}
	if !foundPartID.Valid && pf.MPN != "" && pf.Manufacturer != "" {
		row, err := queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
			Mpn: &pf.MPN, Manufacturer: &pf.Manufacturer, TeamID: teamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
			slog.Debug("bulk enrich: found by MPN+manufacturer", "item_id", itemIDStr, "mpn", pf.MPN, "manufacturer", pf.Manufacturer, "part_id", row.ID.String(), "has_inventory", hasInventory)
		}
	}
	if !foundPartID.Valid && pf.MPN != "" {
		row, err := queries.GetPartByMPNWithInventory(ctx, dbgen.GetPartByMPNWithInventoryParams{
			Mpn: &pf.MPN, TeamID: teamID,
		})
		if err == nil {
			foundPartID = row.ID
			hasInventory = row.InventoryEntryID.Valid
			slog.Debug("bulk enrich: found by MPN", "item_id", itemIDStr, "mpn", pf.MPN, "part_id", row.ID.String(), "has_inventory", hasInventory)
		}
	}

	if foundPartID.Valid {
		// Part is known — resolve task type and mark enriched without API calls.
		taskType := "known_part_create_entry"
		if hasInventory {
			taskType = "known_part_stock_adjustment"
		}
		slog.Debug("bulk enrich: known part, skipping API", "item_id", itemIDStr, "part_id", foundPartID.String(), "task_type", taskType)
		_, err := queries.SetBulkScanItemEnriched(ctx, dbgen.SetBulkScanItemEnrichedParams{
			ID:       itemID,
			TaskType: &taskType,
			PartID:   foundPartID,
		})
		if err != nil {
			q.setBulkItemError(ctx, queries, itemID, sessionIDStr, userIDStr, teamIDStr, "failed to update item: "+err.Error())
			return
		}
		q.publishBulkScanChanged(sessionIDStr, itemIDStr, userIDStr, teamIDStr)
		return
	}

	// ── Phase 2: distributor API calls ────────────────────────────────────────

	// If barcode was unrecognised or had no useful fields, mark as unrecognized.
	if pf.SKU == "" && pf.MPN == "" {
		slog.Debug("bulk enrich: no identifiers extracted, marking unrecognized", "item_id", itemIDStr)
		taskType := "unrecognized_barcode"
		queries.SetBulkScanItemEnriched(ctx, dbgen.SetBulkScanItemEnrichedParams{ //nolint:errcheck
			ID:       itemID,
			TaskType: &taskType,
		})
		q.publishBulkScanChanged(sessionIDStr, itemIDStr, userIDStr, teamIDStr)
		return
	}

	// Also handle LCSC and other distributors not supported for enrichment by
	// checking if the barcode was completely unparseable (ParsedFields nil/empty).
	if len(item.ParsedFields) == 0 {
		slog.Debug("bulk enrich: empty parsed_fields, marking unrecognized", "item_id", itemIDStr)
		taskType := "unrecognized_barcode"
		queries.SetBulkScanItemEnriched(ctx, dbgen.SetBulkScanItemEnrichedParams{ //nolint:errcheck
			ID:       itemID,
			TaskType: &taskType,
		})
		q.publishBulkScanChanged(sessionIDStr, itemIDStr, userIDStr, teamIDStr)
		return
	}

	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil || len(configs) == 0 {
		slog.Debug("bulk enrich: no distributor configs available", "item_id", itemIDStr, "err", err)
		q.setBulkItemError(ctx, queries, itemID, sessionIDStr, userIDStr, teamIDStr, "no distributor configs available")
		return
	}
	slog.Debug("bulk enrich: distributor configs loaded", "item_id", itemIDStr, "count", len(configs))

	// Find the config for the scanned distributor — prefer UUID match (set at
	// scan time), fall back to exact name match for older items.
	var primaryConfig *dbgen.ListEnabledDistributorConfigsForTeamRow
	for i := range configs {
		if pf.DistributorID != "" && configs[i].DistributorID.String() == pf.DistributorID {
			primaryConfig = &configs[i]
			break
		}
		if pf.DistributorID == "" && configs[i].DistributorName == pf.Distributor {
			primaryConfig = &configs[i]
			break
		}
	}
	if primaryConfig != nil {
		slog.Debug("bulk enrich: primary distributor matched", "item_id", itemIDStr, "distributor", primaryConfig.DistributorName, "match_by", func() string {
			if pf.DistributorID != "" {
				return "uuid"
			}
			return "name"
		}())
	} else {
		slog.Debug("bulk enrich: no primary distributor match", "item_id", itemIDStr, "parsed_distributor", pf.Distributor, "parsed_distributor_id", pf.DistributorID)
	}

	// Step 5: Call EnrichPart on the scanned distributor (if we have a SKU and config).
	var primaryResult *distributor.EnrichmentResult
	var primaryDistID string
	var primaryDistName string

	if primaryConfig != nil && pf.SKU != "" && primaryConfig.ApiType != nil {
		client := q.distributors.Lookup(*primaryConfig.ApiType)
		if client != nil {
			slog.Debug("bulk enrich: calling primary distributor API", "item_id", itemIDStr, "distributor", primaryConfig.DistributorName, "sku", pf.SKU)
			enrichCtx, cancel := context.WithTimeout(ctx, enrichmentTimeout)
			var enrichErr error
			primaryResult, enrichErr = client.EnrichPart(enrichCtx, pf.SKU, json.RawMessage(primaryConfig.ApiCredentials))
			cancel()
			primaryDistID = primaryConfig.DistributorID.String()
			primaryDistName = primaryConfig.DistributorName
			if primaryResult != nil {
				slog.Debug("bulk enrich: primary distributor returned result", "item_id", itemIDStr, "distributor", primaryDistName, "mpn", primaryResult.MPN, "listings", len(primaryResult.Listings))
			} else {
				slog.Debug("bulk enrich: primary distributor returned no result", "item_id", itemIDStr, "distributor", primaryDistName, "err", enrichErr)
			}
			// Update MPN/manufacturer from enrichment if not in barcode.
			if primaryResult != nil {
				if pf.MPN == "" {
					pf.MPN = primaryResult.MPN
				}
				if pf.Manufacturer == "" {
					pf.Manufacturer = primaryResult.Manufacturer
				}
			}
		}
	}

	// Step 6: Query all OTHER enabled distributors in parallel if we have an MPN.
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
			// Skip the primary distributor only if it was actually called (had a SKU).
			// If it was identified as primary but had no SKU, include it here so it
			// gets searched by MPN alongside the others.
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
				dr := distResult{
					distID:   cfg.DistributorID.String(),
					distName: cfg.DistributorName,
					result:   result,
				}
				if err != nil {
					dr.errMsg = err.Error()
					slog.Debug("bulk enrich: secondary distributor error", "item_id", itemIDStr, "distributor", cfg.DistributorName, "mpn", pf.MPN, "err", err)
				} else if result != nil {
					slog.Debug("bulk enrich: secondary distributor returned result", "item_id", itemIDStr, "distributor", cfg.DistributorName, "mpn", pf.MPN, "listings", len(result.Listings))
				} else {
					slog.Debug("bulk enrich: secondary distributor returned no result", "item_id", itemIDStr, "distributor", cfg.DistributorName, "mpn", pf.MPN)
				}
				mu.Lock()
				otherResults = append(otherResults, dr)
				mu.Unlock()
			}(cfg, client)
		}
		wg.Wait()
	}

	// Aggregate all distributor results.
	var entries []enrichmentDataEntry

	// Primary distributor (if called).
	if primaryDistID != "" {
		e := enrichmentDataEntry{
			DistributorID: primaryDistID,
			Name:          primaryDistName,
		}
		if primaryResult != nil {
			e.Result = toSerialisableEnrichment(primaryResult)
		} else {
			e.Error = "not found"
		}
		entries = append(entries, e)
	}

	for _, dr := range otherResults {
		e := enrichmentDataEntry{
			DistributorID: dr.distID,
			Name:          dr.distName,
		}
		if dr.result != nil {
			e.Result = toSerialisableEnrichment(dr.result)
		}
		if dr.errMsg != "" {
			e.Error = dr.errMsg
		}
		entries = append(entries, e)
	}

	// Check if all distributors failed.
	allFailed := true
	for _, e := range entries {
		if e.Result != nil {
			allFailed = false
			break
		}
	}
	if len(entries) == 0 {
		allFailed = true
	}

	if allFailed {
		slog.Debug("bulk enrich: all distributors failed", "item_id", itemIDStr, "sku", pf.SKU, "mpn", pf.MPN)
		q.setBulkItemError(ctx, queries, itemID, sessionIDStr, userIDStr, teamIDStr,
			fmt.Sprintf("enrichment failed: no distributor returned a result for %s", func() string {
				if pf.SKU != "" {
					return "SKU " + pf.SKU
				}
				return "MPN " + pf.MPN
			}()))
		return
	}

	// Store enrichment_data and mark item as enriched.
	enrichmentData := map[string]any{"distributors": entries}
	edJSON, _ := json.Marshal(enrichmentData)

	taskType := "unknown_part_import"

	// Re-check part existence using MPN obtained from enrichment (if barcode didn't have it).
	if pf.MPN != "" && !foundPartID.Valid {
		if pf.Manufacturer != "" {
			row, err := queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
				Mpn: &pf.MPN, Manufacturer: &pf.Manufacturer, TeamID: teamID,
			})
			if err == nil {
				foundPartID = row.ID
				hasInventory = row.InventoryEntryID.Valid
			}
		}
		if !foundPartID.Valid {
			row, err := queries.GetPartByMPNWithInventory(ctx, dbgen.GetPartByMPNWithInventoryParams{
				Mpn: &pf.MPN, TeamID: teamID,
			})
			if err == nil {
				foundPartID = row.ID
				hasInventory = row.InventoryEntryID.Valid
			}
		}
		if foundPartID.Valid {
			taskType = "known_part_create_entry"
			if hasInventory {
				taskType = "known_part_stock_adjustment"
			}
			slog.Debug("bulk enrich: part matched via enriched MPN", "item_id", itemIDStr, "mpn", pf.MPN, "manufacturer", pf.Manufacturer, "part_id", foundPartID.String(), "task_type", taskType)
		}
	}
	slog.Debug("bulk enrich: complete", "item_id", itemIDStr, "task_type", taskType, "part_id", foundPartID.String())

	_, err = queries.SetBulkScanItemEnriched(ctx, dbgen.SetBulkScanItemEnrichedParams{
		ID:             itemID,
		TaskType:       &taskType,
		PartID:         foundPartID,
		EnrichmentData: edJSON,
	})
	if err != nil {
		q.setBulkItemError(ctx, queries, itemID, sessionIDStr, userIDStr, teamIDStr, "failed to save enrichment: "+err.Error())
		return
	}

	q.publishBulkScanChanged(sessionIDStr, itemIDStr, userIDStr, teamIDStr)
}

// handleBulkScanRetryDistributors re-queries a specific set of distributors
// (by UUID, comma-separated) for an already-enriched item and merges the new
// results into the existing enrichment_data.
func (q *Queue) handleBulkScanRetryDistributors(ctx context.Context, itemID pgtype.UUID, sessionIDStr, userIDStr, teamIDStr, retryDistributorIDs string) {
	itemIDStr := itemID.String()
	slog.Debug("bulk enrich: partial retry start", "item_id", itemIDStr, "distributor_ids", retryDistributorIDs)

	// Build a set of the requested distributor UUIDs.
	retrySet := map[string]bool{}
	for _, id := range strings.Split(retryDistributorIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			retrySet[id] = true
		}
	}

	var teamID pgtype.UUID
	if teamID.Scan(teamIDStr) != nil {
		return
	}

	queries := dbgen.New(q.db)
	item, err := queries.GetBulkScanItem(ctx, itemID)
	if err != nil || (item.Status != "enriched" && item.Status != "pending_enrichment") {
		return
	}

	// Recover MPN from parsed_fields.
	var pf struct {
		MPN          string `json:"mpn"`
		Manufacturer string `json:"manufacturer"`
	}
	if len(item.ParsedFields) > 0 {
		json.Unmarshal(item.ParsedFields, &pf) //nolint:errcheck
	}
	if pf.MPN == "" {
		slog.Debug("bulk enrich: partial retry skipped — no MPN", "item_id", itemIDStr)
		return
	}

	// Load existing enrichment_data.
	var existing struct {
		Distributors []enrichmentDataEntry `json:"distributors"`
	}
	if len(item.EnrichmentData) > 0 {
		json.Unmarshal(item.EnrichmentData, &existing) //nolint:errcheck
	}

	// Load distributor configs and filter to the requested set.
	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil {
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	updated := map[string]enrichmentDataEntry{}

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
			e := enrichmentDataEntry{DistributorID: cfg.DistributorID.String(), Name: cfg.DistributorName}
			if err != nil {
				e.Error = err.Error()
				slog.Debug("bulk enrich: partial retry distributor error", "item_id", itemIDStr, "distributor", cfg.DistributorName, "err", err)
			} else if result != nil {
				e.Result = toSerialisableEnrichment(result)
				slog.Debug("bulk enrich: partial retry distributor result", "item_id", itemIDStr, "distributor", cfg.DistributorName, "listings", len(result.Listings))
			} else {
				e.Error = "not found"
				slog.Debug("bulk enrich: partial retry distributor no result", "item_id", itemIDStr, "distributor", cfg.DistributorName)
			}
			mu.Lock()
			updated[cfg.DistributorID.String()] = e
			mu.Unlock()
		}(cfg, client)
	}
	wg.Wait()

	// Merge: replace retried entries, keep everything else.
	merged := make([]enrichmentDataEntry, 0, len(existing.Distributors))
	for _, e := range existing.Distributors {
		if u, ok := updated[e.DistributorID]; ok {
			merged = append(merged, u)
			delete(updated, e.DistributorID)
		} else {
			merged = append(merged, e)
		}
	}
	// Append any retried distributors that weren't in the existing list.
	for _, u := range updated {
		merged = append(merged, u)
	}

	edJSON, _ := json.Marshal(map[string]any{"distributors": merged})
	if _, err := queries.UpdateBulkScanItemEnrichmentData(ctx, dbgen.UpdateBulkScanItemEnrichmentDataParams{
		ID:             itemID,
		EnrichmentData: edJSON,
	}); err != nil {
		slog.Debug("bulk enrich: partial retry failed to save", "item_id", itemIDStr, "err", err)
		return
	}

	q.publishBulkScanChanged(sessionIDStr, itemIDStr, userIDStr, teamIDStr)
}

func (q *Queue) setBulkItemError(ctx context.Context, queries *dbgen.Queries, itemID pgtype.UUID, sessionID, userID, teamID, msg string) {
	errMsg := msg
	queries.SetBulkScanItemError(ctx, dbgen.SetBulkScanItemErrorParams{ //nolint:errcheck
		ID:           itemID,
		ErrorMessage: &errMsg,
	})
	q.publishBulkScanChanged(sessionID, itemID.String(), userID, teamID)
}

func (q *Queue) publishBulkScanChanged(sessionID, itemID, userID, teamID string) {
	d, _ := json.Marshal(map[string]string{
		"session_id": sessionID,
		"item_id":    itemID,
	})
	q.Publish(Event{
		Type:         "bulk_scan_changed",
		TeamID:       teamID,
		TargetUserID: userID,
		Data:         d,
	})
}

