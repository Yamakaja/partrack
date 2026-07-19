package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/partmatch"
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
	resolvedSKU, resolvedMPN, resolvedPrior, resolvedGeneric, ambiguousCount, substitutesImportedCount, importingCount := 0, 0, 0, 0, 0, 0, 0
	for _, it := range items {
		switch {
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_sku":
			resolvedSKU++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && (*it.ResolutionState == "auto_mpn" || *it.ResolutionState == "auto_mpn_fuzzy"):
			resolvedMPN++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_prior_revision":
			resolvedPrior++
		case it.ResolvedPartID.Valid && it.ResolutionState != nil && *it.ResolutionState == "auto_generic":
			resolvedGeneric++
		case it.ResolutionState != nil && *it.ResolutionState == "ambiguous":
			ambiguousCount++
		case it.ResolutionState != nil && *it.ResolutionState == "importing":
			importingCount++
		}
	}

	publishProgress := func(status string, noMatch int) {
		q.publishProgress(job, "bom_import_progress", map[string]any{
			"status":                  status,
			"resolved":                resolvedSKU + resolvedMPN + resolvedPrior + resolvedGeneric,
			"resolved_sku":            resolvedSKU,
			"resolved_mpn":            resolvedMPN,
			"resolved_prior_revision": resolvedPrior,
			"resolved_generic":        resolvedGeneric,
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

	// collapseOrigin folds a prior-revision reuse into a single hop. If the line we
	// are reusing had itself inherited its part, we adopt its origin rather than
	// pointing at it — otherwise a project that re-imports an unchanged BOM builds a
	// chain of "matched to last revision" that says nothing about the decision that
	// actually chose the part. Otherwise the source line's own state is the origin.
	collapseOrigin := func(srcRevID pgtype.UUID, srcState, srcOriginState *string, srcOriginRevID pgtype.UUID) (*string, pgtype.UUID) {
		if srcState != nil && *srcState == "auto_prior_revision" && srcOriginState != nil {
			return srcOriginState, srcOriginRevID
		}
		return srcState, srcRevID
	}

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
		// Set only by the prior-revision tier: the decision this reuse ultimately
		// descends from, already collapsed to one hop (see collapseOrigin).
		var originState *string
		var originRevID pgtype.UUID

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
					if r, err := queries.GetPriorRevisionPartByMPNAndManufacturer(ctx, dbgen.GetPriorRevisionPartByMPNAndManufacturerParams{
						ProjectID:         projectID,
						CurrentRevisionID: revID,
						Mpn:               item.Mpn,
						Manufacturer:      item.Manufacturer,
					}); err == nil {
						priorPartID = r.ResolvedPartID
						originState, originRevID = collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
					}
				}
				if !priorPartID.Valid {
					if r, err := queries.GetPriorRevisionPartByMPN(ctx, dbgen.GetPriorRevisionPartByMPNParams{
						ProjectID:         projectID,
						CurrentRevisionID: revID,
						Mpn:               item.Mpn,
					}); err == nil {
						priorPartID = r.ResolvedPartID
						originState, originRevID = collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
					}
				}
			}

			// 4b. SKU+distributor fallback: covers items with no MPN.
			if !priorPartID.Valid && item.Sku != nil && *item.Sku != "" && item.ResolvedDistributorID.Valid {
				if r, err := queries.GetPriorRevisionPartBySKU(ctx, dbgen.GetPriorRevisionPartBySKUParams{
					ProjectID:         projectID,
					CurrentRevisionID: revID,
					Sku:               item.Sku,
					DistributorID:     item.ResolvedDistributorID,
				}); err == nil {
					priorPartID = r.ResolvedPartID
					originState, originRevID = collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
				}
			}

			// 4c. Line-signature fallback: for a re-imported (identical) BOM, a line with
			// the same reference designators + raw value + footprint is the same line. This
			// carries forward manual resolutions of generic/parametric parts that have no
			// MPN or SKU to key on (the common case the MPN/SKU tiers above can't cover).
			if !priorPartID.Valid {
				if r, err := queries.GetPriorRevisionPartByLineSignature(ctx, dbgen.GetPriorRevisionPartByLineSignatureParams{
					ProjectID:            projectID,
					CurrentRevisionID:    revID,
					ReferenceDesignators: item.ReferenceDesignators,
					Value:                item.Value,
					Footprint:            item.Footprint,
				}); err == nil {
					priorPartID = r.ResolvedPartID
					originState, originRevID = collapseOrigin(r.BomRevisionID, r.ResolutionState, r.OriginResolutionState, r.OriginRevisionID)
				}
			}

			if priorPartID.Valid {
				resolvedPartID = priorPartID
				resolutionState = strPtr("auto_prior_revision")
			} else {
				// No tier hit — don't carry provenance from a lookup that missed.
				originState, originRevID = nil, pgtype.UUID{}
			}
		}

		// 5. Parametric (generic) match-only tier: for a row the parser read as a
		// generic spec, try to resolve to an existing catalog part by category +
		// canonical package + primary value (within epsilon). A unique match
		// auto-resolves; anything else falls through to remain unresolved. This tier
		// never creates a part — generic creation is an explicit user action.
		if resolutionState == nil {
			pid, n := tryParametricMatch(ctx, queries, teamID, item.ID)
			switch {
			case n == 1:
				resolvedPartID = pid
				resolutionState = strPtr("auto_generic")
			case n > 1:
				// Several catalog parts satisfy the spec — not safe to auto-resolve.
				// Surface for review; the candidate picker offers the same set.
				resolutionState = strPtr("ambiguous")
			}
		}

		// 5b. Fuzzy local MPN match: the exact tiers want an exact MPN, but the catalog
		// often holds a packaged variant of the BOM's base part (BOM "AZ431A" → catalog
		// "AZ431AN-ATRG1"). Before querying distributors, check the local library the
		// same way the review pane does (MPN/alias substring). A single hit auto-resolves;
		// multiple leave the line for review; none falls through to the distributor tier.
		if resolutionState == nil && item.Mpn != nil && *item.Mpn != "" {
			if fuzzy, err := queries.FindPartsByMPNFuzzy(ctx, dbgen.FindPartsByMPNFuzzyParams{
				Search: item.Mpn,
				TeamID: teamID,
			}); err == nil {
				switch {
				case len(fuzzy) == 1:
					// Resolve to the local part, but flag it: a substring match is less
					// certain than an exact one, so it surfaces in the review queue for a
					// one-click confirm (see auto_mpn_fuzzy handling in the web UI).
					resolvedPartID = fuzzy[0].ID
					resolutionState = strPtr("auto_mpn_fuzzy")
				case len(fuzzy) > 1:
					resolutionState = strPtr("ambiguous")
				}
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
				ResolvedPartID:        resolvedPartID,
				ResolutionState:       resolutionState,
				OriginResolutionState: originState,
				OriginRevisionID:      originRevID,
				ID:                    item.ID,
				BomRevisionID:         revID,
			}); err != nil {
				continue // non-fatal; skip progress update for this item
			}

			switch *resolutionState {
			case "auto_sku":
				resolvedSKU++
			case "auto_mpn", "auto_mpn_fuzzy":
				resolvedMPN++
			case "auto_prior_revision":
				resolvedPrior++
			case "auto_generic":
				resolvedGeneric++
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
		resolved := resolvedSKU + resolvedMPN + resolvedPrior + resolvedGeneric
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

// tryParametricMatch finds existing catalog parts that satisfy a BOM line's
// persisted parametric interpretation: category subtree + canonical package +
// (when the line carries one) primary value within an epsilon window. It returns
// the matched part id and the number of candidates; the id is meaningful only when
// the count is 1. It NEVER creates a part (generic creation is an explicit user
// action). The caller auto-resolves on a unique match and surfaces a non-unique
// result for review (the candidate set is the same one the review-gate picker shows
// — see internal/partmatch).
func tryParametricMatch(ctx context.Context, queries *dbgen.Queries, teamID, itemID pgtype.UUID) (pgtype.UUID, int) {
	var none pgtype.UUID
	raw, err := queries.GetBOMLineItemInterpretation(ctx, itemID)
	if err != nil || len(raw) == 0 {
		return none, 0
	}
	var interp genspec.Interpretation
	if err := json.Unmarshal(raw, &interp); err != nil {
		return none, 0
	}
	matches, err := partmatch.Find(ctx, queries, teamID, interp)
	if err != nil {
		return none, 0
	}
	if len(matches) == 1 {
		return matches[0].ID, 1
	}
	return none, len(matches)
}
