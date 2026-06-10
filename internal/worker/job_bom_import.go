package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
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

	// Parsed once; used when creating part_resolve_items for new parts.
	var createdBy pgtype.UUID
	_ = createdBy.Scan(payload["user_id"])

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
	resolvedSKU, resolvedMPN, resolvedPrior, ambiguousCount, substitutesImportedCount, importingCount := 0, 0, 0, 0, 0, 0
	for _, it := range items {
		switch {
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_sku":
			resolvedSKU++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_mpn":
			resolvedMPN++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_prior_revision":
			resolvedPrior++
		case it.ResolutionState != nil && *it.ResolutionState == "ambiguous":
			ambiguousCount++
		case it.ResolutionState != nil && *it.ResolutionState == "importing":
			importingCount++
		}
	}

	publishProgress := func(status string, noMatch int) {
		q.publishProgress(job, "bom_import_progress", map[string]any{
			"status":                  status,
			"resolved":                resolvedSKU + resolvedMPN + resolvedPrior,
			"resolved_sku":            resolvedSKU,
			"resolved_mpn":            resolvedMPN,
			"resolved_prior_revision": resolvedPrior,
			"ambiguous":               ambiguousCount,
			"importing":               importingCount,
			"no_match":                noMatch,
			"total":                   len(items),
			"job_id":                  job.ID,
			"substitutes_copied":      substitutesImportedCount,
		})
	}

	publishProgress("progress", 0)

	strPtr := func(s string) *string { return &s }

	noMatchCount := 0
	lastPublish := time.Now()

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
			// Not known to partrack. If we have identifiers, hand the new part off to
			// the unified resolve pipeline (multi-distributor fan-out + cached
			// enrichment). The resolve worker auto-commits unambiguous results and
			// links the line item; ambiguous/failed ones surface in the review UI.
			hasSKU := item.Sku != nil && *item.Sku != "" && item.ResolvedDistributorID.Valid
			hasMPN := item.Mpn != nil && *item.Mpn != ""
			if hasSKU || hasMPN {
				inputFields, _ := json.Marshal(map[string]string{
					"sku":            derefStr(item.Sku),
					"distributor_id": uuidStr(item.ResolvedDistributorID),
					"mpn":            derefStr(item.Mpn),
					"manufacturer":   derefStr(item.Manufacturer),
				})
				ri, err := queries.CreatePartResolveItem(ctx, dbgen.CreatePartResolveItemParams{
					TeamID:      teamID,
					SourceType:  "bom_line_item",
					SourceID:    item.ID,
					InputFields: inputFields,
					CreatedBy:   createdBy,
				})
				if err == nil {
					importingState := "importing"
					if err := queries.SetBOMLineItemResolveItem(ctx, dbgen.SetBOMLineItemResolveItemParams{
						ResolveItemID:   ri.ID,
						ResolutionState: &importingState,
						ID:              item.ID,
					}); err == nil {
						q.Enqueue(JobPartResolve, teamID.String(), map[string]string{
							"resolve_item_id": ri.ID.String(),
							"team_id":         teamID.String(),
							"user_id":         payload["user_id"],
						})
						importingCount++
					} else {
						noMatchCount++
					}
				} else {
					noMatchCount++
				}
			} else {
				// No usable identifiers — leave unresolved for manual review.
				noMatchCount++
			}
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
			publishProgress("progress", noMatchCount)
			lastPublish = time.Now()
		}
	}

	publishProgress("complete", noMatchCount)

	// Team-feed rollup: one bom.imported milestone carrying the resolution stats,
	// instead of N per-part rows (per-part creation audit lives on parts.created_by).
	if q.activity != nil {
		label := ""
		if rev, err := queries.GetRevision(ctx, dbgen.GetRevisionParams{ID: revID, ProjectID: projectID}); err == nil {
			label = rev.Label
		}
		resolved := resolvedSKU + resolvedMPN + resolvedPrior
		q.activity.Record(activity.Event{
			TeamID:  teamID,
			ActorID: createdBy,
			Source:  activity.SourceJob,
			Action:  "bom.imported",
			Feed:    activity.FeedTeam,
			Subject: &activity.Ref{
				Type:   "revision",
				ID:     revID.String(),
				Label:  label,
				Parent: &activity.Ref{Type: "project", ID: projectID.String()},
			},
			Values: map[string]any{
				"total": len(items), "resolved": resolved, "resolved_sku": resolvedSKU,
				"resolved_mpn": resolvedMPN, "importing": importingCount,
				"ambiguous": ambiguousCount, "no_match": noMatchCount,
				"substitutes_copied": substitutesImportedCount,
			},
			Summary: fmt.Sprintf("Imported BOM %s: %d lines, %d resolved, %d importing, %d ambiguous",
				label, len(items), resolved, importingCount, ambiguousCount),
		})
	}

	projData, _ := json.Marshal(map[string]string{"project_id": payload["project_id"]})
	q.Publish(Event{Type: "project_changed", TeamID: job.TeamID, UserID: payload["user_id"], Data: projData})
	revData, _ := json.Marshal(map[string]string{"project_id": payload["project_id"], "revision_id": payload["bom_revision_id"]})
	q.Publish(Event{Type: "revision_changed", TeamID: job.TeamID, Data: revData})
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return u.String()
}
