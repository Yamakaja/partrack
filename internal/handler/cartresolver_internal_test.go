package handler

import (
	"math/big"
	"testing"
	"time"

	dbgen "github.com/Yamakaja/partrack/internal/db/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

func num(s string) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(s)
	return n
}

func i32p(v int32) *int32 { return &v }

// listingOpt configures a test listing row.
type listingOpt struct {
	name      string
	unitPrice string
	moq       int32
	priority  *int32
	preferred bool
	stock     *int32
	// ageSeconds is how long ago the listing was refreshed; 0 = now (fresh).
	ageSeconds int
	cacheSecs  int32
}

func makeRow(now time.Time, o listingOpt) dbgen.GetListingsForCartResolutionRow {
	cache := o.cacheSecs
	if cache == 0 {
		cache = 3600
	}
	return dbgen.GetListingsForCartResolutionRow{
		DistributorName:      o.name,
		Sku:                  o.name + "-SKU",
		UnitPrice:            num(o.unitPrice),
		Moq:                  o.moq,
		Priority:             o.priority,
		Preferred:            o.preferred,
		StockAvailable:       o.stock,
		LastRefreshedAt:      pgtype.Timestamptz{Time: now.Add(-time.Duration(o.ageSeconds) * time.Second), Valid: true},
		CacheDurationSeconds: cache,
	}
}

func TestPickBestListing_CostStrategyPicksCheapest(t *testing.T) {
	now := time.Now()
	rows := []dbgen.GetListingsForCartResolutionRow{
		makeRow(now, listingOpt{name: "DigiKey", unitPrice: "2.00", moq: 1, stock: i32p(100)}),
		makeRow(now, listingOpt{name: "Mouser", unitPrice: "1.00", moq: 1, stock: i32p(100)}),
	}
	got := pickBestListing(rows, 10, sourcingPolicy{strategy: "cost"}, now)
	if got == nil || got.DistributorName != "Mouser" {
		t.Fatalf("expected Mouser (cheapest), got %+v", got)
	}
	if got.Reason != "cost" {
		t.Errorf("expected reason 'cost', got %q", got.Reason)
	}
}

func TestPickBestListing_PreferredInStockHonorsRankOverCost(t *testing.T) {
	now := time.Now()
	rows := []dbgen.GetListingsForCartResolutionRow{
		makeRow(now, listingOpt{name: "DigiKey", unitPrice: "2.00", moq: 1, priority: i32p(1), preferred: true, stock: i32p(100)}),
		makeRow(now, listingOpt{name: "Mouser", unitPrice: "1.00", moq: 1, priority: i32p(2), stock: i32p(100)}),
	}
	got := pickBestListing(rows, 10, sourcingPolicy{strategy: "preferred_in_stock"}, now)
	if got == nil || got.DistributorName != "DigiKey" {
		t.Fatalf("expected DigiKey (top rank, in stock), got %+v", got)
	}
	if got.Reason != "preferred" {
		t.Errorf("expected reason 'preferred', got %q", got.Reason)
	}
	if got.PreferredStale {
		t.Errorf("expected fresh stock, PreferredStale should be false")
	}
}

func TestPickBestListing_FallsThroughRankWhenOutOfStock(t *testing.T) {
	now := time.Now()
	rows := []dbgen.GetListingsForCartResolutionRow{
		// DigiKey ranked #1 but fresh data shows only 3 in stock for a demand of 10.
		makeRow(now, listingOpt{name: "DigiKey", unitPrice: "1.00", moq: 1, priority: i32p(1), preferred: true, stock: i32p(3)}),
		makeRow(now, listingOpt{name: "Mouser", unitPrice: "2.00", moq: 1, priority: i32p(2), stock: i32p(100)}),
	}
	got := pickBestListing(rows, 10, sourcingPolicy{strategy: "preferred_in_stock"}, now)
	if got == nil || got.DistributorName != "Mouser" {
		t.Fatalf("expected Mouser (rank #1 out of stock), got %+v", got)
	}
	if got.Reason != "preferred_fallback" {
		t.Errorf("expected reason 'preferred_fallback', got %q", got.Reason)
	}
}

func TestPickBestListing_OutOfStockFallbackToCheapest(t *testing.T) {
	now := time.Now()
	rows := []dbgen.GetListingsForCartResolutionRow{
		// Only ranked distributor, out of stock.
		makeRow(now, listingOpt{name: "DigiKey", unitPrice: "5.00", moq: 1, priority: i32p(1), preferred: true, stock: i32p(0)}),
		// Unranked, in stock, cheapest.
		makeRow(now, listingOpt{name: "LCSC", unitPrice: "1.00", moq: 1, stock: i32p(100)}),
	}
	got := pickBestListing(rows, 10, sourcingPolicy{strategy: "preferred_in_stock"}, now)
	if got == nil || got.DistributorName != "LCSC" {
		t.Fatalf("expected LCSC (cost fallback), got %+v", got)
	}
	if got.Reason != "out_of_stock_fallback" {
		t.Errorf("expected reason 'out_of_stock_fallback', got %q", got.Reason)
	}
}

func TestPickBestListing_CostGuardrailVetoesPreference(t *testing.T) {
	now := time.Now()
	rows := []dbgen.GetListingsForCartResolutionRow{
		makeRow(now, listingOpt{name: "DigiKey", unitPrice: "2.00", moq: 1, priority: i32p(1), preferred: true, stock: i32p(100)}),
		makeRow(now, listingOpt{name: "Mouser", unitPrice: "1.00", moq: 1, priority: i32p(2), stock: i32p(100)}),
	}
	// 15% tolerance: DigiKey total 20.00 vs Mouser 10.00 is far over → veto.
	policy := sourcingPolicy{strategy: "preferred_in_stock", tolerancePct: big.NewFloat(15)}
	got := pickBestListing(rows, 10, policy, now)
	if got == nil || got.DistributorName != "Mouser" {
		t.Fatalf("expected Mouser (preference over tolerance), got %+v", got)
	}
	if got.Reason != "over_tolerance" {
		t.Errorf("expected reason 'over_tolerance', got %q", got.Reason)
	}
}

func TestPickBestListing_ToleranceAllowsSmallPremium(t *testing.T) {
	now := time.Now()
	rows := []dbgen.GetListingsForCartResolutionRow{
		makeRow(now, listingOpt{name: "DigiKey", unitPrice: "1.10", moq: 1, priority: i32p(1), preferred: true, stock: i32p(100)}),
		makeRow(now, listingOpt{name: "Mouser", unitPrice: "1.00", moq: 1, priority: i32p(2), stock: i32p(100)}),
	}
	// 15% tolerance: DigiKey 11.00 vs Mouser 10.00 = 10% premium → honoured.
	policy := sourcingPolicy{strategy: "preferred_in_stock", tolerancePct: big.NewFloat(15)}
	got := pickBestListing(rows, 10, policy, now)
	if got == nil || got.DistributorName != "DigiKey" {
		t.Fatalf("expected DigiKey (within tolerance), got %+v", got)
	}
	if got.Reason != "preferred" {
		t.Errorf("expected reason 'preferred', got %q", got.Reason)
	}
}

func TestPickBestListing_StaleStockHonoredButFlagged(t *testing.T) {
	now := time.Now()
	rows := []dbgen.GetListingsForCartResolutionRow{
		// Ranked #1, but stock data is 2 days old with a 1h cache window → stale.
		makeRow(now, listingOpt{name: "DigiKey", unitPrice: "2.00", moq: 1, priority: i32p(1), preferred: true, stock: i32p(0), ageSeconds: 172800, cacheSecs: 3600}),
		makeRow(now, listingOpt{name: "Mouser", unitPrice: "1.00", moq: 1, priority: i32p(2), stock: i32p(100)}),
	}
	got := pickBestListing(rows, 10, sourcingPolicy{strategy: "preferred_in_stock"}, now)
	if got == nil || got.DistributorName != "DigiKey" {
		t.Fatalf("expected DigiKey (stale stock honoured optimistically), got %+v", got)
	}
	if !got.PreferredStale {
		t.Errorf("expected PreferredStale=true for stale data")
	}
	if got.Reason != "preferred" {
		t.Errorf("expected reason 'preferred', got %q", got.Reason)
	}
}
