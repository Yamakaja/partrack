package handler

import (
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

// Scanner Pick mode: sort an unsorted pile ON a list. Each bag is scanned, and
// the list row it matches gets checked — the persisted selection the desktop
// panel renders as checkboxes, so the pile you sorted on the phone is the
// selection waiting for you at the PC.
//
// Where Filter answers "does this bag belong?" with an ephemeral client-side
// tally, Pick writes the answer down. That is the whole difference, and it buys
// the one verdict Filter cannot express: already_checked — you have pulled one
// of these out already, so this bag is a duplicate.
//
// Like Collect, Pick is resolve-only: it reuses the barcode→part lookup chain
// but never creates inventory entries. The only write is the list row's
// checked_at.

// publishPickActivity emits scanner_activity for the pick loop. Unlike Collect
// this run has a known total, so progress is determinate.
func (h *Handler) publishPickActivity(r *http.Request, sess *dbgen.ScanSession, listName, lastLabel string, done, total int32) {
	var last any
	if lastLabel != "" {
		last = map[string]string{"label": lastLabel, "at": time.Now().UTC().Format(time.RFC3339)}
	}
	d, _ := json.Marshal(map[string]any{
		"type":     "pick",
		"ref":      listName,
		"state":    "in_progress",
		"progress": map[string]any{"done": done, "total": total, "unit": "parts"},
		"last":     last,
	})
	h.Worker.Publish(worker.Event{
		Type:         "scanner_activity",
		TeamID:       sess.TeamID.String(),
		TargetUserID: userIDFromContext(r),
		Data:         d,
	})
}

// scanPartResponse is the part identity a pick verdict card renders.
func scanPartResponse(p *resolvedScanPart) map[string]any {
	return map[string]any{
		"id": p.ID.String(), "mpn": p.Mpn,
		"manufacturer": p.Manufacturer, "description": p.Description,
	}
}

// PickScan handles POST /api/v1/scan/collections/{collectionId}/pick — one
// scan of the Pick loop, or a manual tick for the parts that carry no barcode.
//
// Body: {barcode} or {item_id, checked}. Responses discriminate on `status`:
//   - checked         — the row was on the list and is now ticked
//   - already_checked — it was ticked before this scan (duplicate bag)
//   - not_on_list     — resolved to a catalog part that this list doesn't hold
//   - unknown         — reason: unparsed | not_in_catalog, plus parsed fields
//   - unchecked       — manual untick (item_id path only; the undo affordance)
func (h *Handler) PickScan(w http.ResponseWriter, r *http.Request) {
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
		Barcode string `json:"barcode"`
		ItemID  string `json:"item_id"`
		Checked *bool  `json:"checked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	// Manual path: the phone already knows the row (tapped in the outstanding
	// sheet, or undoing the last scan).
	if body.ItemID != "" {
		var itemID pgtype.UUID
		if err := itemID.Scan(body.ItemID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid item_id", "BAD_REQUEST")
			return
		}
		checked := body.Checked == nil || *body.Checked
		item, err := h.Queries.UpdatePartCollectionItem(r.Context(), dbgen.UpdatePartCollectionItemParams{
			ID: itemID, CollectionID: c.ID, SetChecked: true, Checked: checked,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
			} else {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			}
			return
		}
		status := "unchecked"
		if checked {
			status = "checked"
		}
		h.finishPickWrite(w, r, sess, c, map[string]any{
			"status":     status,
			"item_id":    item.ID.String(),
			"checked_at": timestamptzString(item.CheckedAt),
		}, "")
		return
	}

	if strings.TrimSpace(body.Barcode) == "" {
		writeError(w, http.StatusBadRequest, "barcode or item_id is required", "BAD_REQUEST")
		return
	}

	parsed, parseErr := scanner.ParseAny(body.Barcode)
	if parseErr != nil || (parsed.SKU == "" && parsed.MPN == "") {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "unknown", "reason": "unparsed", "raw": body.Barcode,
		})
		return
	}
	part, err := h.resolveBarcodePart(r.Context(), sess.TeamID, parsed)
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
		})
		return
	}

	existing, err := h.Queries.GetPartCollectionItemByPart(r.Context(), dbgen.GetPartCollectionItemByPartParams{
		CollectionID: c.ID, PartID: part.ID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		// Resolved, but this list doesn't hold it — the "put it back on the
		// pile" verdict. Nothing is written, and the list is never grown by a
		// pick (that is Collect's job).
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "not_on_list",
			"part":   scanPartResponse(part),
		})
		return
	}

	label := ""
	if part.Mpn != nil {
		label = *part.Mpn
	}

	// Already ticked: report it without a write, so checked_at keeps pointing at
	// the scan that actually found the part.
	if existing.CheckedAt.Valid {
		tally, err := h.Queries.GetPartCollectionTally(r.Context(), c.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
		h.publishPickActivity(r, sess, c.Name, label, tally.CheckedCount, tally.ItemCount)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "already_checked",
			"part":       scanPartResponse(part),
			"item_id":    existing.ID.String(),
			"quantity":   existing.Quantity,
			"note":       existing.Note,
			"checked_at": timestamptzString(existing.CheckedAt),
			"tally":      pickTally(tally),
		})
		return
	}

	item, err := h.Queries.UpdatePartCollectionItem(r.Context(), dbgen.UpdatePartCollectionItemParams{
		ID: existing.ID, CollectionID: c.ID, SetChecked: true, Checked: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.finishPickWrite(w, r, sess, c, map[string]any{
		"status":     "checked",
		"part":       scanPartResponse(part),
		"item_id":    item.ID.String(),
		"quantity":   item.Quantity,
		"note":       item.Note,
		"checked_at": timestamptzString(item.CheckedAt),
	}, label)
}

// finishPickWrite touches the list, reads the fresh tally onto the response,
// fans out the owner-targeted events, and writes the reply — the tail every
// mutating pick path shares.
func (h *Handler) finishPickWrite(w http.ResponseWriter, r *http.Request, sess *dbgen.ScanSession, c dbgen.PartCollection, resp map[string]any, label string) {
	if err := h.Queries.TouchPartCollection(r.Context(), c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	tally, err := h.Queries.GetPartCollectionTally(r.Context(), c.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	resp["tally"] = pickTally(tally)
	h.publishCollectionChanged(r, sess.TeamID, c.ID.String())
	h.publishPickActivity(r, sess, c.Name, label, tally.CheckedCount, tally.ItemCount)
	writeJSON(w, http.StatusOK, resp)
}

func pickTally(t dbgen.GetPartCollectionTallyRow) map[string]any {
	return map[string]any{
		"item_count":         t.ItemCount,
		"checked_count":      t.CheckedCount,
		"total_quantity":     t.TotalQuantity,
		"unquantified_count": t.UnquantifiedCount,
	}
}

// SetScanCollectionChecked handles
// POST /api/v1/scan/collections/{collectionId}/checked — the phone's bulk
// selection op. Body: {op} (all | none | invert). Its reason for existing is
// "start fresh": beginning a pick against a list that still carries yesterday's
// checks would make the progress count a lie.
func (h *Handler) SetScanCollectionChecked(w http.ResponseWriter, r *http.Request) {
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
		Op string `json:"op"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	switch body.Op {
	case "all", "none", "invert":
	default:
		writeError(w, http.StatusBadRequest, "op must be all, none, or invert", "BAD_REQUEST")
		return
	}
	if _, err := h.Queries.SetPartCollectionItemsChecked(r.Context(), dbgen.SetPartCollectionItemsCheckedParams{
		CollectionID: c.ID, Op: body.Op,
	}); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"item_count":    tally.ItemCount,
		"checked_count": tally.CheckedCount,
	})
}
