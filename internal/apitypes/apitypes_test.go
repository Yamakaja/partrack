package apitypes

import (
	"encoding/json"
	"testing"
)

// The wire format is the contract between the handlers and every client — the
// web frontend and `pt`. These tests pin the shapes that are easy to get subtly
// wrong, so a refactor that changes the JSON fails here rather than in
// production.

// The list envelope is what `GET /teams` returns. A client decoding a bare array
// against it fails at runtime, which is exactly the bug that motivated this
// package, so the envelope's key set is asserted explicitly.
func TestListEnvelopeKeys(t *testing.T) {
	data, err := json.Marshal(List[Team]{
		Total: 1, Limit: 50, Offset: 0,
		Items: []Team{{ID: "t1", Name: "Lab", Role: "member"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("envelope is not a JSON object: %s", data)
	}
	for _, key := range []string{"total", "limit", "offset", "items"} {
		if _, ok := got[key]; !ok {
			t.Errorf("envelope is missing %q: %s", key, data)
		}
	}
	if len(got) != 4 {
		t.Errorf("envelope has %d keys, want exactly 4: %s", len(got), data)
	}
}

// An empty result must encode as [], never null: a client ranging over the items
// is fine either way, but `.items | length` in jq is not.
func TestListEmptyItemsEncodeAsArray(t *testing.T) {
	data, err := json.Marshal(List[Team]{Items: []Team{}})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Items) != "[]" {
		t.Errorf("empty items encoded as %s, want []", got.Items)
	}
}

// User-specified parameters are a distinct type precisely so the source keys are
// absent rather than null. Collapsing the two into one struct with omitempty
// would silently drop a distributor's null source_distributor_name instead.
func TestParameterSourceKeysDifferByType(t *testing.T) {
	userJSON, err := json.Marshal(UserParameter{
		PartParameter: PartParameter{ID: "p1", Name: "Notes", Value: "house standard"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var user map[string]json.RawMessage
	if err := json.Unmarshal(userJSON, &user); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source_distributor_id", "source_distributor_name"} {
		if _, ok := user[key]; ok {
			t.Errorf("a user parameter must not carry %q: %s", key, userJSON)
		}
	}
	// The embedded base fields must be flattened, not nested under a key.
	if _, ok := user["value"]; !ok {
		t.Errorf("embedded base fields were not flattened: %s", userJSON)
	}

	distJSON, err := json.Marshal(DistributorParameter{
		PartParameter:       PartParameter{ID: "p2", Name: "Resistance", Value: "10 kOhms"},
		SourceDistributorID: "d1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var dist map[string]json.RawMessage
	if err := json.Unmarshal(distJSON, &dist); err != nil {
		t.Fatal(err)
	}
	// Present-and-null, not absent: a distributor parameter always reports its
	// source, even when the distributor's display name is unknown.
	raw, ok := dist["source_distributor_name"]
	if !ok {
		t.Errorf("a distributor parameter must carry source_distributor_name: %s", distJSON)
	} else if string(raw) != "null" {
		t.Errorf("source_distributor_name = %s, want null", raw)
	}
}

// The location inventory endpoint predates the standard envelope and carries
// only items and total. Widening it to List would add keys to a live response.
func TestLocationInventoryEnvelopeIsNarrow(t *testing.T) {
	data, err := json.Marshal(LocationInventoryList{
		Items: []LocationInventoryItem{}, Total: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["items"] == nil {
		t.Errorf("expected exactly {items, total}, got %s", data)
	}
}

// Nullable columns must round-trip as null rather than being omitted: a client
// distinguishes "no minimum set" from "minimum is zero".
func TestNullableQuantitiesEncodeAsNull(t *testing.T) {
	data, err := json.Marshal(PartListItem{ID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"quantity_on_hand", "minimum_stock_quantity", "reorder_quantity",
		"inventory_entry_id", "storage_location_label", "mpn",
	} {
		raw, ok := got[key]
		if !ok {
			t.Errorf("%q was omitted; it must be present and null", key)
			continue
		}
		if string(raw) != "null" {
			t.Errorf("%q = %s, want null", key, raw)
		}
	}
	// parameters is the one deliberately omitempty field — it only appears when
	// the request carried a category filter.
	if _, ok := got["parameters"]; ok {
		t.Errorf("parameters should be omitted when empty: %s", data)
	}
}

// The order header carries three fields that are explicitly present-and-null
// rather than omitted, and an integrations object that defaults to {} rather
// than null. Both were map-built before these types existed; adding `omitempty`
// to any of them would be a silent wire change.
func TestOrderNullableFieldsStayPresent(t *testing.T) {
	data, err := json.Marshal(Order{ID: "o1", Integrations: json.RawMessage("{}")})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"submission_method", "external_ref_linked_at", "received_at",
		"external_reference", "notes", "purchase_order_number", "distributor_api_type",
	} {
		raw, ok := got[key]
		if !ok {
			t.Errorf("%q is absent; it must be present and null", key)
			continue
		}
		if string(raw) != "null" {
			t.Errorf("%q = %s, want null", key, raw)
		}
	}
	if string(got["integrations"]) != "{}" {
		t.Errorf("integrations = %s, want {} — a client indexes into it without a nil check",
			got["integrations"])
	}
}

// OrderDetail embeds Order, which must flatten: a nested "Order" key would break
// every client reading order.status off the detail response.
func TestOrderDetailFlattensTheHeader(t *testing.T) {
	data, err := json.Marshal(OrderDetail{
		Order: Order{ID: "o1", Status: "draft", Integrations: json.RawMessage("{}")},
		Items: []OrderLineItem{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if _, nested := got["Order"]; nested {
		t.Fatalf("the header did not flatten: %s", data)
	}
	for _, key := range []string{"id", "status", "line_item_count", "items"} {
		if _, ok := got[key]; !ok {
			t.Errorf("detail is missing %q: %s", key, data)
		}
	}
}

// A build's consumed items are null until it is assembled, which is a different
// statement from an assembled build that consumed nothing. Adding omitempty or
// pre-allocating the slice would collapse the two.
func TestBuildConsumedItemsAreNullBeforeAssembly(t *testing.T) {
	data, err := json.Marshal(BuildDetail{ID: "b1"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["items"]) != "null" {
		t.Errorf("items = %s, want null for a build that is not assembled", got["items"])
	}
	if _, ok := got["kitting_session"]; !ok {
		t.Error("kitting_session must be present and null, not omitted")
	}

	assembled, err := json.Marshal(BuildDetail{ID: "b1", Items: []BuildConsumedItem{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(assembled, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["items"]) != "[]" {
		t.Errorf("items = %s, want [] for an assembled build that consumed nothing", got["items"])
	}
}

// A cart listing that the user chose carries no reason; one the sourcing policy
// resolved does. The two keys are omitempty precisely so "chosen by hand" does
// not render as an empty explanation.
func TestCartListingReasonIsOmittedWhenChosen(t *testing.T) {
	chosen, err := json.Marshal(CartListing{ListingID: "l1", Chosen: true})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(chosen, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["reason"]; ok {
		t.Errorf("reason should be omitted for a hand-chosen listing: %s", chosen)
	}
	if _, ok := got["preferred_stale"]; ok {
		t.Errorf("preferred_stale should be omitted for a hand-chosen listing: %s", chosen)
	}

	resolved, err := json.Marshal(CartListing{ListingID: "l1", Reason: "cheapest_in_stock"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(resolved, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["reason"]) != `"cheapest_in_stock"` {
		t.Errorf("reason = %s, want the policy rule that applied", got["reason"])
	}
	// A cart item always reports its listing key, even when nothing resolved.
	item, err := json.Marshal(CartItem{ID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(item, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["listing"]) != "null" {
		t.Errorf("listing = %s, want null when no listing resolved", got["listing"])
	}
}

// A BOM line's inherited-resolution fields are omitted rather than null, which is
// how the frontend tells "this line did not inherit" from "it inherited from
// nowhere". The resolved-part fields opposite them are present-and-null.
func TestBOMLineItemOmitsInheritanceUnlessInherited(t *testing.T) {
	data, err := json.Marshal(BOMLineItem{ID: "li1", Substitutes: []BOMSubstituteEntry{}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"origin_resolution_state", "origin_revision_label", "resolved_part_flag",
		"value", "footprint", "interpretation", "resolve_item",
	} {
		if _, ok := got[key]; ok {
			t.Errorf("%q should be omitted when unset: %s", key, data)
		}
	}
	for _, key := range []string{"resolved_part_id", "resolved_part_mpn", "quantity_on_hand", "resolution_state"} {
		if raw, ok := got[key]; !ok || string(raw) != "null" {
			t.Errorf("%q must be present and null, got (%s, present=%v)", key, raw, ok)
		}
	}
	if string(got["substitutes"]) != "[]" {
		t.Errorf("substitutes = %s, want []", got["substitutes"])
	}
}
