package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Yamakaja/partrack/internal/apitypes"
)

// These tests drive the workflow commands — projects, BOM revisions, orders,
// builds, the cart and checkouts — against a stub server, so the query strings
// pt sends and the shapes it decodes are both asserted rather than assumed.

const (
	testProjectID  = "aaaaaaaa-0000-0000-0000-000000000001"
	testRevisionID = "bbbbbbbb-0000-0000-0000-000000000002"
	testOrderID    = "cccccccc-0000-0000-0000-000000000003"
	testBuildID    = "dddddddd-0000-0000-0000-000000000004"
	testCheckoutID = "eeeeeeee-0000-0000-0000-000000000005"
	testPartID     = "11111111-1111-1111-1111-111111111111"
	teamPath       = "/api/v1/teams/team-uuid-1"
)

// registerWorkflowStubs adds the workflow endpoints to a stub server's mux. It
// is called by newWorkflowServer rather than by newStubServer so the part-only
// tests keep their smaller surface.
func registerWorkflowStubs(t *testing.T, s *stubServer, mux *http.ServeMux) {
	t.Helper()
	write := func(w http.ResponseWriter, r *http.Request, v any) {
		s.record(r)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encoding stub response: %v", err)
		}
	}

	mux.HandleFunc(teamPath+"/projects", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, apitypes.List[ProjectListItem]{
			Total: 2, Limit: 50, Offset: 0,
			Items: []ProjectListItem{
				{
					ID: testProjectID, Name: "Power Supply v2", Status: "active",
					ActiveBomRevisionID: sp(testRevisionID), ActiveBomRevisionLabel: sp("rev-3"),
					CreatedByDisplayName: "Dana", CreatedAt: "2026-08-01T09:00:00Z",
					ProjectManagerDisplayName: sp("Dana"),
				},
				{
					ID: "aaaaaaaa-0000-0000-0000-000000000009", Name: "Sensor Board",
					Status: "archived", CreatedByDisplayName: "Dana",
					CreatedAt: "2026-07-01T09:00:00Z",
				},
			},
		})
	})
	mux.HandleFunc(teamPath+"/projects/"+testProjectID, func(w http.ResponseWriter, r *http.Request) {
		write(w, r, ProjectDetail{
			ID: testProjectID, Name: "Power Supply v2", Status: "active",
			ActiveBomRevisionID: sp(testRevisionID), ActiveBomRevisionLabel: sp("rev-3"),
			CreatedByDisplayName: "Dana", CreatedAt: "2026-08-01T09:00:00Z",
			ActiveRevisionSummary: &apitypes.RevisionSummary{
				ItemCount: 12, ResolvedCount: 10, UnresolvedCount: 1, AmbiguousCount: 1,
			},
		})
	})
	mux.HandleFunc(teamPath+"/projects/"+testProjectID+"/revisions", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, apitypes.RevisionList{Items: []RevisionListItem{
			{
				ID: testRevisionID, Label: "rev-3", ImportedAt: "2026-08-20T10:00:00Z",
				ImportedByDisplayName: "Dana", ItemCount: 12, ResolvedCount: 10,
				UnresolvedCount: 2, IsActive: true,
			},
			{
				ID: "bbbbbbbb-0000-0000-0000-000000000008", Label: "rev-2",
				ImportedAt: "2026-07-20T10:00:00Z", ImportedByDisplayName: "Dana",
				ItemCount: 11, ResolvedCount: 11,
			},
		}})
	})
	mux.HandleFunc(teamPath+"/projects/"+testProjectID+"/note", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, Note{Body: "Do not fit R7 on the pre-production run."})
	})
	mux.HandleFunc(teamPath+"/projects/"+testProjectID+"/revisions/"+testRevisionID,
		func(w http.ResponseWriter, r *http.Request) {
			qty := int32(4200)
			write(w, r, RevisionDetail{
				ID: testRevisionID, Label: "rev-3", ImportedAt: "2026-08-20T10:00:00Z",
				ImportedByDisplayName: "Dana", IsActive: true,
				Items: []BOMLineItem{
					{
						ID: "line-1", ReferenceDesignators: "R1,R2", Quantity: 2,
						Value: sp("10k"), ResolvedPartID: sp(testPartID),
						ResolvedPartMpn: sp("RC0402FR-0710KL"), ResolvedPartMfr: sp("Yageo"),
						QuantityOnHand: &qty, ResolutionState: sp("auto_mpn"),
						Substitutes: []apitypes.BOMSubstituteEntry{},
					},
					{
						ID: "line-2", ReferenceDesignators: "U3", Quantity: 1,
						RawDescription:  sp("Buck regulator, 3A"),
						ResolutionState: sp("ambiguous"),
						Substitutes:     []apitypes.BOMSubstituteEntry{},
					},
					{
						ID: "line-3", ReferenceDesignators: "C9", Quantity: 1,
						Value: sp("100n"), Substitutes: []apitypes.BOMSubstituteEntry{},
					},
				},
			})
		})
	mux.HandleFunc(teamPath+"/projects/"+testProjectID+"/revisions/"+testRevisionID+"/stock-check",
		func(w http.ResponseWriter, r *http.Request) {
			write(w, r, StockCheck{
				RevisionID: testRevisionID, BuildQuantity: 10, FullyBuildable: 4,
				Items: []StockCheckItem{
					{
						BomLineItemID: "line-1", ReferenceDesignators: "R1,R2",
						QuantityPerBoard: 2, RequiredQuantity: 20, QuantityOnHand: 4200,
						QuantityFree: 4200, StockStatus: "in_stock",
						ResolvedPartID: sp(testPartID), ResolvedPartMpn: sp("RC0402FR-0710KL"),
					},
					{
						BomLineItemID: "line-2", ReferenceDesignators: "U3",
						QuantityPerBoard: 1, RequiredQuantity: 10, QuantityOnHand: 3,
						QuantityFree: 3, Shortfall: 7, StockStatus: "short",
						ResolvedPartMpn: sp("TPS54331"),
					},
				},
			})
		})

	mux.HandleFunc(teamPath+"/orders", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, apitypes.List[OrderListItem]{
			Total: 1, Limit: 50, Offset: 0,
			Items: []OrderListItem{{
				ID: testOrderID, DistributorID: "dist-1", DistributorName: "DigiKey",
				Status: "submitted", ExternalReference: sp("WEB-4471"),
				CreatedAt: "2026-08-10T12:00:00Z", CreatedByDisplayName: "Dana",
				LineItemCount: 2, TotalPrice: sp("142.50"),
			}},
		})
	})
	mux.HandleFunc(teamPath+"/orders/"+testOrderID, func(w http.ResponseWriter, r *http.Request) {
		write(w, r, OrderDetail{
			Order: apitypes.Order{
				ID: testOrderID, DistributorID: "dist-1", DistributorName: "DigiKey",
				Status: "submitted", ExternalReference: sp("WEB-4471"),
				CreatedAt: "2026-08-10T12:00:00Z", CreatedByDisplayName: "Dana",
				Integrations: json.RawMessage("{}"),
			},
			LineItemCount: 1,
			Items: []OrderLineItem{{
				ID: "oli-1", ListingID: "listing-1", PartID: testPartID,
				PartMpn: sp("RC0402FR-0710KL"), PartManufacturer: sp("Yageo"),
				Sku: "311-10.0KLRCT-ND", PackagingType: "cut_tape",
				QuantityOrdered: 1000, QuantityReceived: 0, UnitPriceSnapshot: sp("0.0123"),
			}},
		})
	})
	mux.HandleFunc(teamPath+"/orders/"+testOrderID+"/linked-builds", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, []LinkedBuild{{
			ID: testBuildID, ProjectID: testProjectID, ProjectName: "Power Supply v2",
			BomRevisionID: testRevisionID, BomRevisionLabel: "rev-3", Quantity: 10,
			Status: "pending", CreatedAt: "2026-08-05T08:00:00Z",
			TotalLineItems: 12, FulfilledLineItems: 9,
		}})
	})

	mux.HandleFunc(teamPath+"/builds", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, apitypes.List[BuildListItem]{
			Total: 1, Limit: 50, Offset: 0,
			Items: []BuildListItem{{
				ID: testBuildID, ProjectID: testProjectID, ProjectName: "Power Supply v2",
				BomRevisionID: testRevisionID, BomRevisionLabel: "rev-3", Quantity: 10,
				Status: "pending", CreatedAt: "2026-08-05T08:00:00Z",
				CreatedBy: "user-1", CreatedByDisplayName: sp("Dana"),
			}},
		})
	})
	mux.HandleFunc(teamPath+"/builds/pipeline", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, map[string][]PipelineBuild{
			"draft":   {},
			"pending": {{ID: testBuildID, ProjectName: "Power Supply v2", BomRevisionLabel: "rev-3", Quantity: 10, Status: "pending", CreatedAt: "2026-08-05T08:00:00Z"}},
			"ordered": {},
			"ready":   {},
		})
	})
	mux.HandleFunc(teamPath+"/builds/"+testBuildID, func(w http.ResponseWriter, r *http.Request) {
		write(w, r, BuildDetail{
			ID: testBuildID, ProjectID: testProjectID, ProjectName: "Power Supply v2",
			BomRevisionID: testRevisionID, BomRevisionLabel: "rev-3", Quantity: 10,
			Status: "pending", CreatedAt: "2026-08-05T08:00:00Z",
			CreatedByDisplayName: sp("Dana"),
			Allocations: []apitypes.BuildAllocation{{
				ID: "alloc-1", PartID: testPartID, PartMpn: sp("RC0402FR-0710KL"),
				PartManufacturer: sp("Yageo"), QuantityNeeded: 20, QuantityReserved: 20,
			}},
			OrderIDs:             []string{testOrderID},
			SubstituteSelections: []apitypes.BuildSubstituteSelection{},
			LineItemOverrides:    []apitypes.BuildLineItemOverride{},
		})
	})
	mux.HandleFunc(teamPath+"/builds/"+testBuildID+"/preview", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, apitypes.BuildPreview{Items: []BuildPreviewItem{{
			BomLineItemID: "line-1", ReferenceDesignators: "R1,R2",
			ResolvedPartMpn:  sp("RC0402FR-0710KL"),
			QuantityPerBoard: 2, EffectiveQuantityPerBoard: 2,
			TotalQuantity: 20, EffectiveTotalQuantity: 20,
			QuantityOnHand: 4200, Sufficient: true,
			Substitutes: []apitypes.BuildPreviewSubstitute{},
		}}})
	})

	mux.HandleFunc(teamPath+"/cart", func(w http.ResponseWriter, r *http.Request) {
		stock := int32(150000)
		write(w, r, apitypes.Cart{Items: []CartItem{
			{
				ID: "cart-1", PartID: testPartID, PartMpn: sp("RC0402FR-0710KL"),
				PartManufacturer: sp("Yageo"), Quantity: 500, Source: "bom_shortfall",
				Listing: &apitypes.CartListing{
					ListingID: "listing-1", DistributorName: sp("DigiKey"),
					SKU: sp("311-10.0KLRCT-ND"), UnitPrice: sp("0.0123"), MOQ: 1,
					StockAvailable: &stock, OptimizedQty: 1000, Chosen: false,
					Reason: "cheapest_in_stock",
				},
				BuildDemands: []apitypes.CartBuildDemand{{
					BuildID: testBuildID, ProjectName: "Power Supply v2",
					BomRevisionLabel: "rev-3", QuantityNeeded: 500,
				}},
			},
			{
				ID: "cart-2", PartID: "22222222-2222-2222-2222-222222222222",
				PartMpn: sp("OBSCURE-PART"), Quantity: 3, Source: "manual",
				BuildDemands: []apitypes.CartBuildDemand{},
			},
		}})
	})

	mux.HandleFunc(teamPath+"/checkouts", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, apitypes.List[CheckoutListItem]{
			Total: 1, Limit: 50, Offset: 0,
			Items: []CheckoutListItem{{
				ID: testCheckoutID, PerformedAt: "2026-08-22T14:30:00Z",
				Reason: sp("bench repair"), PerformedByDisplayName: "Dana", ItemCount: 2,
			}},
		})
	})
	mux.HandleFunc(teamPath+"/checkouts/"+testCheckoutID, func(w http.ResponseWriter, r *http.Request) {
		write(w, r, CheckoutDetail{
			ID: testCheckoutID, PerformedAt: "2026-08-22T14:30:00Z",
			Reason: sp("bench repair"), PerformedByDisplayName: "Dana",
			Items: []apitypes.CheckoutLineItem{
				{PartID: testPartID, PartMpn: sp("RC0402FR-0710KL"), Quantity: 10,
					StockAdjustmentID: sp("adj-1111-2222-3333")},
			},
		})
	})

	mux.HandleFunc("/api/v1/distributors", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, apitypes.List[distributorRef]{
			Total: 2, Limit: 2,
			Items: []distributorRef{{ID: "dist-1", Name: "DigiKey"}, {ID: "dist-2", Name: "Mouser"}},
		})
	})
}

// newWorkflowServer is newStubServer plus the workflow endpoints.
func newWorkflowServer(t *testing.T) *stubServer {
	t.Helper()
	s := newStubServer(t)
	registerWorkflowStubs(t, s, s.mux)
	return s
}

// setupWorkflowApp points the package globals at the stub and resets the
// workflow commands' flags, which cobra binds as globals.
func setupWorkflowApp(t *testing.T, srv *stubServer) *bytes.Buffer {
	t.Helper()
	out := setupApp(t, srv)
	projectListFlags = projectsListFlags{limit: 50}
	orderListFlags = ordersListFlags{limit: 50}
	orderShowFlags = ordersShowFlags{}
	buildListFlags = buildsListFlags{limit: 50}
	buildShowFlags = buildsShowFlags{}
	cartFlags = cartShowFlags{}
	checkoutListFlags = checkoutsListFlags{limit: 50}
	bomShowF = bomShowFlags{}
	bomStockF = bomStockFlags{}
	return out
}

func TestProjectsListAndShow(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	projectListFlags.status = "active"
	if err := runProjectsList(context.Background()); err != nil {
		t.Fatalf("runProjectsList: %v", err)
	}
	if got := srv.queryFor(t, teamPath[len("/api/v1"):]+"/projects").Get("status"); got != "active" {
		t.Errorf("status filter = %q, want active", got)
	}
	for _, want := range []string{"Power Supply v2", "rev-3", "2 projects"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("listing is missing %q:\n%s", want, out)
		}
	}

	// @1 now addresses a project, which is what the detail view resolves.
	out.Reset()
	if err := runProjectsShow(context.Background(), "@1"); err != nil {
		t.Fatalf("runProjectsShow: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"Power Supply v2", "ACTIVE REVISION", "REVISIONS", "rev-2", "active",
		"Do not fit R7", // the project note rides along with the dossier
	} {
		if !strings.Contains(text, want) {
			t.Errorf("dossier is missing %q:\n%s", want, text)
		}
	}
	// The summary must name the fix, not just the count.
	if !strings.Contains(text, "pt bom show") {
		t.Errorf("unresolved lines should point at the command that lists them:\n%s", text)
	}
}

// A project reference resolves by name, and an unresolved BOM filter must not
// silently drop the lines a fuzzy MPN match produced — those still want a human.
func TestBOMShowUnresolvedFilter(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	bomShowF.unresolved = true
	if err := runBOMShow(context.Background(), "Power Supply"); err != nil {
		t.Fatalf("runBOMShow: %v", err)
	}
	text := out.String()
	if strings.Contains(text, "RC0402FR-0710KL") {
		t.Errorf("--unresolved kept a resolved line:\n%s", text)
	}
	for _, want := range []string{"U3", "ambiguous", "C9", "unresolved", "2 of 3 lines"} {
		if !strings.Contains(text, want) {
			t.Errorf("output is missing %q:\n%s", want, text)
		}
	}
}

// The stock check is meaningless without a build quantity, so the flag is
// validated here rather than surfacing as a 400 from the server.
func TestBOMStockRequiresAQuantity(t *testing.T) {
	srv := newWorkflowServer(t)
	setupWorkflowApp(t, srv)

	err := runBOMStock(context.Background(), "Power Supply v2")
	if err == nil {
		t.Fatal("expected an error with no --quantity")
	}
	if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
		t.Errorf("missing --quantity should be a usage error, got %v", err)
	}
	if len(srv.queries) > 0 {
		if _, hit := srv.queries[teamPath+"/projects/"+testProjectID+"/revisions/"+testRevisionID+"/stock-check"]; hit {
			t.Error("the stock-check endpoint was called despite the missing quantity")
		}
	}
}

func TestBOMStockSendsBuildQuantityAndReportsShortfall(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	bomStockF.quantity = 10
	if err := runBOMStock(context.Background(), "Power Supply v2"); err != nil {
		t.Fatalf("runBOMStock: %v", err)
	}
	path := teamPath[len("/api/v1"):] + "/projects/" + testProjectID + "/revisions/" + testRevisionID + "/stock-check"
	if got := srv.queryFor(t, path).Get("build_quantity"); got != "10" {
		t.Errorf("build_quantity = %q, want 10", got)
	}
	text := out.String()
	for _, want := range []string{"×10 boards", "Fully buildable", "4 boards", "TPS54331", "7"} {
		if !strings.Contains(text, want) {
			t.Errorf("stock check is missing %q:\n%s", want, text)
		}
	}
}

// `--status open` is not a server-side value: it expands to every status that is
// not yet terminal, which costs one request each.
func TestOrdersListExpandsOpenStatus(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	orderListFlags.status = []string{"open"}
	orderListFlags.distributor = "DigiKey"
	if err := runOrdersList(context.Background()); err != nil {
		t.Fatalf("runOrdersList: %v", err)
	}
	q := srv.queryFor(t, teamPath[len("/api/v1"):]+"/orders")
	if got := q.Get("distributor_id"); got != "dist-1" {
		t.Errorf("distributor_id = %q, want the resolved id dist-1", got)
	}
	if got := q.Get("status"); got != "shipped" {
		// The stub records the last request, and `open` ends on shipped.
		t.Errorf("status = %q, want the last of the expanded set", got)
	}
	if !strings.Contains(out.String(), "WEB-4471") {
		t.Errorf("listing is missing the order reference:\n%s", out)
	}

	// An unknown status must fail locally with the vocabulary.
	orderListFlags.status = []string{"nonsense"}
	err := runOrdersList(context.Background())
	if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
		t.Errorf("unknown status should be a usage error, got %v", err)
	} else if !strings.Contains(err.Error(), "submitted") {
		t.Errorf("the error must list the valid statuses: %v", err)
	}
}

func TestOrdersShowRendersLinesAndLinkedBuilds(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	if err := runOrdersShow(context.Background(), testOrderID); err != nil {
		t.Fatalf("runOrdersShow: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"DigiKey order", "submitted", "WEB-4471",
		"LINE ITEMS", "311-10.0KLRCT-ND", "1000",
		"RAISED FOR", "Power Supply v2", "9/12",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("order dossier is missing %q:\n%s", want, text)
		}
	}
	// The distributor call has side effects, so it must not happen unasked.
	if _, hit := srv.queries[teamPath+"/orders/"+testOrderID+"/distributor-status"]; hit {
		t.Error("distributor-status was called without --distributor-status")
	}
}

func TestBuildsListSendsFiltersAndSort(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	buildListFlags.project = "Power Supply v2"
	buildListFlags.status = []string{"pending", "ordered"}
	buildListFlags.sortBy = "created_at:desc"
	if err := runBuildsList(context.Background()); err != nil {
		t.Fatalf("runBuildsList: %v", err)
	}
	q := srv.queryFor(t, teamPath[len("/api/v1"):]+"/builds")
	if got := q.Get("project_id"); got != testProjectID {
		t.Errorf("project_id = %q, want the resolved project id", got)
	}
	// status is repeatable server-side, so both go on one request.
	if got := q["status"]; len(got) != 2 || got[0] != "pending" || got[1] != "ordered" {
		t.Errorf("status = %v, want [pending ordered] on a single request", got)
	}
	if q.Get("sort_by") != "created_at" || q.Get("sort_dir") != "desc" {
		t.Errorf("sort = %q/%q, want created_at/desc", q.Get("sort_by"), q.Get("sort_dir"))
	}
	if !strings.Contains(out.String(), "Power Supply v2") {
		t.Errorf("listing is missing the build:\n%s", out)
	}

	buildListFlags.sortBy = "nonsense"
	if err := runBuildsList(context.Background()); err == nil {
		t.Error("expected an error for an unknown sort field")
	} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitUsage {
		t.Errorf("unknown sort should be a usage error, got %v", err)
	}
}

// The pipeline always prints its four stages, empty ones included, so the shape
// of the answer does not change with the data.
func TestBuildsPipelinePrintsEveryStage(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	if err := runBuildsPipeline(context.Background()); err != nil {
		t.Fatalf("runBuildsPipeline: %v", err)
	}
	text := out.String()
	for _, stage := range pipelineStages {
		if !strings.Contains(text, strings.ToUpper(stage)) {
			t.Errorf("pipeline is missing the %s stage:\n%s", stage, text)
		}
	}
	if !strings.Contains(text, "none") {
		t.Errorf("an empty stage should say so:\n%s", text)
	}
}

func TestBuildsShowIsPreviewFreeUnlessAsked(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	if err := runBuildsShow(context.Background(), testBuildID); err != nil {
		t.Fatalf("runBuildsShow: %v", err)
	}
	if _, hit := srv.queries[teamPath+"/builds/"+testBuildID+"/preview"]; hit {
		t.Error("the preview endpoint was called without --preview")
	}
	text := out.String()
	for _, want := range []string{"Power Supply v2", "RESERVED STOCK", "ORDERS"} {
		if !strings.Contains(text, want) {
			t.Errorf("build dossier is missing %q:\n%s", want, text)
		}
	}
	// A pending build has consumed nothing, and null items must not render as an
	// empty CONSUMED section claiming it was assembled and took nothing.
	if strings.Contains(text, "CONSUMED") {
		t.Errorf("a pending build must not report consumption:\n%s", text)
	}

	out.Reset()
	buildShowFlags.preview = true
	if err := runBuildsShow(context.Background(), testBuildID); err != nil {
		t.Fatalf("runBuildsShow --preview: %v", err)
	}
	if !strings.Contains(out.String(), "WILL CONSUME") {
		t.Errorf("--preview should print the consumption table:\n%s", out)
	}
}

func TestCartShowTotalsAndUnlistedItems(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	cartFlags.demands = true
	// reason is not a default column — it is the explanation you ask for once the
	// `auto` in the chosen column has raised the question.
	cartFlags.fields = append(append([]string(nil), defaultCartFields...), "reason")
	if err := runCartShow(context.Background()); err != nil {
		t.Fatalf("runCartShow: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"RC0402FR-0710KL", "DigiKey", "auto", "cheapest in stock",
		"1000",  // the optimized order quantity, not the 500 asked for
		"12.30", // 1000 × 0.0123, the line's real cost
		"1 item has no listing",
		"WHY THESE ARE HERE", "Power Supply v2",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("cart is missing %q:\n%s", want, text)
		}
	}
}

// Cart rows address parts, because the useful follow-up is `pt show @N`.
func TestCartRowsAddressParts(t *testing.T) {
	srv := newWorkflowServer(t)
	setupWorkflowApp(t, srv)

	if err := runCartShow(context.Background()); err != nil {
		t.Fatalf("runCartShow: %v", err)
	}
	got, err := ap.resolveResultRef("@1", kindPart)
	if err != nil {
		t.Fatalf("@1 after `pt cart show` should be a part: %v", err)
	}
	if got != testPartID {
		t.Errorf("@1 = %q, want the cart item's part id", got)
	}
}

func TestCheckoutsListAndShow(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)

	if err := runCheckoutsList(context.Background()); err != nil {
		t.Fatalf("runCheckoutsList: %v", err)
	}
	if !strings.Contains(out.String(), "bench repair") {
		t.Errorf("listing is missing the reason:\n%s", out)
	}

	out.Reset()
	if err := runCheckoutsShow(context.Background(), "@1"); err != nil {
		t.Fatalf("runCheckoutsShow: %v", err)
	}
	text := out.String()
	for _, want := range []string{"bench repair", "WITHDRAWN", "RC0402FR-0710KL", "1 part, 10 units"} {
		if !strings.Contains(text, want) {
			t.Errorf("checkout dossier is missing %q:\n%s", want, text)
		}
	}
}

// A checkout has no name to search by, so a bare word must say that rather than
// listing every checkout or silently matching one.
func TestCheckoutRefRejectsFreeText(t *testing.T) {
	srv := newWorkflowServer(t)
	setupWorkflowApp(t, srv)

	_, err := ap.resolveCheckoutRef(context.Background(), "bench repair")
	if err == nil {
		t.Fatal("expected an error for a text checkout reference")
	}
	if !strings.Contains(err.Error(), "pt checkouts list") {
		t.Errorf("the error must name the way to address one: %v", err)
	}
}

// JSON output stays native: quantities are numbers, absent values are null, and
// the display dash never leaks into a machine-readable field.
func TestWorkflowJSONUsesNativeTypes(t *testing.T) {
	srv := newWorkflowServer(t)
	out := setupWorkflowApp(t, srv)
	ap.format = FormatJSON

	if err := runCartShow(context.Background()); err != nil {
		t.Fatalf("runCartShow: %v", err)
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out.String()), &resp); err != nil {
		t.Fatalf("cart JSON did not parse: %v\n%s", err, out)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(resp.Items))
	}
	if v, ok := resp.Items[0]["qty"].(float64); !ok || v != 500 {
		t.Errorf("qty = %#v, want the number 500", resp.Items[0]["qty"])
	}
	// The second item has no listing, so every listing-derived field is null.
	for _, key := range []string{"dist", "sku", "unit", "order_qty", "line", "chosen"} {
		if resp.Items[1][key] != nil {
			t.Errorf("%s = %#v, want null for an item with no listing", key, resp.Items[1][key])
		}
	}
	for _, item := range resp.Items {
		for k, v := range item {
			if s, ok := v.(string); ok && s == dash {
				t.Errorf("%s carries the display placeholder into JSON", k)
			}
		}
	}
}

// A BOM line that resolved to nothing still occupies its @N slot. Dropping such
// lines from the saved result set would silently shift every later @N onto the
// wrong part — `pt show @3` would fetch the part from line 4.
func TestBOMShowKeepsUnresolvedLinesInTheIndex(t *testing.T) {
	srv := newWorkflowServer(t)
	setupWorkflowApp(t, srv)

	// The stub's revision is line 1 resolved, lines 2 and 3 unresolved.
	if err := runBOMShow(context.Background(), "Power Supply v2"); err != nil {
		t.Fatalf("runBOMShow: %v", err)
	}

	got, err := ap.resolveResultRef("@1", kindPart)
	if err != nil || got != testPartID {
		t.Errorf("@1 = (%q, %v), want the part on the first line", got, err)
	}

	// @2 is a line with no part. It must say so, not resolve to line 1's part or
	// to an empty id.
	if _, err := ap.resolveResultRef("@2", kindPart); err == nil {
		t.Fatal("@2 resolved even though that line has no part")
	} else if ee, ok := err.(*ExitError); !ok || ee.Code != ExitNotFound {
		t.Errorf("an unresolved slot should be ExitNotFound, got %v", err)
	} else if !strings.Contains(err.Error(), "U3") {
		t.Errorf("the error should name the row's designators: %v", err)
	}
}

// The same alignment holds for the stock check, whose rows are also a mix of
// resolved and unresolved lines.
func TestBOMStockKeepsRowNumbersAligned(t *testing.T) {
	srv := newWorkflowServer(t)
	setupWorkflowApp(t, srv)

	bomStockF.quantity = 10
	if err := runBOMStock(context.Background(), "Power Supply v2"); err != nil {
		t.Fatalf("runBOMStock: %v", err)
	}
	if got, err := ap.resolveResultRef("@1", kindPart); err != nil || got != testPartID {
		t.Errorf("@1 = (%q, %v), want the first line's part", got, err)
	}
	// Line 2 carries an MPN but no resolved part id, so it holds a slot with
	// nothing to look up.
	if _, err := ap.resolveResultRef("@2", kindPart); err == nil {
		t.Error("@2 resolved even though that line has no part id")
	}
}
