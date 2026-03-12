// Package handler contains HTTP handler implementations for all partrack API
// endpoints. All handlers are methods on the Handler struct which holds
// shared dependencies.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Yamakaja/partrack/internal/config"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	DB           *pgxpool.Pool
	Queries      *dbgen.Queries
	Config       *config.Config
	Worker       *worker.Queue
	Cache        *middleware.AuthCache
	Distributors distributor.Registry
	CartResolver *CartResolver
	oidcEntries  map[string]*oidcEntry // populated by initOIDC() in auth.go
	refreshGroup singleflight.Group    // deduplicates concurrent credential enrichment per (team, distributor)
}

// getEnrichedCreds returns credentials for ordering API calls.
// If the distributor client implements CredentialEnricher, it enriches the base
// 2-legged credentials with runtime-resolved tokens (e.g. OAuth).
// Concurrent calls for the same (team, distributor) are deduplicated via singleflight.
func (h *Handler) getEnrichedCreds(r *http.Request, order dbgen.GetOrderRow) (json.RawMessage, error) {
	ctx := r.Context()

	if order.DistributorApiType == nil {
		return nil, fmt.Errorf("distributor has no api_type")
	}
	client := h.Distributors.Lookup(*order.DistributorApiType)

	cfg, err := h.Queries.GetDistributorConfigRaw(ctx, dbgen.GetDistributorConfigRawParams{
		TeamID:        order.TeamID,
		DistributorID: order.DistributorID,
	})
	if err != nil {
		return nil, fmt.Errorf("get distributor config: %w", err)
	}

	ce, ok := client.(distributor.CredentialEnricher)
	if !ok {
		return cfg.ApiCredentials, nil
	}

	user := middleware.UserFromContext(r.Context())
	var userID pgtype.UUID
	userID.Scan(user.ID) //nolint:errcheck

	provider := &dbCredentialProvider{
		queries:       h.Queries,
		teamID:        order.TeamID,
		userID:        userID,
		distributorID: order.DistributorID,
	}
	key := order.TeamID.String() + "/" + user.ID + "/" + order.DistributorID.String()
	result, err, _ := h.refreshGroup.Do(key, func() (any, error) {
		return ce.EnrichCredentials(ctx, cfg.ApiCredentials, provider)
	})
	if err != nil {
		return nil, err
	}
	return result.(json.RawMessage), nil
}

// New creates a Handler and initialises OIDC providers.
func New(db *pgxpool.Pool, cfg *config.Config, q *worker.Queue, cache *middleware.AuthCache, reg distributor.Registry) *Handler {
	h := &Handler{
		DB:           db,
		Queries:      dbgen.New(db),
		Config:       cfg,
		Worker:       q,
		Cache:        cache,
		Distributors: reg,
		CartResolver: NewCartResolver(),
	}
	h.initOIDC()
	return h
}

// writeError writes a JSON error response. For 5xx statuses it logs the
// message and a stack trace to help with debugging.
func writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"error": message,
		"code":  code,
	})
}

// writeJSON encodes v as JSON and writes it with status 200.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// userIDFromContext returns the authenticated user's ID, or "" if not present.
func userIDFromContext(r *http.Request) string {
	if u := middleware.UserFromContext(r.Context()); u != nil {
		return u.ID
	}
	return ""
}

// checkTeamMembership returns the team membership for (teamID, userID),
// consulting the auth cache before hitting the database. Returns nil, nil if
// the user is not a member.
func (h *Handler) checkTeamMembership(ctx context.Context, teamID, userID pgtype.UUID) (*middleware.TeamMembership, error) {
	teamIDStr := teamID.String()
	userIDStr := userID.String()

	if role, ok := h.Cache.LookupMembership(teamIDStr, userIDStr); ok {
		return &middleware.TeamMembership{TeamID: teamIDStr, UserID: userIDStr, Role: role}, nil
	}

	row, err := h.Queries.GetTeamMembership(ctx, dbgen.GetTeamMembershipParams{
		TeamID: teamID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil //nolint:nilnil
		}
		return nil, err
	}
	h.Cache.StoreMembership(teamIDStr, userIDStr, string(row.Role))
	return &middleware.TeamMembership{TeamID: teamIDStr, UserID: userIDStr, Role: string(row.Role)}, nil
}

// publishEvent marshals data and publishes a team-scoped SSE event, skipping the acting user's channel.
func (h *Handler) publishEvent(r *http.Request, eventType, teamID string, data map[string]string) {
	d, _ := json.Marshal(data)
	h.Worker.Publish(worker.Event{Type: eventType, TeamID: teamID, UserID: userIDFromContext(r), Data: d})
}
