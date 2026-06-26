package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/genspec"
	"github.com/Yamakaja/partrack/internal/mfrmatch"
	"github.com/Yamakaja/partrack/internal/paramparse"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnrichTarget identifies one part to enrich from a specific distributor SKU.
type EnrichTarget struct {
	PartID        string
	Sku           string
	DistributorID string
}

// PartEnrichBatchPayload is the job payload for JobPartEnrichBatch — a bounded
// bulk re-enrichment of the parts matching a search (see RefreshSearchParameters).
type PartEnrichBatchPayload struct {
	Targets []EnrichTarget
	UserID  string
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
	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil {
		q.publishError(job, "failed to list distributor configs: "+err.Error())
		return
	}

	result, err := q.enrichOnePart(ctx, queries, teamID, partID, distributorID, sku, configs)
	if err != nil {
		q.publishError(job, err.Error())
		return
	}
	if result == nil {
		q.publishProgress(job, "part_enrichment_progress", map[string]any{
			"status":         "not_found",
			"distributor_id": distIDStr,
		})
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

// handlePartEnrichBatch enriches every target in the payload, one live API call
// per part, emitting a per-part `part_enrich_done` progress event and a terminal
// `part_enrich_complete` {total, done, not_found, failed}. Subscribers rely on the
// terminal event rather than counting per-part events.
func (q *Queue) handlePartEnrichBatch(job Job) {
	ctx := context.Background()

	payload, ok := job.Payload.(PartEnrichBatchPayload)
	if !ok {
		q.publishError(job, "invalid payload type")
		return
	}
	var teamID pgtype.UUID
	if err := teamID.Scan(job.TeamID); err != nil {
		q.publishError(job, "invalid team_id: "+job.TeamID)
		return
	}

	queries := dbgen.New(q.db)
	configs, err := queries.ListEnabledDistributorConfigsForTeam(ctx, teamID)
	if err != nil {
		q.publishError(job, "failed to list distributor configs: "+err.Error())
		return
	}

	var done, notFound, failed int
	changedParts := make([]string, 0, len(payload.Targets))
	publishComplete := func() {
		q.publishProgress(job, "part_enrich_complete", map[string]any{
			"status":    "complete",
			"total":     len(payload.Targets),
			"done":      done,
			"not_found": notFound,
			"failed":    failed,
		})
	}

	for _, t := range payload.Targets {
		var partID, distID pgtype.UUID
		if err := partID.Scan(t.PartID); err != nil {
			failed++
			q.publishProgress(job, "part_enrich_done", map[string]any{"status": "error", "part_id": t.PartID, "error": "invalid part_id"})
			continue
		}
		if err := distID.Scan(t.DistributorID); err != nil {
			failed++
			q.publishProgress(job, "part_enrich_done", map[string]any{"status": "error", "part_id": t.PartID, "error": "invalid distributor_id"})
			continue
		}
		result, err := q.enrichOnePart(ctx, queries, teamID, partID, distID, t.Sku, configs)
		if err != nil {
			failed++
			q.publishProgress(job, "part_enrich_done", map[string]any{"status": "error", "part_id": t.PartID, "error": err.Error()})
			continue
		}
		if result == nil {
			notFound++
			q.publishProgress(job, "part_enrich_done", map[string]any{"status": "not_found", "part_id": t.PartID})
			continue
		}
		done++
		changedParts = append(changedParts, t.PartID)
		q.publishProgress(job, "part_enrich_done", map[string]any{"status": "done", "part_id": t.PartID})
	}

	// Notify other users' views of the changed parts; the acting user is skipped
	// (UserID) and refreshes its own views on completion.
	for _, partID := range changedParts {
		data, _ := json.Marshal(map[string]string{"part_id": partID})
		q.Publish(Event{Type: "part_changed", TeamID: job.TeamID, UserID: payload.UserID, Data: data})
	}
	cartData, _ := json.Marshal(map[string]string{})
	q.Publish(Event{Type: "cart_changed", TeamID: job.TeamID, Data: cartData})
	q.invalidateCart(job.TeamID)
	publishComplete()
}

// enrichOnePart runs enrichment for a single (part, sku, distributor) and persists
// the result within its own transaction. It returns the EnrichmentResult on success,
// nil if the distributor returned no match (not found), or an error. It publishes
// no events — callers decide how to report progress.
func (q *Queue) enrichOnePart(
	ctx context.Context,
	queries *dbgen.Queries,
	teamID, partID, distributorID pgtype.UUID,
	sku string,
	configs []dbgen.ListEnabledDistributorConfigsForTeamRow,
) (*distributor.EnrichmentResult, error) {
	part, err := queries.GetPart(ctx, partID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("part %s not found", partID.String())
		}
		return nil, fmt.Errorf("failed to get part: %w", err)
	}

	// Find the specific distributor config for this team + distributor.
	var cfg *dbgen.ListEnabledDistributorConfigsForTeamRow
	for i := range configs {
		if configs[i].DistributorID == distributorID {
			cfg = &configs[i]
			break
		}
	}
	if cfg == nil {
		return nil, fmt.Errorf("distributor %s not configured or not enabled for this team", distributorID.String())
	}
	if cfg.ApiType == nil {
		return nil, fmt.Errorf("distributor has no API integration")
	}
	client := q.distributors.Lookup(*cfg.ApiType)
	if client == nil {
		return nil, fmt.Errorf("no client registered for api_type %s", *cfg.ApiType)
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
		return nil, fmt.Errorf("enrichment failed: %w", err)
	}
	if result == nil {
		return nil, nil
	}

	tx, err := q.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	qtx := queries.WithTx(tx)

	if err := persistEnrichmentResult(ctx, qtx, partID, cfg.DistributorID, result, isAuthoritative, isAuthoritative); err != nil {
		tx.Rollback(ctx) //nolint:errcheck
		return nil, fmt.Errorf("failed to persist enrichment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	return result, nil
}

// ApplyEnrichmentResult finds or creates a part matching result, applies parameters
// (for newly created parts only, to preserve curated data on existing parts), upserts all
// distributor listings, and marks the part enriched. Manages its own database transaction.
// Returns the resolved part ID.
//
// fallbackMPN and fallbackManufacturer are used when the enrichment result itself
// omits those fields. Returns an invalid pgtype.UUID and a non-nil error on failure.
//
// mfr canonicalizes the manufacturer name before matching and creation, so a part
// listed under a distributor's spelling variant ("TI") resolves to — and is stored
// under — the canonical manufacturer ("Texas Instruments"), keeping one part per
// manufacturer+MPN. nil disables canonicalization (raw spelling is used as-is).
func ApplyEnrichmentResult(
	ctx context.Context,
	db *pgxpool.Pool,
	queries *dbgen.Queries,
	teamID pgtype.UUID,
	distributorID pgtype.UUID,
	result *distributor.EnrichmentResult,
	fallbackMPN, fallbackManufacturer *string,
	mfr *mfrmatch.Resolver,
) (pgtype.UUID, error) {
	mpn := strOrNil(result.MPN)
	if mpn == nil {
		mpn = fallbackMPN
	}
	manufacturer := strOrNil(result.Manufacturer)
	if manufacturer == nil {
		manufacturer = fallbackManufacturer
	}
	// Canonicalize the manufacturer spelling so matching and creation both key on the
	// canonical identity rather than the distributor's variant spelling.
	if mfr != nil && manufacturer != nil {
		if canon, ok := mfr.CanonicalName(*manufacturer); ok {
			manufacturer = &canon
		}
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
			Package:          strOrNil(result.Package),
			CanonicalPackage: strOrNil(genspec.NormalizePackage(result.Package)),
			DatasheetUrl:     strOrNil(result.DatasheetURL),
			LifecycleStatus:  lifecycleStatus,
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
			var enumValues []string

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
					// Parse using the parameter's value_type: enum_set yields a
					// value list, everything else a numeric (or nothing).
					valueType := paramDefValueType[id]
					if valueType == "enum_set" {
						enumValues = paramparse.ParseEnumSet(p.Value)
					} else {
						pr := paramparse.Parse(p.Value, valueType)
						numericValue = pr.Value
						numericValueMax = pr.ValueMax
					}
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
				EnumValues:          enumValues,
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
			Package:            strOrNil(result.Package),
			CanonicalPackage:   strOrNil(genspec.NormalizePackage(result.Package)),
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
