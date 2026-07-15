package worker

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/distributor/testclient"
	"github.com/Yamakaja/partrack/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// jsonMap decodes raw JSON bytes into a map for assertion convenience.
func jsonMap(data json.RawMessage) map[string]any {
	var m map[string]any
	json.Unmarshal(data, &m) //nolint:errcheck
	return m
}

var testDB *pgxpool.Pool

func TestMain(m *testing.M) {
	testDB = testutil.MustDB(nil)
	os.Exit(m.Run())
}

// newTestQueue builds a zero-worker Queue so process() can be called synchronously.
func newTestQueue(db *pgxpool.Pool) *Queue {
	reg := distributor.NewRegistry()
	reg.Register("test", testclient.New())
	return New(0, db, reg)
}

// drainEvents reads from ch until a status field matches wantStatus or timeout
// elapses. Returns the matching event data, or nil on timeout.
func drainEvents(ch <-chan Event, wantStatus string, timeout time.Duration) map[string]any {
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-ch:
			d := jsonMap(ev.Data)
			if d["status"] == wantStatus {
				return d
			}
		case <-deadline:
			return nil
		}
	}
}

func TestHandlePartEnrich_LM317(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "enrichtest@example.com", "pw", "Enrich Test")
	teamID := testutil.CreateTeam(t, testDB, userID, "admin", "enrich-team")
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)
	partID := testutil.CreatePart(t, testDB, "global", "LM317T", nil)

	q := newTestQueue(testDB)
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()

	q.process(Job{
		ID:     "j-lm317",
		Type:   JobPartEnrich,
		TeamID: teamID,
		Payload: map[string]string{
			"part_id":        partID,
			"sku":            "LM317-TEST-TUBE",
			"distributor_id": distID,
		},
	})

	if d := drainEvents(ch, "complete", 2*time.Second); d == nil {
		t.Fatal("timed out waiting for status=complete")
	}

	// Assert DB state: 3 parameters, 1 listing, enriched_at set.
	var paramCount int
	if err := testDB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM part_parameters WHERE part_id = $1`, partID,
	).Scan(&paramCount); err != nil {
		t.Fatalf("count parameters: %v", err)
	}
	if paramCount != 3 {
		t.Errorf("expected 3 parameters, got %d", paramCount)
	}

	var listingCount int
	if err := testDB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM distributor_listings WHERE part_id = $1`, partID,
	).Scan(&listingCount); err != nil {
		t.Fatalf("count listings: %v", err)
	}
	if listingCount != 1 {
		t.Errorf("expected 1 listing, got %d", listingCount)
	}

	var enrichedAtNull bool
	if err := testDB.QueryRow(context.Background(),
		`SELECT enriched_at IS NULL FROM parts WHERE id = $1`, partID,
	).Scan(&enrichedAtNull); err != nil {
		t.Fatalf("check enriched_at: %v", err)
	}
	if enrichedAtNull {
		t.Error("expected enriched_at to be set after enrichment")
	}
}

func TestHandlePartEnrich_UnknownSKU(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "bc547@example.com", "pw", "BC547 Test")
	teamID := testutil.CreateTeam(t, testDB, userID, "admin", "bc547-team")
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)
	partID := testutil.CreatePart(t, testDB, "global", "BC547", nil)

	q := newTestQueue(testDB)
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()

	q.process(Job{
		ID:     "j-bc547",
		Type:   JobPartEnrich,
		TeamID: teamID,
		Payload: map[string]string{
			"part_id":        partID,
			"sku":            "BC547-BOGUS-SKU",
			"distributor_id": distID,
		},
	})

	// SKU not known to testclient → not_found event (no complete published).
	if d := drainEvents(ch, "not_found", 2*time.Second); d == nil {
		t.Fatal("expected not_found event for unknown SKU")
	}

	var paramCount, listingCount int
	testDB.QueryRow(context.Background(), `SELECT COUNT(*) FROM part_parameters WHERE part_id = $1`, partID).Scan(&paramCount)        //nolint:errcheck
	testDB.QueryRow(context.Background(), `SELECT COUNT(*) FROM distributor_listings WHERE part_id = $1`, partID).Scan(&listingCount) //nolint:errcheck
	if paramCount != 0 || listingCount != 0 {
		t.Errorf("expected 0 params/listings for unknown MPN, got %d/%d", paramCount, listingCount)
	}
}

func TestHandlePartEnrich_NoMPN(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "nompn@example.com", "pw", "No MPN")
	teamID := testutil.CreateTeam(t, testDB, userID, "admin", "nompn-team")
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)

	// Create part with no MPN.
	var partID string
	if err := testDB.QueryRow(context.Background(),
		`INSERT INTO parts (scope) VALUES ('global') RETURNING id`,
	).Scan(&partID); err != nil {
		t.Fatalf("insert part without mpn: %v", err)
	}

	q := newTestQueue(testDB)
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()

	q.process(Job{
		ID:      "j-nompn",
		Type:    JobPartEnrich,
		TeamID:  teamID,
		Payload: map[string]string{"part_id": partID},
	})

	// Expect an error event (no complete).
	if d := drainEvents(ch, "error", 2*time.Second); d == nil {
		t.Fatal("expected error event for part with no MPN")
	}
}

func TestHandlePartEnrich_InvalidPayload(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "badpayload@example.com", "pw", "Bad Payload")
	teamID := testutil.CreateTeam(t, testDB, userID, "admin", "bad-payload-team")

	q := newTestQueue(testDB)
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()

	// Wrong payload type — not map[string]string.
	q.process(Job{
		ID:      "j-bad",
		Type:    JobPartEnrich,
		TeamID:  teamID,
		Payload: "not-a-map",
	})

	if d := drainEvents(ch, "error", 2*time.Second); d == nil {
		t.Fatal("expected error event for invalid payload")
	}
}

func TestHandleListingRefresh_LM317(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "refresh@example.com", "pw", "Refresh Test")
	teamID := testutil.CreateTeam(t, testDB, userID, "admin", "refresh-team")
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)
	partID := testutil.CreatePart(t, testDB, "global", "LM317T", nil)

	// Insert a listing row directly.
	var listingID string
	if err := testDB.QueryRow(context.Background(),
		`INSERT INTO distributor_listings (part_id, distributor_id, sku, packaging_type, moq)
		 VALUES ($1, $2, 'LM317-TEST-TUBE', 'tube', 1) RETURNING id`,
		partID, distID,
	).Scan(&listingID); err != nil {
		t.Fatalf("insert listing: %v", err)
	}

	q := newTestQueue(testDB)
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()

	q.process(Job{
		ID:      "j-refresh",
		Type:    JobListingRefresh,
		TeamID:  teamID,
		Payload: ListingRefreshPayload{ListingID: listingID},
	})

	if d := drainEvents(ch, "done", 2*time.Second); d == nil {
		t.Fatal("timed out waiting for status=done")
	}

	var stockAvailable *int32
	if err := testDB.QueryRow(context.Background(),
		`SELECT stock_available FROM distributor_listings WHERE id = $1`, listingID,
	).Scan(&stockAvailable); err != nil {
		t.Fatalf("query listing: %v", err)
	}
	if stockAvailable == nil || *stockAvailable != 0 {
		t.Errorf("expected stock_available=0, got %v", stockAvailable)
	}
}

// TestHandleOrderImport_SplitLineItems reproduces a DigiKey order where one part
// ships split across two line items (same SKU, different quantity-break prices —
// a reel ran out mid-pick). Both resolve to the same listing, so importing them
// must collapse into a single order line item: previously the second insert
// violated UNIQUE(order_id, listing_id) and rolled back the whole import.
func TestHandleOrderImport_SplitLineItems(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "orderimport@example.com", "pw", "Order Import")
	teamID := testutil.CreateTeam(t, testDB, userID, "purchaser", "order-import-team")
	distID := testutil.GetTestDistributorID(t, testDB)
	testutil.EnableDistributor(t, testDB, teamID, distID)

	// Pre-create part + listing so the two split lines resolve by direct lookup.
	partID := testutil.CreatePart(t, testDB, "global", "LM317T", nil)
	listingID := testutil.CreateListing(t, testDB, partID, distID, "LM317-TEST-TUBE")

	q := newTestQueue(testDB)
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()

	q.process(Job{
		ID:     "j-order-import",
		Type:   JobOrderImport,
		TeamID: teamID,
		Payload: map[string]string{
			"sales_order_id": testclient.SplitOrderID,
			"distributor_id": distID,
			"user_id":        userID,
		},
	})

	d := drainEvents(ch, "complete", 3*time.Second)
	if d == nil {
		t.Fatal("timed out waiting for status=complete")
	}
	// total/resolved count DigiKey lines (2) pre-merge.
	if d["total"] != float64(2) {
		t.Errorf("expected total=2, got %v", d["total"])
	}
	if d["resolved"] != float64(2) {
		t.Errorf("expected resolved=2, got %v", d["resolved"])
	}

	orderID, _ := d["order_id"].(string)
	if orderID == "" {
		t.Fatalf("missing order_id in complete event: %v", d)
	}

	// The two split lines must collapse into exactly one row for the listing,
	// with quantities summed and the unit price blended (extended cost preserved).
	var lineCount int
	var totalQty, extShipped int32
	var unitPriceText string
	if err := testDB.QueryRow(context.Background(),
		`SELECT count(*), coalesce(sum(quantity_ordered),0), coalesce(sum(external_qty_shipped),0),
		        coalesce(max(unit_price_snapshot::text), '')
		 FROM order_line_items WHERE order_id = $1 AND listing_id = $2`,
		orderID, listingID,
	).Scan(&lineCount, &totalQty, &extShipped, &unitPriceText); err != nil {
		t.Fatalf("query line items: %v", err)
	}
	if lineCount != 1 {
		t.Fatalf("expected 1 merged line item, got %d", lineCount)
	}
	if totalQty != 112 {
		t.Errorf("expected summed quantity_ordered 112, got %d", totalQty)
	}
	if extShipped != 112 {
		t.Errorf("expected summed external_qty_shipped 112, got %d", extShipped)
	}
	// Blended unit price: (100*0.0103 + 12*0.0180) / 112 = 1.246/112 = 0.011125 -> "0.0111".
	blended, err := strconv.ParseFloat(unitPriceText, 64)
	if err != nil {
		t.Fatalf("parse unit price %q: %v", unitPriceText, err)
	}
	if math.Abs(blended-0.0111) > 1e-9 {
		t.Errorf("expected blended unit price 0.0111, got %s", unitPriceText)
	}
}

func TestHandleListingRefresh_NotFound(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })

	userID := testutil.CreateUser(t, testDB, "notfound@example.com", "pw", "Not Found")
	teamID := testutil.CreateTeam(t, testDB, userID, "admin", "notfound-team")

	q := newTestQueue(testDB)
	ch, cancel := q.Subscribe(teamID, "")
	defer cancel()

	q.process(Job{
		ID:      "j-notfound",
		Type:    JobListingRefresh,
		TeamID:  teamID,
		Payload: map[string]string{"listing_id": "00000000-0000-0000-0000-000000000000"},
	})

	if d := drainEvents(ch, "error", 2*time.Second); d == nil {
		t.Fatal("expected error event for nonexistent listing")
	}
}
