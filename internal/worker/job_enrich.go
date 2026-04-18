package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

	part, err := queries.GetPart(ctx, partID)
	if err != nil {
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

	// Determine whether this distributor is the authoritative source for part
	// metadata and parameters. Priority order:
	//   1. Part's explicit preferred_parameter_source_id (user-set override).
	//   2. Team's preferred distributor config flag.
	//   3. Team's preferred listing distributor for this part.
	// If none of the above designate a different distributor, treat this one as
	// authoritative (no competing preference to protect).
	isAuthoritative := isEnrichmentAuthoritative(ctx, queries, part, distributorID, teamID, cfg.Preferred, configs)

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

	if err := persistEnrichmentResult(ctx, qtx, partID, cfg.DistributorID, result, isAuthoritative, isAuthoritative); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		q.publishError(job, "failed to persist enrichment: "+err.Error())
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
	cartData, _ := json.Marshal(map[string]string{})
	q.Publish(Event{Type: "cart_changed", TeamID: job.TeamID, Data: cartData})
	q.invalidateCart(job.TeamID)
}

// ApplyEnrichmentResult finds or creates a part matching result, applies parameters
// (for newly created parts only, to preserve curated data on existing parts), upserts all
// distributor listings, and marks the part enriched. Manages its own database transaction.
// Returns the resolved part ID.
//
// fallbackMPN and fallbackManufacturer are used when the enrichment result itself
// omits those fields. Returns an invalid pgtype.UUID and a non-nil error on failure.
func ApplyEnrichmentResult(
	ctx context.Context,
	db *pgxpool.Pool,
	queries *dbgen.Queries,
	teamID pgtype.UUID,
	distributorID pgtype.UUID,
	result *distributor.EnrichmentResult,
	fallbackMPN, fallbackManufacturer *string,
) (pgtype.UUID, error) {
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
			Scope:        dbgen.PartScopeGlobal,
			TeamID:       pgtype.UUID{}, // NULL for global parts
			Mpn:          mpn,
			Manufacturer: manufacturer,
			Description:  strOrNil(result.Description),
			// category_id resolved during enrichment; not set at part creation time here
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

	// 3. Determine whether this distributor is authoritative for the part's metadata
	// and parameters. New parts get full enrichment. For existing parts, respect the
	// part's preferred_parameter_source_id if set; if not set, allow updates.
	updateMetadata := partIsNew
	if !partIsNew {
		existingPart, err := queries.GetPart(ctx, partID)
		if err == nil {
			updateMetadata = !existingPart.PreferredParameterSourceID.Valid ||
				existingPart.PreferredParameterSourceID == distributorID
		}
	}

	// 4. Apply enrichment data in a transaction.
	tx, err := db.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin tx: %w", err)
	}
	qtx := queries.WithTx(tx)

	if err := persistEnrichmentResult(ctx, qtx, partID, distributorID, result, partIsNew, updateMetadata); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		return pgtype.UUID{}, fmt.Errorf("persist enrichment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit: %w", err)
	}

	return partID, nil
}

// isEnrichmentAuthoritative returns true when distributorID should be treated as the
// authoritative source for a part's metadata and parameters. The decision follows this
// priority order:
//  1. Part's explicit preferred_parameter_source_id — if set, only that distributor wins.
//  2. Team's preferred listing distributor for this part — if set, that distributor wins.
//  3. Team's distributor config preferred flag — if any config is marked preferred, only
//     the preferred one wins.
//  4. No preference configured anywhere — treat all distributors as authoritative.
func isEnrichmentAuthoritative(
	ctx context.Context,
	queries *dbgen.Queries,
	part dbgen.Part,
	distributorID pgtype.UUID,
	teamID pgtype.UUID,
	cfgPreferred bool,
	allConfigs []dbgen.ListEnabledDistributorConfigsForTeamRow,
) bool {
	// 1. Part-level explicit preference overrides everything.
	if part.PreferredParameterSourceID.Valid {
		return part.PreferredParameterSourceID == distributorID
	}

	// 2. Team's preferred listing for this part.
	prefListingDist, err := queries.GetPreferredListingDistributorByPartID(ctx, dbgen.GetPreferredListingDistributorByPartIDParams{
		TeamID: teamID,
		PartID: part.ID,
	})
	if err == nil {
		return prefListingDist == distributorID
	}

	// 3. Team distributor config preferred flag.
	for _, c := range allConfigs {
		if c.Preferred {
			// At least one distributor is marked preferred; this one must be it.
			return cfgPreferred
		}
	}

	// 4. No preference set — any distributor is authoritative.
	return true
}

// persistEnrichmentResult writes the distributor-sourced data from result into the
// database within the provided transaction. It always upserts listings. When
// refreshParameters is true it replaces all distributor-sourced parameters; when false
// existing parameters are left untouched (preserves curated data on known parts).
// When updateMetadata is true it also updates part metadata (description, category,
// URLs, lifecycle) and refreshes aliases; when false those fields are left unchanged.
// Both are false when a non-authoritative distributor is enriching a part that already
// has a preferred metadata source. The caller is responsible for rolling back on error.
func persistEnrichmentResult(
	ctx context.Context,
	qtx *dbgen.Queries,
	partID pgtype.UUID,
	distributorID pgtype.UUID,
	result *distributor.EnrichmentResult,
	refreshParameters bool,
	updateMetadata bool,
) error {
	if refreshParameters || true {
		if err := qtx.DeleteDistributorParameters(ctx, dbgen.DeleteDistributorParametersParams{
			PartID:              partID,
			SourceDistributorID: distributorID,
		}); err != nil {
			return fmt.Errorf("delete distributor parameters: %w", err)
		}

		// Load all parameter definitions once to use as a cache for value_type lookups.
		paramDefs, err := qtx.ListParameterDefinitions(ctx)
		if err != nil {
			return fmt.Errorf("load parameter definitions: %w", err)
		}

		paramDefValueType := make(map[int32]string, len(paramDefs))
		for _, d := range paramDefs {
			paramDefValueType[int32(d.ID)] = d.ValueType
		}

		for _, p := range result.Parameters {
			var paramID *int32
			var numericValue, numericValueMax *float64

			if p.DistributorParamID != "" {
				// Upsert the mapping row so newly-seen params appear in the admin UI
				// as unmapped. If a mapping already exists with a parameter_id, use it.
				if _, err := qtx.UpsertDistributorParamMapping(ctx, dbgen.UpsertDistributorParamMappingParams{
					DistributorID:        distributorID,
					DistributorParamID:   p.DistributorParamID,
					DistributorParamName: p.Name,
				}); err != nil {
					return fmt.Errorf("upsert param mapping %q: %w", p.Name, err)
				}
				// Look up the resolved partrack parameter (may return no rows if unmapped).
				if mapped, err := qtx.GetMappedParameter(ctx, dbgen.GetMappedParameterParams{
					DistributorID:      distributorID,
					DistributorParamID: p.DistributorParamID,
				}); err == nil {
					id := int32(mapped.ID)
					paramID = &id
					// Parse numeric value using the parameter's value_type.
					valueType := paramDefValueType[id]
					pr := paramparse.Parse(p.Value, valueType)
					numericValue = pr.Value
					numericValueMax = pr.ValueMax
				}
			}

			if _, err := qtx.InsertDistributorParameter(ctx, dbgen.InsertDistributorParameterParams{
				PartID:              partID,
				Name:                p.Name,
				Value:               p.Value,
				SourceDistributorID: distributorID,
				ParameterID:         paramID,
				NumericValue:        numericValue,
				NumericValueMax:     numericValueMax,
			}); err != nil {
				return fmt.Errorf("insert parameter %q: %w", p.Name, err)
			}
		}
	}

	// Resolve distributor category → partrack category mapping.
	// Walk the chain from most-specific (leaf) to least-specific (root), stopping at
	// the first level that has an existing mapping. This lets a leaf without a direct
	// mapping fall back to its parent category.
	var resolvedCategoryID *int32
	for _, cat := range result.DistributorCategoryChain {
		if cat.Key == "" || cat.Key == "0" {
			continue
		}
		mapping, err := qtx.UpsertDistributorCategoryMapping(ctx, dbgen.UpsertDistributorCategoryMappingParams{
			DistributorID:           distributorID,
			DistributorCategoryID:   cat.Key,
			DistributorCategoryName: cat.Name,
			// Pass NULL so COALESCE preserves any admin-set mapping.
			CategoryID: nil,
		})
		if err == nil && mapping.CategoryID != nil {
			resolvedCategoryID = mapping.CategoryID
			break
		}
	}

	var distCategoryID, distCategoryName *string
	if len(result.DistributorCategoryChain) > 0 {
		leaf := result.DistributorCategoryChain[0]
		distCategoryID = &leaf.Key
		distCategoryName = &leaf.Name
	}

	for _, l := range result.Listings {
		unitPrice, err := parseNumeric(l.UnitPrice)
		if err != nil {
			return fmt.Errorf("invalid unit_price %q: %w", l.UnitPrice, err)
		}
		fixedCost, err := parseNumeric(l.FixedOrderCost)
		if err != nil {
			return fmt.Errorf("invalid fixed_order_cost %q: %w", l.FixedOrderCost, err)
		}
		priceBreaksJSON, _ := json.Marshal(l.PriceBreaks)
		if _, err := qtx.UpsertListing(ctx, dbgen.UpsertListingParams{
			PartID:                  partID,
			DistributorID:           distributorID,
			Sku:                     l.SKU,
			PackagingType:           l.PackagingType,
			UnitPrice:               unitPrice,
			StockAvailable:          l.StockAvailable,
			Moq:                     l.Moq,
			FixedOrderCost:          fixedCost,
			PriceBreaks:             priceBreaksJSON,
			DistributorCategoryID:   distCategoryID,
			DistributorCategoryName: distCategoryName,
		}); err != nil {
			return fmt.Errorf("upsert listing %q: %w", l.SKU, err)
		}
	}

	if updateMetadata {
		var lifecycleStatus dbgen.NullPartLifecycleStatus
		if result.LifecycleStatus != "" {
			lifecycleStatus = dbgen.NullPartLifecycleStatus{
				PartLifecycleStatus: dbgen.PartLifecycleStatus(result.LifecycleStatus),
				Valid:               true,
			}
		}
		if err := qtx.SetPartEnriched(ctx, dbgen.SetPartEnrichedParams{
			ID:                 partID,
			EnrichmentSourceID: distributorID,
			LifecycleStatus:    lifecycleStatus,
			Mpn:                strOrNil(result.MPN),
			Description:        strOrNil(result.Description),
			DatasheetUrl:       strOrNil(result.DatasheetURL),
			ShortDescription:   strOrNil(result.ShortDescription),
			ProductUrl:         strOrNil(result.ProductURL),
			ImageUrl:           strOrNil(result.ImageURL),
			NewCategoryID:      resolvedCategoryID,
		}); err != nil {
			return fmt.Errorf("set enriched: %w", err)
		}

		if err := qtx.DeletePartAliases(ctx, partID); err != nil {
			return fmt.Errorf("delete aliases: %w", err)
		}
		for _, alias := range result.Aliases {
			if err := qtx.InsertPartAlias(ctx, dbgen.InsertPartAliasParams{
				PartID: partID,
				Alias:  alias,
			}); err != nil {
				return fmt.Errorf("insert alias %q: %w", alias, err)
			}
		}
	}

	return nil
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

// strOrNil returns a pointer to s, or nil if s is empty.
func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
