package handler

import (
	"context"

	"github.com/Yamakaja/partrack/internal/activity"
	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

// Resolvers for the snapshot labels carried on activity-log subjects, so the
// feed reads "Cancelled DigiKey #PO-1042" rather than "Cancelled the order".
// Formatting lives in internal/activity so every call site agrees on how an
// entity is named.
//
// Each is a single indexed read on the write path, run after the operation's
// transaction has committed. A lookup failure yields "" rather than an error:
// logging must never fail the thing it logs, and the frontend degrades to its
// generic wording.

func (h *Handler) partLabel(ctx context.Context, partID pgtype.UUID) string {
	row, err := h.Queries.GetPartLabelFields(ctx, partID)
	if err != nil {
		return ""
	}
	return activity.PartLabel(row.Mpn, row.ShortDescription)
}

func (h *Handler) orderLabel(ctx context.Context, orderID, teamID pgtype.UUID) string {
	row, err := h.Queries.GetOrderLabelFields(ctx, dbgen.GetOrderLabelFieldsParams{
		ID:     orderID,
		TeamID: teamID,
	})
	if err != nil {
		return ""
	}
	return activity.OrderLabel(row.DistributorName, row.PurchaseOrderNumber, row.ExternalReference, orderID.String())
}

func (h *Handler) buildLabel(ctx context.Context, buildID, teamID pgtype.UUID) string {
	row, err := h.Queries.GetBuildLabelFields(ctx, dbgen.GetBuildLabelFieldsParams{
		ID:     buildID,
		TeamID: teamID,
	})
	if err != nil {
		return ""
	}
	return activity.RevisionLabel(row.ProjectName, row.BomRevisionLabel)
}
