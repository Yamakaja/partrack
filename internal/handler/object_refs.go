package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/objectref"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// maxObjectRefsPerRequest caps one batch resolve. Well above what any screen
// references at once (a long note, a full table page), low enough that the
// endpoint can't be used to enumerate a team's objects cheaply.
const maxObjectRefsPerRequest = 200

// ListObjectRefs handles GET /api/v1/teams/{teamId}/object-refs
//
// Query params are one per kind, each a comma-separated id list:
//
//	?part=<uuid>,<uuid>&order=<uuid>&build=<uuid>
//
// It answers "what are these things called?" for ids a client already holds but
// has no display fields for — a build's order_ids, a note's link chips, an
// activity ref written before labels were snapshotted. The client batches every
// unresolved id on screen into one call rather than fetching each object.
//
// Ids the team can't see are simply absent from the response; that is not an
// error, since a note may outlive the object it links to.
func (h *Handler) ListObjectRefs(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team id", "BAD_REQUEST")
		return
	}

	byKind := map[string][]string{}
	total := 0
	for _, kind := range objectref.Kinds {
		raw := r.URL.Query().Get(string(kind))
		if raw == "" {
			continue
		}
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				byKind[string(kind)] = append(byKind[string(kind)], id)
				total++
			}
		}
	}
	if total > maxObjectRefsPerRequest {
		writeError(w, http.StatusBadRequest, "too many ids requested", "TOO_MANY_IDS")
		return
	}

	objects, err := h.resolveObjectSummaries(r.Context(), teamID, byKind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"objects": objects})
}

// resolveObjectSummaries batch-resolves ids, grouped by kind, into display
// summaries keyed by id. One query per kind present — never one per object.
//
// Every query is team-scoped at the data layer, so this cannot surface another
// team's objects regardless of the ids supplied. Unknown ids are dropped.
func (h *Handler) resolveObjectSummaries(
	ctx context.Context,
	teamID pgtype.UUID,
	byKind map[string][]string,
) (map[string]objectref.Summary, error) {
	out := make(map[string]objectref.Summary)
	tid := teamID.String()

	for kind, ids := range byKind {
		pg := toPgUUIDs(ids)
		if len(pg) == 0 {
			continue
		}

		switch objectref.Kind(kind) {
		case objectref.KindPart:
			rows, err := h.Queries.GetPartsForManifest(ctx, dbgen.GetPartsForManifestParams{Ids: pg, TeamID: teamID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				id := r.ID.String()
				out[id] = objectref.Summary{
					Kind:      objectref.KindPart,
					ID:        id,
					Label:     objectref.PartLabel(r.Mpn, r.ShortDescription),
					Qualifier: derefStr(r.Manufacturer),
					Status:    string(r.LifecycleStatus),
					Href:      objectref.Href(objectref.KindPart, tid, id, ""),
				}
			}

		case objectref.KindProject:
			rows, err := h.Queries.GetProjectsByIDs(ctx, dbgen.GetProjectsByIDsParams{Ids: pg, TeamID: teamID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				id := r.ID.String()
				out[id] = objectref.Summary{
					Kind:   objectref.KindProject,
					ID:     id,
					Label:  r.Name,
					Status: string(r.Status),
					Href:   objectref.Href(objectref.KindProject, tid, id, ""),
				}
			}

		case objectref.KindRevision:
			rows, err := h.Queries.GetBOMRevisionsByIDs(ctx, dbgen.GetBOMRevisionsByIDsParams{Ids: pg, TeamID: teamID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				id := r.ID.String()
				out[id] = objectref.Summary{
					Kind:  objectref.KindRevision,
					ID:    id,
					Label: objectref.RevisionLabel(r.ProjectName, r.Label),
					Href:  objectref.Href(objectref.KindRevision, tid, id, r.ProjectID.String()),
				}
			}

		case objectref.KindOrder:
			rows, err := h.Queries.GetOrdersByIDs(ctx, dbgen.GetOrdersByIDsParams{Ids: pg, TeamID: teamID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				id := r.ID.String()
				out[id] = objectref.Summary{
					Kind:   objectref.KindOrder,
					ID:     id,
					Label:  objectref.OrderLabel(r.DistributorName, r.PurchaseOrderNumber, r.ExternalReference, id),
					Status: string(r.Status),
					Href:   objectref.Href(objectref.KindOrder, tid, id, ""),
				}
			}

		case objectref.KindBuild:
			rows, err := h.Queries.GetBuildsByIDs(ctx, dbgen.GetBuildsByIDsParams{Ids: pg, TeamID: teamID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				id := r.ID.String()
				out[id] = objectref.Summary{
					Kind:      objectref.KindBuild,
					ID:        id,
					Label:     objectref.RevisionLabel(r.ProjectName, r.RevisionLabel),
					Qualifier: "×" + strconv.FormatInt(int64(r.Quantity), 10),
					Status:    r.Status,
					Href:      objectref.Href(objectref.KindBuild, tid, id, ""),
				}
			}

		case objectref.KindStorage:
			rows, err := h.Queries.GetStorageLocationsByIDs(ctx, dbgen.GetStorageLocationsByIDsParams{Ids: pg, TeamID: teamID})
			if err != nil {
				return nil, err
			}
			for _, r := range rows {
				id := r.ID.String()
				out[id] = objectref.Summary{
					Kind:  objectref.KindStorage,
					ID:    id,
					Label: r.Label,
					Href:  objectref.Href(objectref.KindStorage, tid, id, ""),
				}
			}
		}
	}
	return out, nil
}
