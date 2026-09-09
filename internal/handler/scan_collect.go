package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/scanner"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Scanner Collect mode: scan a pile of bags INTO a part collection ("list").
// Collections are user-private; the scan token resolves to the owning user, so
// the same owner filter the desktop handlers use applies verbatim here.
//
// Collect is resolve-only on the catalog side: it reuses the barcode→SKU→
// listing→part lookup chain but deliberately NOT writeLookupFound — collecting
// onto a list must never auto-create inventory entries. The only write is the
// list upsert.

// resolvedScanPart is the part identity produced by barcode resolution.
type resolvedScanPart struct {
	ID           pgtype.UUID
	Mpn          *string
	Manufacturer *string
	Description  *string
}

// resolveBarcodePart runs the lookup chain (distributor-scoped SKU → any-
// distributor SKU → MPN+manufacturer → MPN) without side effects. Returns nil
// when no catalog part matches.
func (h *Handler) resolveBarcodePart(ctx context.Context, teamID pgtype.UUID, parsed *scanner.ParsedBarcode) (*resolvedScanPart, error) {
	if parsed.SKU != "" {
		if parsed.Distributor != "" {
			row, err := h.Queries.GetPartBySKUAndDistributorWithInventory(ctx, dbgen.GetPartBySKUAndDistributorWithInventoryParams{
				Sku:             parsed.SKU,
				TeamID:          teamID,
				DistributorName: parsed.Distributor,
			})
			if err == nil {
				return &resolvedScanPart{ID: row.ID, Mpn: row.Mpn, Manufacturer: row.Manufacturer, Description: row.ShortDescription}, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
		row, err := h.Queries.GetPartBySKUWithInventory(ctx, dbgen.GetPartBySKUWithInventoryParams{
			Sku:    parsed.SKU,
			TeamID: teamID,
		})
		if err == nil {
			return &resolvedScanPart{ID: row.ID, Mpn: row.Mpn, Manufacturer: row.Manufacturer, Description: row.ShortDescription}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if parsed.MPN != "" {
		if parsed.Manufacturer != "" {
			mfrName := h.canonicalManufacturer(parsed.Manufacturer)
			row, err := h.Queries.GetPartByMPNAndManufacturerWithInventory(ctx, dbgen.GetPartByMPNAndManufacturerWithInventoryParams{
				Mpn:          &parsed.MPN,
				Manufacturer: &mfrName,
				TeamID:       teamID,
			})
			if err == nil {
				return &resolvedScanPart{ID: row.ID, Mpn: row.Mpn, Manufacturer: row.Manufacturer, Description: row.ShortDescription}, nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
		row, err := h.Queries.GetPartByMPNWithInventory(ctx, dbgen.GetPartByMPNWithInventoryParams{
			Mpn:    &parsed.MPN,
			TeamID: teamID,
		})
		if err == nil {
			return &resolvedScanPart{ID: row.ID, Mpn: row.Mpn, Manufacturer: row.Manufacturer, Description: row.ShortDescription}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	return nil, nil
}

// scanCollectionOwned loads a collection owned by the scan token's user within
// the scan session's team; writes 404 otherwise.
func (h *Handler) scanCollectionOwned(w http.ResponseWriter, r *http.Request, sess *dbgen.ScanSession, collectionIDStr string) (dbgen.PartCollection, bool) {
	var collectionID pgtype.UUID
	if err := collectionID.Scan(collectionIDStr); err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id", "BAD_REQUEST")
		return dbgen.PartCollection{}, false
	}
	var userID pgtype.UUID
	if err := userID.Scan(userIDFromContext(r)); err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return dbgen.PartCollection{}, false
	}
	c, err := h.Queries.GetPartCollection(r.Context(), dbgen.GetPartCollectionParams{
		ID: collectionID, UserID: userID, TeamID: sess.TeamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "collection not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		}
		return dbgen.PartCollection{}, false
	}
	return c, true
}

// publishCollectActivity emits the standardized scanner_activity event for the
// future live-activity island. Collect targets a private list, so the event is
// owner-targeted like collection_changed.
func (h *Handler) publishCollectActivity(r *http.Request, sess *dbgen.ScanSession, listName, state, lastLabel string) {
	var last any
	if lastLabel != "" {
		last = map[string]string{"label": lastLabel, "at": time.Now().UTC().Format(time.RFC3339)}
	}
	d, _ := json.Marshal(map[string]any{
		"type":     "collect",
		"ref":      listName,
		"state":    state,
		"progress": nil, // no target set — collect is indeterminate
		"last":     last,
	})
	h.Worker.Publish(worker.Event{
		Type:         "scanner_activity",
		TeamID:       sess.TeamID.String(),
		TargetUserID: userIDFromContext(r),
		Data:         d,
	})
}

// ListScanCollections handles GET /api/v1/scan/collections — the phone's list
// picker: the token user's active lists in the session's team.
func (h *Handler) ListScanCollections(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if !h.requireScanMember(w, r, sess) {
		return
	}
	var userID pgtype.UUID
	if err := userID.Scan(userIDFromContext(r)); err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	rows, err := h.Queries.ListPartCollections(r.Context(), dbgen.ListPartCollectionsParams{
		UserID: userID, TeamID: sess.TeamID, IncludeArchived: false,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	type item struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		CollectCaptureQty *bool  `json:"collect_capture_qty"`
		ItemCount         int32  `json:"item_count"`
		TotalQuantity     int64  `json:"total_quantity"`
		CheckedCount      int32  `json:"checked_count"`
		UpdatedAt         string `json:"updated_at"`
	}
	resp := make([]item, 0, len(rows))
	for _, c := range rows {
		resp = append(resp, item{
			ID:                c.ID.String(),
			Name:              c.Name,
			CollectCaptureQty: c.CollectCaptureQty,
			ItemCount:         c.ItemCount,
			TotalQuantity:     c.TotalQuantity,
			CheckedCount:      c.CheckedCount,
			UpdatedAt:         c.UpdatedAt.Time.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": resp})
}

// CreateScanCollection handles POST /api/v1/scan/collections — "new list" on
// the phone. Body: {name?} (blank auto-names, same as desktop).
func (h *Handler) CreateScanCollection(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if !h.requireScanMember(w, r, sess) {
		return
	}
	var userID pgtype.UUID
	if err := userID.Scan(userIDFromContext(r)); err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.Name == "" {
		body.Name = "Untitled list — " + time.Now().Format("Jan 2")
	}
	c, err := h.Queries.CreatePartCollection(r.Context(), dbgen.CreatePartCollectionParams{
		UserID: userID, TeamID: sess.TeamID, Name: body.Name,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, sess.TeamID, c.ID.String())
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": c.ID.String(), "name": c.Name, "collect_capture_qty": c.CollectCaptureQty,
	})
}

// UpdateScanCollection handles PATCH /api/v1/scan/collections/{collectionId} —
// the phone persists the first-scan quantity-capture decision.
// Body: {collect_capture_qty}.
func (h *Handler) UpdateScanCollection(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if !h.requireScanMember(w, r, sess) {
		return
	}
	c, ok := h.scanCollectionOwned(w, r, sess, chi.URLParam(r, "collectionId"))
	if !ok {
		return
	}
	var body struct {
		CollectCaptureQty *bool `json:"collect_capture_qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CollectCaptureQty == nil {
		writeError(w, http.StatusBadRequest, "collect_capture_qty is required", "BAD_REQUEST")
		return
	}
	var userID pgtype.UUID
	_ = userID.Scan(userIDFromContext(r)) //nolint:errcheck // validated by scanCollectionOwned
	updated, err := h.Queries.UpdatePartCollection(r.Context(), dbgen.UpdatePartCollectionParams{
		ID: c.ID, UserID: userID, TeamID: sess.TeamID,
		CollectCaptureQty: body.CollectCaptureQty,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, sess.TeamID, updated.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{
		"id": updated.ID.String(), "collect_capture_qty": updated.CollectCaptureQty,
	})
}

// ListScanCollectionItems handles GET /api/v1/scan/collections/{collectionId}/items —
// the phone's read-only list view: rows with on-hand stock and storage
// location (the "where do these parts live?" lens).
func (h *Handler) ListScanCollectionItems(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	c, ok := h.scanCollectionOwned(w, r, sess, chi.URLParam(r, "collectionId"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListPartCollectionItemsWithStock(r.Context(), dbgen.ListPartCollectionItemsWithStockParams{
		TeamID:       sess.TeamID,
		CollectionID: c.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	type location struct {
		ID         string  `json:"id"`
		Coordinate []int32 `json:"coordinate"`
	}
	type item struct {
		ID              string    `json:"id"`
		PartID          string    `json:"part_id"`
		Quantity        *int32    `json:"quantity"`
		Note            *string   `json:"note"`
		CheckedAt       *string   `json:"checked_at"`
		Mpn             *string   `json:"mpn"`
		Description     string    `json:"description"`
		QuantityOnHand  *int32    `json:"quantity_on_hand"`
		StorageLocation *location `json:"storage_location"`
	}
	items := make([]item, 0, len(rows))
	for _, row := range rows {
		it := item{
			ID:             row.ID.String(),
			PartID:         row.PartID.String(),
			Quantity:       row.Quantity,
			Note:           row.Note,
			CheckedAt:      timestamptzString(row.CheckedAt),
			Mpn:            row.Mpn,
			Description:    row.PartDescription,
			QuantityOnHand: row.QuantityOnHand,
		}
		if row.StorageLocationID.Valid {
			it.StorageLocation = &location{ID: row.StorageLocationID.String(), Coordinate: row.StorageCoordinate}
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": c.Name, "items": items})
}

// CollectScan handles POST /api/v1/scan/collect — one scan of the Collect loop.
//
// Body: {collection_id, barcode?, part_id?, quantity?}
//   - barcode: first phase. Resolves to a catalog part; per the list's
//     collect_capture_qty toggle either upserts immediately (OFF → bare
//     membership; ON + barcode carries a piece qty → that qty) or answers
//     {status:"quantity_required"} without writing (ON + no qty) so the phone
//     can prompt.
//   - part_id (+quantity?): second phase after the prompt. quantity absent =
//     Skip = bare membership (never a silent +1).
//
// Responses: added | quantity_required | unknown (with parsed fields).
func (h *Handler) CollectScan(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	if !h.requireScanMember(w, r, sess) {
		return
	}

	var body struct {
		CollectionID string `json:"collection_id"`
		Barcode      string `json:"barcode"`
		PartID       string `json:"part_id"`
		Quantity     *int32 `json:"quantity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CollectionID == "" {
		writeError(w, http.StatusBadRequest, "collection_id is required", "BAD_REQUEST")
		return
	}
	if body.Quantity != nil && *body.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "quantity must be positive", "BAD_REQUEST")
		return
	}
	c, ok := h.scanCollectionOwned(w, r, sess, body.CollectionID)
	if !ok {
		return
	}

	var part *resolvedScanPart
	quantity := body.Quantity

	switch {
	case body.PartID != "":
		// Second phase: the phone resolved already and prompted for a count.
		var partID pgtype.UUID
		if err := partID.Scan(body.PartID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
			return
		}
		p, ok := h.requirePartInTeamOrGlobal(w, r, partID, sess.TeamID)
		if !ok {
			return
		}
		part = &resolvedScanPart{ID: p.ID, Mpn: p.Mpn, Manufacturer: p.Manufacturer, Description: p.ShortDescription}

	case strings.TrimSpace(body.Barcode) != "":
		parsed, parseErr := scanner.ParseAny(body.Barcode)
		if parseErr != nil || (parsed.SKU == "" && parsed.MPN == "") {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "unknown", "reason": "unparsed", "raw": body.Barcode,
			})
			return
		}
		part, err = h.resolveBarcodePart(r.Context(), sess.TeamID, parsed)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		if part == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":              "unknown",
				"reason":              "not_in_catalog",
				"parsed_mpn":          parsed.MPN,
				"parsed_sku":          parsed.SKU,
				"parsed_manufacturer": parsed.Manufacturer,
				"parsed_distributor":  parsed.Distributor,
				"parsed_qty":          parsed.Quantity,
			})
			return
		}
		// Quantity per the per-list toggle (NULL toggle = capture off).
		captureOn := c.CollectCaptureQty != nil && *c.CollectCaptureQty
		if captureOn && quantity == nil {
			if parsed.Quantity > 0 {
				q := int32(parsed.Quantity) //nolint:gosec // barcode pack quantities are small
				quantity = &q
			} else {
				// Never invent a quantity: the phone prompts (skippable numpad).
				writeJSON(w, http.StatusOK, map[string]any{
					"status": "quantity_required",
					"part": map[string]any{
						"id": part.ID.String(), "mpn": part.Mpn,
						"manufacturer": part.Manufacturer, "description": part.Description,
					},
				})
				return
			}
		}
		if !captureOn {
			quantity = nil // bare membership regardless of what the barcode carried
		}

	default:
		writeError(w, http.StatusBadRequest, "barcode or part_id is required", "BAD_REQUEST")
		return
	}

	item, err := h.Queries.UpsertPartCollectionItem(r.Context(), dbgen.UpsertPartCollectionItemParams{
		CollectionID: c.ID,
		PartID:       part.ID,
		Quantity:     quantity,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := h.Queries.TouchPartCollection(r.Context(), c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	tally, err := h.Queries.GetPartCollectionTally(r.Context(), c.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	h.publishCollectionChanged(r, sess.TeamID, c.ID.String())
	label := ""
	if part.Mpn != nil {
		label = *part.Mpn
	}
	h.publishCollectActivity(r, sess, c.Name, "in_progress", label)

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "added",
		"part": map[string]any{
			"id": part.ID.String(), "mpn": part.Mpn,
			"manufacturer": part.Manufacturer, "description": part.Description,
		},
		"quantity_added": quantity,
		"item_quantity":  item.Quantity,
		"tally": map[string]any{
			"item_count":         tally.ItemCount,
			"total_quantity":     tally.TotalQuantity,
			"unquantified_count": tally.UnquantifiedCount,
		},
	})
}
