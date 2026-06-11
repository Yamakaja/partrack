package handler

import (
	"encoding/json"
	"net/http"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type dashboardStats struct {
	ProjectCount               int64 `json:"project_count"`
	ActiveProjectCount         int64 `json:"active_project_count"`
	TrackedPartCount           int64 `json:"tracked_part_count"`
	InStockPartCount           int64 `json:"in_stock_part_count"`
	TotalQuantityOnHand        int64 `json:"total_quantity_on_hand"`
	CartItemCount              int64 `json:"cart_item_count"`
	OutstandingOrderCount      int64 `json:"outstanding_order_count"`
	InventoryBelowRestockCount int64 `json:"inventory_below_restock_count"`
}

type dashboardRecentProject struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Status              string  `json:"status"`
	ActiveRevisionLabel *string `json:"active_revision_label"`
	ActiveRevisionID    *string `json:"active_revision_id"`
}

type dashboardInventoryAlert struct {
	PartID           string  `json:"part_id"`
	MPN              *string `json:"mpn"`
	Description      *string `json:"description"`
	QuantityOnHand   int32   `json:"quantity_on_hand"`
	MinStockLevel    int32   `json:"min_stock_level"`
	InventoryEntryID string  `json:"inventory_entry_id"`
}

type dashboardOrder struct {
	ID                  string  `json:"id"`
	DistributorName     string  `json:"distributor_name"`
	PurchaseOrderNumber *string `json:"purchase_order_number"`
	ExternalReference   *string `json:"external_reference"`
	CreatedAt           string  `json:"created_at"`
	ItemCount           int64   `json:"item_count"`
	Status              string  `json:"status"`
}

type dashboardActivitySubject struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// dashboardActivityItem mirrors the ListActivity item shape so the dashboard
// widget and the side panel render through the same frontend component.
type dashboardActivityItem struct {
	ID                      string                    `json:"id"`
	Action                  string                    `json:"action"`
	Feed                    string                    `json:"feed"`
	Source                  string                    `json:"source"`
	ActorLabel              *string                   `json:"actor_label"`
	Subject                 *dashboardActivitySubject `json:"subject"`
	Summary                 *string                   `json:"summary"`
	Payload                 json.RawMessage           `json:"payload,omitempty"`
	CreatedAt               string                    `json:"created_at"`
	PartManufacturer        *string                   `json:"part_manufacturer"`
	PartImageURL            *string                   `json:"part_image_url"`
	InventoryEntryID        *string                   `json:"inventory_entry_id"`
	InventoryQuantityOnHand *int32                    `json:"inventory_quantity_on_hand"`
}

type dashboardResponse struct {
	Stats             dashboardStats             `json:"stats"`
	RecentProjects    []dashboardRecentProject   `json:"recent_projects"`
	InventoryAlerts   []dashboardInventoryAlert  `json:"inventory_alerts"`
	OutstandingOrders []dashboardOrder           `json:"outstanding_orders"`
	RecentActivity    []dashboardActivityItem    `json:"recent_activity"`
	BuildPipeline     map[string][]pipelineBuild `json:"build_pipeline"`
}

// GetDashboard handles GET /api/v1/teams/{teamId}/dashboard
// Returns aggregated stats and recent activity for the team dashboard.
func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	var teamID pgtype.UUID
	if err := teamID.Scan(chi.URLParam(r, "teamId")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid team ID", "BAD_REQUEST")
		return
	}

	ctx := r.Context()

	stats, err := h.Queries.GetDashboardStats(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	recentProjectRows, err := h.Queries.GetDashboardRecentProjects(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	alertRows, err := h.Queries.GetDashboardInventoryAlerts(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	orderRows, err := h.Queries.GetDashboardOutstandingOrders(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	activityRows, err := h.Queries.RecentTeamActivity(ctx, dbgen.RecentTeamActivityParams{
		TeamID: teamID,
		Limit:  10,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	pipelineRows, err := h.Queries.GetBuildPipeline(ctx, teamID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	// Assemble recent projects.
	recentProjects := make([]dashboardRecentProject, len(recentProjectRows))
	for i, p := range recentProjectRows {
		recentProjects[i] = dashboardRecentProject{
			ID:                  p.ID.String(),
			Name:                p.Name,
			Status:              string(p.Status),
			ActiveRevisionLabel: p.ActiveRevisionLabel,
			ActiveRevisionID:    uuidPtrString(p.ActiveRevisionID),
		}
	}

	// Assemble inventory alerts.
	alerts := make([]dashboardInventoryAlert, len(alertRows))
	for i, a := range alertRows {
		var minStock int32
		if a.MinimumStockQuantity != nil {
			minStock = *a.MinimumStockQuantity
		}
		alerts[i] = dashboardInventoryAlert{
			PartID:           a.PartID.String(),
			MPN:              a.Mpn,
			Description:      a.Description,
			QuantityOnHand:   a.QuantityOnHand,
			MinStockLevel:    minStock,
			InventoryEntryID: a.InventoryEntryID.String(),
		}
	}

	// Assemble outstanding orders.
	orders := make([]dashboardOrder, len(orderRows))
	for i, o := range orderRows {
		orders[i] = dashboardOrder{
			ID:                  o.ID.String(),
			DistributorName:     o.DistributorName,
			PurchaseOrderNumber: o.PurchaseOrderNumber,
			ExternalReference:   o.ExternalReference,
			CreatedAt:           o.CreatedAt.Time.UTC().Format(time.RFC3339),
			ItemCount:           o.ItemCount,
			Status:              string(o.Status),
		}
	}

	// Assemble build pipeline.
	pipelineStages := map[string][]pipelineBuild{
		"draft": {}, "pending": {}, "ordered": {}, "ready": {},
	}
	for _, row := range pipelineRows {
		if _, ok := pipelineStages[row.Status]; !ok {
			continue
		}
		pipelineStages[row.Status] = append(pipelineStages[row.Status], pipelineBuild{
			ID:                   row.ID.String(),
			ProjectID:            row.ProjectID.String(),
			ProjectName:          row.ProjectName,
			BomRevisionID:        row.BomRevisionID.String(),
			BomRevisionLabel:     row.BomRevisionLabel,
			Quantity:             row.Quantity,
			Status:               row.Status,
			CreatedAt:            row.CreatedAt.Time.UTC().Format(time.RFC3339),
			Notes:                row.Notes,
			CreatedBy:            row.CreatedBy.String(),
			CreatedByDisplayName: row.CreatedByDisplayName,
			ExcludedFromCart:     row.ExcludedFromCart,
		})
	}

	// Assemble recent activity (team-wide feed).
	activity := make([]dashboardActivityItem, 0, len(activityRows))
	for _, a := range activityRows {
		item := dashboardActivityItem{
			ID:         a.ID.String(),
			Action:     a.Action,
			Feed:       a.Feed,
			Source:     a.Source,
			ActorLabel: a.ActorLabel,
			Summary:    a.Summary,
			Payload:    a.Payload,
			CreatedAt:  a.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
		}
		if a.SubjectID.Valid && a.SubjectType != nil {
			item.Subject = &dashboardActivitySubject{
				Type:  *a.SubjectType,
				ID:    a.SubjectID.String(),
				Label: derefStr(a.SubjectLabel),
			}
		}
		activity = append(activity, item)
	}

	writeJSON(w, http.StatusOK, dashboardResponse{
		Stats: dashboardStats{
			ProjectCount:               stats.ProjectCount,
			ActiveProjectCount:         stats.ActiveProjectCount,
			TrackedPartCount:           stats.TrackedPartCount,
			InStockPartCount:           stats.InStockPartCount,
			TotalQuantityOnHand:        stats.TotalQuantityOnHand,
			CartItemCount:              stats.CartItemCount,
			OutstandingOrderCount:      stats.OutstandingOrderCount,
			InventoryBelowRestockCount: stats.InventoryBelowRestockCount,
		},
		RecentProjects:    recentProjects,
		InventoryAlerts:   alerts,
		OutstandingOrders: orders,
		RecentActivity:    activity,
		BuildPipeline:     pipelineStages,
	})
}
