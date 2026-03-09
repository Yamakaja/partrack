package handler

import (
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

// numericString formats a pgtype.Numeric as a fixed-point decimal string.
// Returns nil when the value is not valid (NULL).
func numericString(n pgtype.Numeric) *string {
	if !n.Valid {
		return nil
	}
	if n.NaN {
		s := "NaN"
		return &s
	}
	isNeg := n.Int.Sign() < 0
	absInt := new(big.Int).Abs(n.Int)
	intStr := absInt.String()

	var result string
	if n.Exp >= 0 {
		result = intStr + strings.Repeat("0", int(n.Exp))
	} else {
		exp := int(-n.Exp)
		if len(intStr) <= exp {
			result = "0." + strings.Repeat("0", exp-len(intStr)) + intStr
		} else {
			result = intStr[:len(intStr)-exp] + "." + intStr[len(intStr)-exp:]
		}
	}
	if isNeg {
		result = "-" + result
	}
	return &result
}

// timestamptzString formats a pgtype.Timestamptz as an RFC3339 string.
// Returns nil when the value is not valid (NULL).
func timestamptzString(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.UTC().Format(time.RFC3339)
	return &s
}

// parsePaginationParams parses limit and offset from query parameters.
// limit defaults to 50, max 200. offset defaults to 0.
func parsePaginationParams(r *http.Request) (limit, offset int32) {
	limit = 50
	offset = 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	return
}

type partFullResponse struct {
	ID                         string  `json:"id"`
	Scope                      string  `json:"scope"`
	TeamID                     *string `json:"team_id"`
	Mpn                        *string `json:"mpn"`
	Manufacturer               *string `json:"manufacturer"`
	Description                *string `json:"description"`
	ShortDescription           *string `json:"short_description"`
	Category                   *string `json:"category"`
	CategoryID                 *int32  `json:"category_id"`
	CategoryName               *string `json:"category_name"`
	CategorySource             string  `json:"category_source"`
	Package                    *string `json:"package"`
	DatasheetURL               *string `json:"datasheet_url"`
	ProductURL                 *string `json:"product_url"`
	ImageURL                   *string `json:"image_url"`
	LifecycleStatus            string  `json:"lifecycle_status"`
	EnrichmentSourceID         *string `json:"enrichment_source_id"`
	EnrichedAt                 *string `json:"enriched_at"`
	PreferredParameterSourceID *string `json:"preferred_parameter_source_id"`
	CreatedAt                  string  `json:"created_at"`
}

func toPartFullResponse(p dbgen.Part) partFullResponse {
	resp := partFullResponse{
		ID:               p.ID.String(),
		Scope:            string(p.Scope),
		Mpn:              p.Mpn,
		Manufacturer:     p.Manufacturer,
		Description:      p.Description,
		ShortDescription: p.ShortDescription,
		Category:         p.Category,
		CategoryID:       p.CategoryID,
		CategorySource:   p.CategorySource,
		Package:          p.Package,
		DatasheetURL:     p.DatasheetUrl,
		ProductURL:       p.ProductUrl,
		ImageURL:         p.ImageUrl,
		LifecycleStatus:  string(p.LifecycleStatus),
		CreatedAt:        p.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if p.TeamID.Valid {
		s := p.TeamID.String()
		resp.TeamID = &s
	}
	if p.EnrichmentSourceID.Valid {
		s := p.EnrichmentSourceID.String()
		resp.EnrichmentSourceID = &s
	}
	if p.EnrichedAt.Valid {
		s := p.EnrichedAt.Time.UTC().Format(time.RFC3339)
		resp.EnrichedAt = &s
	}
	if p.PreferredParameterSourceID.Valid {
		s := p.PreferredParameterSourceID.String()
		resp.PreferredParameterSourceID = &s
	}
	return resp
}
