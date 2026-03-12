package worker

import (
	"context"
	"encoding/json"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

type buildSubstituteSelectionsPayload struct {
	BuildID string `json:"build_id"`
	TeamID  string `json:"team_id"`
}

func (q *Queue) handleBuildSubstituteSelections(job Job) {
	ctx := context.Background()

	raw, _ := json.Marshal(job.Payload)
	var p buildSubstituteSelectionsPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		q.publishError(job, "invalid payload: "+err.Error())
		return
	}

	var buildID pgtype.UUID
	if err := buildID.Scan(p.BuildID); err != nil {
		q.publishError(job, "invalid build_id: "+p.BuildID)
		return
	}
	var teamID pgtype.UUID
	if err := teamID.Scan(p.TeamID); err != nil {
		q.publishError(job, "invalid team_id: "+p.TeamID)
		return
	}

	queries := dbgen.New(q.db)

	build, err := queries.GetBuild(ctx, dbgen.GetBuildParams{
		ID:     buildID,
		TeamID: teamID,
	})
	if err != nil {
		q.publishError(job, "build not found: "+err.Error())
		return
	}

	revID := build.BomRevisionID
	buildQty := build.Quantity

	// Fetch line items, inventory, and substitutes concurrently.
	type liResult struct {
		items []dbgen.ListBOMLineItemsRow
		err   error
	}
	type invResult struct {
		rows []dbgen.GetInventoryQuantitiesForRevisionRow
		err  error
	}
	type subsResult struct {
		rows []dbgen.ListAllSubstitutesForRevisionRow
		err  error
	}

	liCh := make(chan liResult, 1)
	invCh := make(chan invResult, 1)
	subsCh := make(chan subsResult, 1)

	go func() {
		items, err := queries.ListBOMLineItems(ctx, revID)
		liCh <- liResult{items, err}
	}()
	go func() {
		rows, err := queries.GetInventoryQuantitiesForRevision(ctx,
			dbgen.GetInventoryQuantitiesForRevisionParams{
				BomRevisionID: revID,
				TeamID:        teamID,
			})
		invCh <- invResult{rows, err}
	}()
	go func() {
		rows, err := queries.ListAllSubstitutesForRevision(ctx, revID)
		subsCh <- subsResult{rows, err}
	}()

	liRes := <-liCh
	invRes := <-invCh
	subsRes := <-subsCh

	if liRes.err != nil || invRes.err != nil || subsRes.err != nil {
		q.publishError(job, "query error fetching build data")
		return
	}

	// Collect all part IDs (primary + substitute) for reserved quantities query.
	partIDSet := make(map[string]pgtype.UUID)
	for _, li := range liRes.items {
		if li.ResolvedPartID.Valid {
			partIDSet[li.ResolvedPartID.String()] = li.ResolvedPartID
		}
	}
	for _, s := range subsRes.rows {
		partIDSet[s.PartID.String()] = s.PartID
	}
	partIDs := make([]pgtype.UUID, 0, len(partIDSet))
	for _, pid := range partIDSet {
		partIDs = append(partIDs, pid)
	}

	var reservedByPart map[string]int32
	if len(partIDs) > 0 {
		reservedRows, err := queries.GetReservedQtysForParts(ctx, dbgen.GetReservedQtysForPartsParams{
			PartIds: partIDs,
			TeamID:  teamID,
		})
		if err != nil {
			q.publishError(job, "query error fetching reserved quantities")
			return
		}
		reservedByPart = make(map[string]int32, len(reservedRows))
		for _, row := range reservedRows {
			reservedByPart[row.PartID.String()] = row.TotalReserved
		}
	} else {
		reservedByPart = make(map[string]int32)
	}

	invByPart := make(map[string]dbgen.GetInventoryQuantitiesForRevisionRow)
	for _, row := range invRes.rows {
		invByPart[row.PartID.String()] = row
	}

	// Group substitutes by line item (already ordered by priority ASC).
	type subEntry struct {
		PartID string
	}
	subsByItem := make(map[string][]subEntry)
	for _, s := range subsRes.rows {
		key := s.BomLineItemID.String()
		subsByItem[key] = append(subsByItem[key], subEntry{PartID: s.PartID.String()})
	}

	// For each line item, determine the selected substitute (if any).
	for _, li := range liRes.items {
		if !li.ResolvedPartID.Valid {
			continue
		}
		itemID := li.ID.String()
		required := li.Quantity * buildQty

		pid := li.ResolvedPartID.String()
		inv := invByPart[pid]
		primaryFree := inv.QuantityOnHand - reservedByPart[pid]
		if primaryFree < 0 {
			primaryFree = 0
		}
		if primaryFree >= required {
			continue // primary is sufficient; no substitute needed
		}

		for _, sub := range subsByItem[itemID] {
			subInv := invByPart[sub.PartID]
			subFree := subInv.QuantityOnHand - reservedByPart[sub.PartID]
			if subFree < 0 {
				subFree = 0
			}
			if subFree >= required {
				var subPartID pgtype.UUID
				if err := subPartID.Scan(sub.PartID); err != nil {
					continue
				}
				if err := queries.SetBuildSubstituteSelection(ctx, dbgen.SetBuildSubstituteSelectionParams{
					BuildID:        buildID,
					BomLineItemID:  li.ID,
					SelectedPartID: subPartID,
				}); err != nil {
					q.publishError(job, "error saving substitute selection: "+err.Error())
					return
				}
				break
			}
		}
	}

	// Mark selections as checked.
	now := pgtype.Timestamptz{}
	_ = now.Scan(time.Now())
	if err := queries.SetBuildSelectionsCheckedAt(ctx, dbgen.SetBuildSelectionsCheckedAtParams{
		ID:                  buildID,
		SelectionsCheckedAt: now,
	}); err != nil {
		q.publishError(job, "error setting selections_checked_at: "+err.Error())
		return
	}

	// Notify frontend.
	data, _ := json.Marshal(map[string]string{"build_id": p.BuildID})
	q.Publish(Event{
		Type:   "build_changed",
		Data:   data,
		TeamID: job.TeamID,
	})
}
