package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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

	projectID, err := queries.GetRevisionProjectID(ctx, revID)
	if err != nil {
		q.publishError(job, "failed to get revision project: "+err.Error())
		return
	}

	// Pre-fetch the immediately prior revision for substitute inheritance.
	var priorRevID pgtype.UUID
	if id, err := queries.GetImmediatelyPriorRevision(ctx, dbgen.GetImmediatelyPriorRevisionParams{
		ProjectID:         projectID,
		CurrentRevisionID: revID,
	}); err == nil {
		priorRevID = id
	}

	items, err := queries.ListBOMLineItems(ctx, revID)
	if err != nil {
		q.publishError(job, "failed to list BOM line items: "+err.Error())
		return
	}

	// Count items already resolved before this run.
	resolvedSKU, resolvedMPN, resolvedPrior, resolvedDistributor, ambiguousCount, substitutesImportedCount := 0, 0, 0, 0, 0, 0
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
		"substitutes_copied":      substitutesImportedCount,
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
			"substitutes_copied":      substitutesImportedCount,
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

				// Inherit substitutes from the immediately prior revision (if one exists).
				if priorRevID.Valid {
					if n, err := queries.ImportSubstitutesFromRevision(ctx, dbgen.ImportSubstitutesFromRevisionParams{
						BomLineItemID: item.ID,
						BomRevisionID: priorRevID,
					}); err == nil {
						substitutesImportedCount += int(n)
					}
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
	revData, _ := json.Marshal(map[string]string{"project_id": payload["project_id"], "revision_id": payload["bom_revision_id"]})
	q.Publish(Event{Type: "revision_changed", TeamID: job.TeamID, Data: revData})
}
