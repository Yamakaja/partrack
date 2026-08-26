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
