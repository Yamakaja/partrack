package handler

import (
	"net/http"
	"strings"
	"sync"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type searchResults struct {
	Parts            []dbgen.SearchPartsRow            `json:"parts"`
	Projects         []dbgen.SearchProjectsRow         `json:"projects"`
	Revisions        []dbgen.SearchBOMRevisionsRow     `json:"revisions"`
	Orders           []dbgen.SearchOrdersRow           `json:"orders"`
	Builds           []dbgen.SearchBuildsRow           `json:"builds"`
	StorageLocations []dbgen.SearchStorageLocationsRow `json:"storage_locations"`
}

// GlobalSearch handles GET /api/v1/teams/{teamId}/search?q=...
func (h *Handler) GlobalSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, searchResults{
			Parts:            []dbgen.SearchPartsRow{},
			Projects:         []dbgen.SearchProjectsRow{},
			Revisions:        []dbgen.SearchBOMRevisionsRow{},
			Orders:           []dbgen.SearchOrdersRow{},
			Builds:           []dbgen.SearchBuildsRow{},
			StorageLocations: []dbgen.SearchStorageLocationsRow{},
		})
		return
	}

	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team_id", "BAD_REQUEST")
		return
	}

	ctx := r.Context()

	// Pre-initialise with empty (non-nil) slices so JSON always encodes as [].
	results := searchResults{
		Parts:            []dbgen.SearchPartsRow{},
		Projects:         []dbgen.SearchProjectsRow{},
		Revisions:        []dbgen.SearchBOMRevisionsRow{},
		Orders:           []dbgen.SearchOrdersRow{},
		Builds:           []dbgen.SearchBuildsRow{},
		StorageLocations: []dbgen.SearchStorageLocationsRow{},
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		runErr error
	)

	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				if runErr == nil {
					runErr = err
				}
				mu.Unlock()
			}
		}()
	}

	run(func() error {
		rows, err := h.Queries.SearchParts(ctx, dbgen.SearchPartsParams{
			TeamID: teamID,
			Q:      &q,
			Lim:    5,
		})
		if err != nil {
			return err
		}
		if rows != nil {
			mu.Lock()
			results.Parts = rows
			mu.Unlock()
		}
		return nil
	})

	run(func() error {
		rows, err := h.Queries.SearchProjects(ctx, dbgen.SearchProjectsParams{
			TeamID: teamID,
			Q:      &q,
			Lim:    3,
		})
		if err != nil {
			return err
		}
		if rows != nil {
			mu.Lock()
			results.Projects = rows
			mu.Unlock()
		}
		return nil
	})

	run(func() error {
		rows, err := h.Queries.SearchBOMRevisions(ctx, dbgen.SearchBOMRevisionsParams{
			TeamID: teamID,
			Q:      &q,
			Lim:    3,
		})
		if err != nil {
			return err
		}
		if rows != nil {
			mu.Lock()
			results.Revisions = rows
			mu.Unlock()
		}
		return nil
	})

	run(func() error {
		rows, err := h.Queries.SearchOrders(ctx, dbgen.SearchOrdersParams{
			TeamID: teamID,
			Q:      &q,
			Lim:    3,
		})
		if err != nil {
			return err
		}
		if rows != nil {
			mu.Lock()
			results.Orders = rows
			mu.Unlock()
		}
		return nil
	})

	run(func() error {
		rows, err := h.Queries.SearchBuilds(ctx, dbgen.SearchBuildsParams{
			TeamID: teamID,
			Q:      &q,
			Lim:    3,
		})
		if err != nil {
			return err
		}
		if rows != nil {
			mu.Lock()
			results.Builds = rows
			mu.Unlock()
		}
		return nil
	})

	run(func() error {
		rows, err := h.Queries.SearchStorageLocations(ctx, dbgen.SearchStorageLocationsParams{
			TeamID: teamID,
			Q:      &q,
			Lim:    3,
		})
		if err != nil {
			return err
		}
		if rows != nil {
			mu.Lock()
			results.StorageLocations = rows
			mu.Unlock()
		}
		return nil
	})

	wg.Wait()

	if runErr != nil {
		writeError(w, http.StatusInternalServerError, "search failed", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, results)
}
