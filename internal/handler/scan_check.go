package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/scanner"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Scanner Filter mode: scan a pile of bags AGAINST a target set and get a
// per-bag verdict. Strictly read-only — no session table, no inventory writes;
// the phone owns the tally and fetches the target set once for coverage.
//
// The target is a source descriptor, resolved fresh per request:
//   {type:"collection", collection_id}  — a saved list (owner-only)
//   {type:"build", build_id}            — computeBuildDemand + approved substitutes
//   {type:"order", order_ids:[...]}     — line items; SKU-level match first

type checkSourceReq struct {
	Type         string   `json:"type"`
	CollectionID string   `json:"collection_id,omitempty"`
	BuildID      string   `json:"build_id,omitempty"`
	OrderIDs     []string `json:"order_ids,omitempty"`
}

type checkTargetEntry struct {
	PartID        string  `json:"part_id"`
	Mpn           *string `json:"mpn"`
	Description   *string `json:"description,omitempty"`
	QtyNeeded     *int64  `json:"qty_needed,omitempty"`
	Note          *string `json:"note,omitempty"`
	Designators   string  `json:"designators,omitempty"`
	Substitute    bool    `json:"substitute,omitempty"`
	SubstituteFor string  `json:"substitute_for,omitempty"`
	// The substituted primary's part id — a substitute scan covers that
	// primary in the phone's coverage tally.
	SubstituteForPartID string `json:"substitute_for_part_id,omitempty"`
	OrderRef            string `json:"order_ref,omitempty"`
}

type checkTarget struct {
	Ref     string
	Entries map[string]*checkTargetEntry // keyed by part id
	Order   []string                     // stable listing order (primaries first)
	SKUs    map[string]*checkTargetEntry // order source: upper(SKU) → entry
}

func (t *checkTarget) add(e *checkTargetEntry) *checkTargetEntry {
	if existing, ok := t.Entries[e.PartID]; ok {
		return existing
	}
	t.Entries[e.PartID] = e
	t.Order = append(t.Order, e.PartID)
	return e
}

// checkTargetError distinguishes "target not found / bad request" from
// internal errors so the handler can map them to proper statuses.
type checkTargetError struct {
	status  int
	message string
	code    string
}

func (e *checkTargetError) Error() string { return e.message }

func checkNotFound(msg string) error {
	return &checkTargetError{status: http.StatusNotFound, message: msg, code: "NOT_FOUND"}
}

func checkBadRequest(msg string) error {
	return &checkTargetError{status: http.StatusBadRequest, message: msg, code: "BAD_REQUEST"}
}

// resolveCheckTarget materializes the source descriptor into the ephemeral
// target set. Derived targets are never persisted (spec rule: derived
// collections never materialize).
func (h *Handler) resolveCheckTarget(ctx context.Context, r *http.Request, sess *dbgen.ScanSession, src checkSourceReq) (*checkTarget, error) {
	target := &checkTarget{
		Entries: map[string]*checkTargetEntry{},
		SKUs:    map[string]*checkTargetEntry{},
	}

	switch src.Type {
	case "collection":
		var collectionID, userID pgtype.UUID
		if err := collectionID.Scan(src.CollectionID); err != nil {
			return nil, checkBadRequest("invalid collection_id")
		}
		if err := userID.Scan(userIDFromContext(r)); err != nil {
			return nil, checkBadRequest("not authenticated")
		}
		c, err := h.Queries.GetPartCollection(ctx, dbgen.GetPartCollectionParams{
			ID: collectionID, UserID: userID, TeamID: sess.TeamID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, checkNotFound("collection not found")
			}
			return nil, err
		}
		items, err := h.Queries.ListPartCollectionItems(ctx, dbgen.ListPartCollectionItemsParams{
			CollectionID: c.ID, TeamID: sess.TeamID,
		})
		if err != nil {
			return nil, err
		}
		target.Ref = c.Name
		for _, it := range items {
			e := &checkTargetEntry{
				PartID:      it.PartID.String(),
				Mpn:         it.Mpn,
				Description: &it.PartDescription,
				Note:        it.Note,
			}
			if it.Quantity != nil {
				q := int64(*it.Quantity)
				e.QtyNeeded = &q
			}
			target.add(e)
		}

	case "build":
		var buildID pgtype.UUID
		if err := buildID.Scan(src.BuildID); err != nil {
			return nil, checkBadRequest("invalid build_id")
		}
		b, err := h.Queries.GetBuild(ctx, dbgen.GetBuildParams{ID: buildID, TeamID: sess.TeamID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, checkNotFound("build not found")
			}
			return nil, err
		}
		// computeBuildDemand consumes the ForUpdate row shape; synthesize it
		// from the read row (no lock needed for a read-only check).
		demand, err := computeBuildDemand(ctx, h.Queries, dbgen.GetBuildForUpdateRow{
			ID:                  b.ID,
			TeamID:              b.TeamID,
			ProjectID:           b.ProjectID,
			BomRevisionID:       b.BomRevisionID,
			Quantity:            b.Quantity,
			Status:              b.Status,
			ExcludedDesignators: b.ExcludedDesignators,
		}, buildID)
		if err != nil {
			return nil, err
		}
		target.Ref = fmt.Sprintf("%s %s × %d", b.ProjectName, b.BomRevisionLabel, b.Quantity)

		// Display identities for everything the target may contain.
		lineItems, err := h.Queries.ListBOMLineItems(ctx, b.BomRevisionID)
		if err != nil {
			return nil, err
		}
		subs, err := h.Queries.ListSubstitutesForRevisionResolved(ctx, b.BomRevisionID)
		if err != nil {
			return nil, err
		}
		selRows, err := h.Queries.GetBuildSubstituteSelections(ctx, buildID)
		if err != nil {
			return nil, err
		}
		selectionMap := make(map[string]string, len(selRows))
		for _, s := range selRows {
			selectionMap[s.BomLineItemID.String()] = s.SelectedPartID.String()
		}

		ids := make([]pgtype.UUID, 0, len(demand)+len(subs))
		for pid := range demand {
			var u pgtype.UUID
			_ = u.Scan(pid) //nolint:errcheck // demand keys are UUID strings
			ids = append(ids, u)
		}
		for _, s := range subs {
			ids = append(ids, s.PartID)
		}
		briefRows, err := h.Queries.GetPartsBrief(ctx, ids)
		if err != nil {
			return nil, err
		}
		brief := make(map[string]dbgen.GetPartsBriefRow, len(briefRows))
		for _, p := range briefRows {
			brief[p.ID.String()] = p
		}

		// Primary entries: demand quantities + designators of the lines whose
		// effective part (selection wins) contributes to that demand.
		designators := map[string][]string{}
		primaryByLine := map[string]string{} // line item id → effective primary part id
		for _, li := range lineItems {
			if !li.ResolvedPartID.Valid || li.Ignored {
				continue
			}
			pid := li.ResolvedPartID.String()
			if sel, ok := selectionMap[li.ID.String()]; ok {
				pid = sel
			}
			if _, inDemand := demand[pid]; !inDemand {
				continue
			}
			primaryByLine[li.ID.String()] = pid
			if li.ReferenceDesignators != "" {
				designators[pid] = append(designators[pid], li.ReferenceDesignators)
			}
		}
		for pid, qty := range demand {
			q := int64(qty)
			e := &checkTargetEntry{PartID: pid, QtyNeeded: &q, Designators: strings.Join(designators[pid], ", ")}
			if p, ok := brief[pid]; ok {
				e.Mpn = p.Mpn
				e.Description = &p.Description
			}
			target.add(e)
		}
		// Approved substitutes of demanded lines (a part that is already a
		// primary elsewhere keeps its primary verdict).
		for _, s := range subs {
			primaryPID, ok := primaryByLine[s.BomLineItemID.String()]
			if !ok {
				continue
			}
			subPID := s.PartID.String()
			if _, isPrimary := target.Entries[subPID]; isPrimary {
				continue
			}
			subFor := s.ReferenceDesignators
			if subFor == "" {
				if p, ok := brief[primaryPID]; ok && p.Mpn != nil {
					subFor = *p.Mpn
				}
			}
			e := &checkTargetEntry{
				PartID:              subPID,
				Mpn:                 s.Mpn,
				Description:         &s.Description,
				Substitute:          true,
				SubstituteFor:       subFor,
				SubstituteForPartID: primaryPID,
			}
			target.add(e)
		}

	case "order":
		if len(src.OrderIDs) == 0 {
			return nil, checkBadRequest("order_ids is required")
		}
		refs := make([]string, 0, len(src.OrderIDs))
		for _, idStr := range src.OrderIDs {
			var orderID pgtype.UUID
			if err := orderID.Scan(idStr); err != nil {
				return nil, checkBadRequest("invalid order id: " + idStr)
			}
			o, err := h.Queries.GetOrder(ctx, dbgen.GetOrderParams{ID: orderID, TeamID: sess.TeamID})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, checkNotFound("order not found")
				}
				return nil, err
			}
			ref := buildOrderRef(o.ID, o.PurchaseOrderNumber, o.ExternalReference)
			refs = append(refs, ref)
			lines, err := h.Queries.GetOrderLineItems(ctx, orderID)
			if err != nil {
				return nil, err
			}
			for _, li := range lines {
				e := target.add(&checkTargetEntry{
					PartID:      li.PartID.String(),
					Mpn:         li.PartMpn,
					Description: li.PartDescription,
					OrderRef:    ref,
				})
				q := int64(li.QuantityOrdered)
				if e.QtyNeeded == nil {
					e.QtyNeeded = &q
				} else {
					*e.QtyNeeded += q
				}
				target.SKUs[strings.ToUpper(li.Sku)] = e
			}
		}
		target.Ref = "Order " + refs[0]
		if len(refs) > 1 {
			target.Ref = fmt.Sprintf("Orders %s +%d", refs[0], len(refs)-1)
		}

	default:
		return nil, checkBadRequest("source type must be collection, build, or order")
	}

	return target, nil
}

func writeCheckTargetError(w http.ResponseWriter, err error) {
	var te *checkTargetError
	if errors.As(err, &te) {
		writeError(w, te.status, te.message, te.code)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
}

// CheckTarget handles POST /api/v1/scan/check/target — the phone fetches the
// resolved target set once at session start (coverage tally + the
// "never scanned" summary are computed client-side).
func (h *Handler) CheckTarget(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	var body struct {
		Source checkSourceReq `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	target, err := h.resolveCheckTarget(r.Context(), r, sess, body.Source)
	if err != nil {
		writeCheckTargetError(w, err)
		return
	}
	entries := make([]*checkTargetEntry, 0, len(target.Order))
	for _, pid := range target.Order {
		entries = append(entries, target.Entries[pid])
	}
	writeJSON(w, http.StatusOK, map[string]any{"ref": target.Ref, "entries": entries})
}

// CheckScan handles POST /api/v1/scan/check — one scan of the Filter loop.
// Body: {barcode, source, progress?: {done, total}}. Read-only. The optional
// progress echo (the phone's coverage tally) rides into the scanner_activity
// event so the future activity island can render it.
func (h *Handler) CheckScan(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	var body struct {
		Barcode  string         `json:"barcode"`
		Source   checkSourceReq `json:"source"`
		Progress *struct {
			Done  int `json:"done"`
			Total int `json:"total"`
		} `json:"progress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Barcode) == "" {
		writeError(w, http.StatusBadRequest, "barcode is required", "BAD_REQUEST")
		return
	}
	target, err := h.resolveCheckTarget(r.Context(), r, sess, body.Source)
	if err != nil {
		writeCheckTargetError(w, err)
		return
	}

	verdict := "unknown"
	var entry *checkTargetEntry
	var partResp map[string]any

	parsed, parseErr := scanner.ParseAny(body.Barcode)
	if parseErr == nil && (parsed.SKU != "" || parsed.MPN != "") {
		// Order sources match at listing level first (distributor+SKU is the
		// listing's identity; orders are per-distributor so SKU is decisive).
		if parsed.SKU != "" {
			if e, ok := target.SKUs[strings.ToUpper(parsed.SKU)]; ok {
				entry = e
				verdict = "match"
				partResp = map[string]any{"id": e.PartID, "mpn": e.Mpn, "description": e.Description}
			}
		}
		if entry == nil {
			part, err := h.resolveBarcodePart(r.Context(), sess.TeamID, parsed)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
				return
			}
			if part != nil {
				partResp = map[string]any{
					"id": part.ID.String(), "mpn": part.Mpn,
					"manufacturer": part.Manufacturer, "description": part.Description,
				}
				if e, ok := target.Entries[part.ID.String()]; ok {
					entry = e
					if e.Substitute {
						verdict = "substitute"
					} else {
						verdict = "match"
					}
				} else {
					verdict = "miss"
				}
			}
		}
	}

	resp := map[string]any{"verdict": verdict, "part": partResp}
	if entry != nil {
		resp["entry"] = entry
	}

	// scanner_activity: filter is a solo activity — target the scanning user.
	var progress any
	if body.Progress != nil {
		progress = map[string]any{"done": body.Progress.Done, "total": body.Progress.Total, "unit": "parts"}
	}
	label := verdict
	if partResp != nil {
		if mpn, ok := partResp["mpn"].(*string); ok && mpn != nil {
			label = verdict + " " + *mpn
		}
	}
	d, _ := json.Marshal(map[string]any{
		"type":     "filter",
		"ref":      target.Ref,
		"state":    "in_progress",
		"progress": progress,
		"last":     map[string]string{"label": label, "at": time.Now().UTC().Format(time.RFC3339)},
	})
	h.Worker.Publish(worker.Event{
		Type: "scanner_activity", TeamID: sess.TeamID.String(),
		TargetUserID: userIDFromContext(r), Data: d,
	})

	writeJSON(w, http.StatusOK, resp)
}

// CheckFinish handles POST /api/v1/scan/check/finish — records the session
// summary as an activity entry (the durable trace of a filter run) and closes
// the scanner_activity stream. Body: {ref, seen, total}.
func (h *Handler) CheckFinish(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	var body struct {
		Ref   string `json:"ref"`
		Seen  int    `json:"seen"`
		Total int    `json:"total"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	h.scanActivity(sess, activity.Event{
		Action:  "scan.filter_finished",
		Feed:    activity.FeedPersonal,
		Summary: fmt.Sprintf("Filter finished: %d/%d parts of %s seen", body.Seen, body.Total, body.Ref),
		Values:  map[string]any{"ref": body.Ref, "seen": body.Seen, "total": body.Total},
	})
	d, _ := json.Marshal(map[string]any{
		"type": "filter", "ref": body.Ref, "state": "finished",
		"progress": map[string]any{"done": body.Seen, "total": body.Total, "unit": "parts"},
	})
	h.Worker.Publish(worker.Event{
		Type: "scanner_activity", TeamID: sess.TeamID.String(),
		TargetUserID: userIDFromContext(r), Data: d,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ListScanBuilds handles GET /api/v1/scan/builds — the phone's filter-source
// build picker.
func (h *Handler) ListScanBuilds(w http.ResponseWriter, r *http.Request) {
	sess, err := h.scanSessionFromScanToken(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if sess == nil {
		w.WriteHeader(http.StatusGone)
		return
	}
	rows, err := h.Queries.ListBuildsForScanPicker(r.Context(), sess.TeamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	type item struct {
		ID        string `json:"id"`
		Ref       string `json:"ref"`
		Status    string `json:"status"`
		Quantity  int32  `json:"quantity"`
		CreatedAt string `json:"created_at"`
	}
	resp := make([]item, 0, len(rows))
	for _, b := range rows {
		resp = append(resp, item{
			ID:        b.ID.String(),
			Ref:       fmt.Sprintf("%s %s × %d", b.ProjectName, b.RevisionLabel, b.Quantity),
			Status:    b.Status,
			Quantity:  b.Quantity,
			CreatedAt: b.CreatedAt.Time.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"builds": resp})
}
