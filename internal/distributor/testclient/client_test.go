package testclient

import (
	"context"
	"testing"

	"github.com/Yamakaja/partrack/internal/distributor"
)

func TestSearchParts_LM317(t *testing.T) {
	ctx := context.Background()
	c := New()

	res, err := c.SearchParts(ctx, distributor.SearchQuery{Keywords: "LM317"}, nil)
	if err != nil {
		t.Fatalf("SearchParts error: %v", err)
	}
	if res == nil {
		t.Fatalf("expected result, got nil")
	}
	if res.TotalCount != 1 {
		t.Fatalf("expected TotalCount=1, got %d", res.TotalCount)
	}
	if len(res.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(res.Products))
	}
	if res.Products[0].PrimarySKU != testSKU {
		t.Fatalf("expected PrimarySKU=%s, got %s", testSKU, res.Products[0].PrimarySKU)
	}
}

func TestSearchParts_RegulatorPagination(t *testing.T) {
	ctx := context.Background()
	c := New()

	// First page
	res1, err := c.SearchParts(ctx, distributor.SearchQuery{Keywords: "regulator", Limit: 2, Offset: 0}, nil)
	if err != nil {
		t.Fatalf("SearchParts error: %v", err)
	}
	if res1 == nil || len(res1.Products) != 2 {
		t.Fatalf("expected 2 products on first page, got %+v", res1)
	}

	// Second page
	res2, err := c.SearchParts(ctx, distributor.SearchQuery{Keywords: "regulator", Limit: 2, Offset: 2}, nil)
	if err != nil {
		t.Fatalf("SearchParts error: %v", err)
	}
	if res2 == nil || len(res2.Products) != 1 {
		t.Fatalf("expected 1 product on second page, got %+v", res2)
	}
}

func TestSearchParts_NoResults(t *testing.T) {
	ctx := context.Background()
	c := New()

	res, err := c.SearchParts(ctx, distributor.SearchQuery{Keywords: "no-such-part"}, nil)
	if err != nil {
		t.Fatalf("SearchParts error: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result for no-results case")
	}
	if res.TotalCount != 0 || len(res.Products) != 0 {
		t.Fatalf("expected zero results, got %+v", res)
	}
}
