// Package handler contains HTTP handler implementations for all partrack API
// endpoints. All handlers are methods on the Handler struct which holds
// shared dependencies.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Yamakaja/partrack/internal/activity"
	"github.com/Yamakaja/partrack/internal/blob"
	"github.com/Yamakaja/partrack/internal/config"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/llm"
	"github.com/Yamakaja/partrack/internal/mfrmatch"
	"github.com/Yamakaja/partrack/internal/middleware"
	"github.com/Yamakaja/partrack/internal/worker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
)

// Health is a liveness probe: it responds 200 OK as long as the process can
// serve requests. It deliberately does not touch the database, so a transient
// DB outage doesn't cause an orchestrator to kill an otherwise-healthy process.
// Used by the Docker HEALTHCHECK.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready is a readiness probe: it verifies the database is reachable and returns
// 503 otherwise, so a load balancer can stop routing traffic to an instance that
// can't serve real requests (without triggering a restart the way liveness would).
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.DB.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unavailable",
			"error":  "database unreachable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// record writes an activity-log row for a web mutation, filling actor (from the
// authenticated user) and source. The caller sets TeamID, Action, Feed, and the
// structured fields. This is additive to publishEvent: publishEvent handles cache
// invalidation, record handles the durable audit + live feed.
func (h *Handler) record(r *http.Request, e activity.Event) {
	if user := middleware.UserFromContext(r.Context()); user != nil {
		var uid pgtype.UUID
		if uid.Scan(user.ID) == nil {
			e.ActorID = uid
		}
		e.ActorLabel = user.DisplayName
	}
	if e.Source == "" {
		e.Source = activity.SourceWeb
	}
	h.Activity.Record(e)
}

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	DB            *pgxpool.Pool
	Queries       *dbgen.Queries
	Config        *config.Config
	Worker        *worker.Queue
	Cache         *middleware.AuthCache
	Distributors  distributor.Registry
	Manufacturers *mfrmatch.Resolver // manufacturer-name equivalence resolver (shared with the worker)
	Blobs         blob.Store
	LLM           llm.Client // nil unless config.LLM.Active(); the BOM-interpretation fallback
	CartResolver  *CartResolver
	ScanHub       *ScanHub
	Tickets       *ticketStore
	Activity      *activity.Recorder
	oidcEntries   map[string]*oidcEntry // populated by initOIDC() in auth.go
	ldapEntries   map[string]*ldapEntry // populated by initLDAP() in auth_ldap.go
	refreshGroup  singleflight.Group    // deduplicates concurrent credential enrichment per (team, distributor)
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
func New(db *pgxpool.Pool, cfg *config.Config, q *worker.Queue, cache *middleware.AuthCache, reg distributor.Registry, store blob.Store) *Handler {
	h := &Handler{
		DB:            db,
		Queries:       dbgen.New(db),
		Config:        cfg,
		Worker:        q,
		Cache:         cache,
		Distributors:  reg,
		Manufacturers: q.Manufacturers(),
		Blobs:         store,
		CartResolver:  NewCartResolver(),
		ScanHub:       NewScanHub(),
		Tickets:       newTicketStore(),
	}
	if cfg.LLM.Active() {
		h.LLM = llm.New(llm.Options{
			BaseURL:         cfg.LLM.BaseURL,
			APIKey:          cfg.LLM.APIKey,
			Model:           cfg.LLM.Model,
			FallbackModel:   cfg.LLM.FallbackModel,
			Timeout:         time.Duration(cfg.LLM.TimeoutSeconds) * time.Second,
			Debug:           cfg.LLM.Debug,
			ReasoningEffort: cfg.LLM.ReasoningEffort,
			ReasoningStyle:  cfg.LLM.ReasoningStyle,
		})
	}
	h.Activity = activity.New(db, func(eventType, teamID, targetUserID string, data []byte) {
		q.Publish(worker.Event{Type: eventType, TeamID: teamID, TargetUserID: targetUserID, Data: data})
	})
	h.Activity.Start(context.Background())
	// Let worker jobs (BOM import, etc.) record activity too.
	q.SetActivity(h.Activity)
	h.initOIDC()
	h.initLDAP()
	q.SetCartInvalidate(h.CartResolver.Invalidate)
	return h
}

// canonicalManufacturer returns the canonical display spelling for a manufacturer
// name, or the input unchanged when the spelling is unknown or no resolver is
// configured. Aligns "find existing part" lookups with the canonical name new parts
// are stored under (see internal/mfrmatch).
func (h *Handler) canonicalManufacturer(name string) string {
	if h.Manufacturers == nil {
		return name
	}
	canon, _ := h.Manufacturers.CanonicalName(name)
	return canon
}

// writeError writes a JSON error response. For 5xx statuses it logs the
// message and a stack trace to help with debugging. When a concrete error
// value is available, prefer serverError so the underlying cause is logged too.
func writeError(w http.ResponseWriter, status int, message, code string) {
	if status >= 500 {
		slog.Error("request failed",
			"code", code,
			"message", message,
			"stack", string(debug.Stack()),
		)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"error": message,
		"code":  code,
	})
}

// serverError logs the underlying cause of an internal failure (with request
// context and a stack trace) and returns a generic 500 to the client. Use this
// at any handler site where a real error value is in scope, instead of
// writeError(w, http.StatusInternalServerError, ...), so the cause is captured
// in the server logs rather than silently discarded.
func serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("internal server error",
		"method", r.Method,
		"path", r.URL.Path,
		"err", err,
		"stack", string(debug.Stack()),
	)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"error": "internal error",
		"code":  "INTERNAL_ERROR",
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
