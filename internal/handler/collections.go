package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Part collections ("Lists") are user-private within a team: every data-layer
// query filters by (user_id, team_id), so another user's collection id simply
// does not resolve (404) — the same data-layer IDOR closure the rest of the
// app uses for teams, applied one level down.

type collectionResponse struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Archived          bool   `json:"archived"`
	CollectCaptureQty *bool  `json:"collect_capture_qty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	ItemCount         int32  `json:"item_count"`
	TotalQuantity     int64  `json:"total_quantity"`
	UnquantifiedCount int32  `json:"unquantified_count"`
}

type collectionItemResponse struct {
	ID              string  `json:"id"`
	PartID          string  `json:"part_id"`
	Quantity        *int32  `json:"quantity"`
	Note            *string `json:"note"`
	Mpn             *string `json:"mpn"`
	Manufacturer    *string `json:"manufacturer"`
	Description     string  `json:"description"`
	LifecycleStatus string  `json:"lifecycle_status"`
}

func collectionToResponse(c dbgen.PartCollection) collectionResponse {
	return collectionResponse{
		ID:                c.ID.String(),
		Name:              c.Name,
		Archived:          c.Archived,
		CollectCaptureQty: c.CollectCaptureQty,
		CreatedAt:         c.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:         c.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func collectionItemToResponse(it dbgen.ListPartCollectionItemsRow) collectionItemResponse {
	return collectionItemResponse{
		ID:              it.ID.String(),
		PartID:          it.PartID.String(),
		Quantity:        it.Quantity,
		Note:            it.Note,
		Mpn:             it.Mpn,
		Manufacturer:    it.Manufacturer,
		Description:     it.PartDescription,
		LifecycleStatus: string(it.LifecycleStatus),
	}
}

// collectionScope parses the team id from the URL and the acting user from the
// context. Both are required for every collection query's owner filter.
func collectionScope(w http.ResponseWriter, r *http.Request) (userID, teamID pgtype.UUID, ok bool) {
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return userID, teamID, false
	}
	if err := userID.Scan(userIDFromContext(r)); err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated", "UNAUTHORIZED")
		return userID, teamID, false
	}
	return userID, teamID, true
}

// requireCollectionOwned loads the collection iff it belongs to the acting
// user within this team; otherwise writes 404 (existence is not revealed).
func (h *Handler) requireCollectionOwned(w http.ResponseWriter, r *http.Request, userID, teamID pgtype.UUID) (dbgen.PartCollection, bool) {
	var collectionID pgtype.UUID
	if err := collectionID.Scan(chi.URLParam(r, "collectionId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid collection id", "BAD_REQUEST")
		return dbgen.PartCollection{}, false
	}
	c, err := h.Queries.GetPartCollection(r.Context(), dbgen.GetPartCollectionParams{
		ID: collectionID, UserID: userID, TeamID: teamID,
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

// publishCollectionChanged notifies the owner's OTHER devices/tabs. Collections
// are private, so the event targets the owner only (TargetUserID delivers to
// every subscription of that user and nobody else) — unlike publishEvent, which
// would skip the actor and broadcast to teammates who can't see the list.
func (h *Handler) publishCollectionChanged(r *http.Request, teamID pgtype.UUID, collectionID string) {
	d, _ := json.Marshal(map[string]string{"collection_id": collectionID})
	h.Worker.Publish(worker.Event{
		Type:         "collection_changed",
		TeamID:       teamID.String(),
		TargetUserID: userIDFromContext(r),
		Data:         d,
	})
}

// ListCollections handles GET /api/v1/teams/{teamId}/collections
// Query params: archived=true to include archived lists.
func (h *Handler) ListCollections(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPartCollections(r.Context(), dbgen.ListPartCollectionsParams{
		UserID:          userID,
		TeamID:          teamID,
		IncludeArchived: r.URL.Query().Get("archived") == "true",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	resp := make([]collectionResponse, 0, len(rows))
	for _, c := range rows {
		resp = append(resp, collectionResponse{
			ID:                c.ID.String(),
			Name:              c.Name,
			Archived:          c.Archived,
			CollectCaptureQty: c.CollectCaptureQty,
			CreatedAt:         c.CreatedAt.Time.UTC().Format(time.RFC3339),
			UpdatedAt:         c.UpdatedAt.Time.UTC().Format(time.RFC3339),
			ItemCount:         c.ItemCount,
			TotalQuantity:     c.TotalQuantity,
			UnquantifiedCount: c.UnquantifiedCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": resp})
}

// CreateCollection handles POST /api/v1/teams/{teamId}/collections
// Body: {name?} — blank name gets an auto-name ("Untitled list — Jul 17").
func (h *Handler) CreateCollection(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
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
		UserID: userID, TeamID: teamID, Name: body.Name,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, teamID, c.ID.String())
	writeJSON(w, http.StatusCreated, collectionToResponse(c))
}

// GetCollection handles GET /api/v1/teams/{teamId}/collections/{collectionId}
// Returns the collection plus its items in insertion order.
func (h *Handler) GetCollection(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	c, ok := h.requireCollectionOwned(w, r, userID, teamID)
	if !ok {
		return
	}
	items, err := h.Queries.ListPartCollectionItems(r.Context(), c.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	itemsResp := make([]collectionItemResponse, 0, len(items))
	for _, it := range items {
		itemsResp = append(itemsResp, collectionItemToResponse(it))
	}
	resp := collectionToResponse(c)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": resp.ID, "name": resp.Name, "archived": resp.Archived,
		"collect_capture_qty": resp.CollectCaptureQty,
		"created_at":          resp.CreatedAt, "updated_at": resp.UpdatedAt,
		"items": itemsResp,
	})
}

// UpdateCollection handles PATCH /api/v1/teams/{teamId}/collections/{collectionId}
// Body: {name?, archived?, collect_capture_qty?} — all optional.
func (h *Handler) UpdateCollection(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	c, ok := h.requireCollectionOwned(w, r, userID, teamID)
	if !ok {
		return
	}
	var body struct {
		Name              *string `json:"name"`
		Archived          *bool   `json:"archived"`
		CollectCaptureQty *bool   `json:"collect_capture_qty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if body.Name != nil && *body.Name == "" {
		writeError(w, http.StatusBadRequest, "name must not be empty", "BAD_REQUEST")
		return
	}
	updated, err := h.Queries.UpdatePartCollection(r.Context(), dbgen.UpdatePartCollectionParams{
		ID: c.ID, UserID: userID, TeamID: teamID,
		Name:              body.Name,
		Archived:          body.Archived,
		CollectCaptureQty: body.CollectCaptureQty,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, teamID, updated.ID.String())
	writeJSON(w, http.StatusOK, collectionToResponse(updated))
}

// DeleteCollection handles DELETE /api/v1/teams/{teamId}/collections/{collectionId}
// Hard delete — lists are private scratch data with no audit value. Archive
// (PATCH archived=true) is the soft path the UI prefers.
func (h *Handler) DeleteCollection(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	c, ok := h.requireCollectionOwned(w, r, userID, teamID)
	if !ok {
		return
	}
	if _, err := h.Queries.DeletePartCollection(r.Context(), dbgen.DeletePartCollectionParams{
		ID: c.ID, UserID: userID, TeamID: teamID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, teamID, c.ID.String())
	w.WriteHeader(http.StatusNoContent)
}

// AddCollectionItem handles POST /api/v1/teams/{teamId}/collections/{collectionId}/items
// Body: {part_id, quantity?, note?}. Adding an existing part merges: a carried
// quantity sums onto the row, a bare add leaves the stored quantity untouched.
func (h *Handler) AddCollectionItem(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	c, ok := h.requireCollectionOwned(w, r, userID, teamID)
	if !ok {
		return
	}
	var body struct {
		PartID   string  `json:"part_id"`
		Quantity *int32  `json:"quantity"`
		Note     *string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	var partID pgtype.UUID
	if err := partID.Scan(body.PartID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid part_id", "BAD_REQUEST")
		return
	}
	if body.Quantity != nil && *body.Quantity <= 0 {
		writeError(w, http.StatusBadRequest, "quantity must be positive", "BAD_REQUEST")
		return
	}
	// Lists hold resolved catalog parts only; a team-scoped part of another
	// team must not be reachable through a list.
	if _, ok := h.requirePartInTeamOrGlobal(w, r, partID, teamID); !ok {
		return
	}

	item, err := h.Queries.UpsertPartCollectionItem(r.Context(), dbgen.UpsertPartCollectionItemParams{
		CollectionID: c.ID,
		PartID:       partID,
		Quantity:     body.Quantity,
		Note:         body.Note,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := h.Queries.TouchPartCollection(r.Context(), c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, teamID, c.ID.String())
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":       item.ID.String(),
		"part_id":  item.PartID.String(),
		"quantity": item.Quantity,
		"note":     item.Note,
	})
}

// BulkAddCollectionItems handles POST /api/v1/teams/{teamId}/collections/{collectionId}/items/bulk
// Body: {items: [{part_id, quantity?, note?}]}. Upserts every item with the
// collection merge rule in one transaction — the batch counterpart of
// AddCollectionItem, used by "Add from cart" (and later builds/orders).
func (h *Handler) BulkAddCollectionItems(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	c, ok := h.requireCollectionOwned(w, r, userID, teamID)
	if !ok {
		return
	}
	var body struct {
		Items []struct {
			PartID   string  `json:"part_id"`
			Quantity *int32  `json:"quantity"`
			Note     *string `json:"note"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	if len(body.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items is required and must not be empty", "BAD_REQUEST")
		return
	}

	// Parse and authorize everything before writing anything.
	type parsedItem struct {
		PartID   pgtype.UUID
		Quantity *int32
		Note     *string
	}
	parsed := make([]parsedItem, 0, len(body.Items))
	for _, it := range body.Items {
		var pi parsedItem
		if err := pi.PartID.Scan(it.PartID); err != nil {
			writeError(w, http.StatusBadRequest, "invalid part_id: "+it.PartID, "BAD_REQUEST")
			return
		}
		if it.Quantity != nil && *it.Quantity <= 0 {
			writeError(w, http.StatusBadRequest, "item quantity must be positive", "BAD_REQUEST")
			return
		}
		pi.Quantity = it.Quantity
		pi.Note = it.Note
		if _, ok := h.requirePartInTeamOrGlobal(w, r, pi.PartID, teamID); !ok {
			return
		}
		parsed = append(parsed, pi)
	}

	tx, err := h.DB.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	qtx := h.Queries.WithTx(tx)

	for _, pi := range parsed {
		if _, err := qtx.UpsertPartCollectionItem(r.Context(), dbgen.UpsertPartCollectionItemParams{
			CollectionID: c.ID,
			PartID:       pi.PartID,
			Quantity:     pi.Quantity,
			Note:         pi.Note,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
			return
		}
	}
	if err := qtx.TouchPartCollection(r.Context(), c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, teamID, c.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{"added": len(parsed)})
}

// UpdateCollectionItem handles PATCH /api/v1/teams/{teamId}/collections/{collectionId}/items/{itemId}
// Body: {quantity?, note?}. Fields are set-if-present — an explicit null clears
// (quantity back to "no quantity", note removed), an absent field is untouched.
func (h *Handler) UpdateCollectionItem(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	c, ok := h.requireCollectionOwned(w, r, userID, teamID)
	if !ok {
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	// Raw messages distinguish "absent" (leave untouched) from "null" (clear).
	var body struct {
		Quantity json.RawMessage `json:"quantity"`
		Note     json.RawMessage `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}
	params := dbgen.UpdatePartCollectionItemParams{ID: itemID, CollectionID: c.ID}
	if body.Quantity != nil {
		params.SetQuantity = true
		if err := json.Unmarshal(body.Quantity, &params.Quantity); err != nil {
			writeError(w, http.StatusBadRequest, "invalid quantity", "BAD_REQUEST")
			return
		}
		if params.Quantity != nil && *params.Quantity <= 0 {
			writeError(w, http.StatusBadRequest, "quantity must be positive", "BAD_REQUEST")
			return
		}
	}
	if body.Note != nil {
		params.SetNote = true
		if err := json.Unmarshal(body.Note, &params.Note); err != nil {
			writeError(w, http.StatusBadRequest, "invalid note", "BAD_REQUEST")
			return
		}
	}

	item, err := h.Queries.UpdatePartCollectionItem(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		}
		return
	}
	if err := h.Queries.TouchPartCollection(r.Context(), c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, teamID, c.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       item.ID.String(),
		"part_id":  item.PartID.String(),
		"quantity": item.Quantity,
		"note":     item.Note,
	})
}

// DeleteCollectionItem handles DELETE /api/v1/teams/{teamId}/collections/{collectionId}/items/{itemId}
func (h *Handler) DeleteCollectionItem(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := collectionScope(w, r)
	if !ok {
		return
	}
	c, ok := h.requireCollectionOwned(w, r, userID, teamID)
	if !ok {
		return
	}
	var itemID pgtype.UUID
	if err := itemID.Scan(chi.URLParam(r, "itemId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id", "BAD_REQUEST")
		return
	}
	n, err := h.Queries.DeletePartCollectionItem(r.Context(), dbgen.DeletePartCollectionItemParams{
		ID: itemID, CollectionID: c.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "item not found", "NOT_FOUND")
		return
	}
	if err := h.Queries.TouchPartCollection(r.Context(), c.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	h.publishCollectionChanged(r, teamID, c.ID.String())
	w.WriteHeader(http.StatusNoContent)
}
