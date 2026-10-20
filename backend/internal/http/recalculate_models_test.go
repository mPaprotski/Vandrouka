package apihttp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRecalculateRequestPresenceAndRoundTrip(t *testing.T) {
	for _, op := range []RouteChangeType{RouteChangeAdd, RouteChangeRemove} {
		body := `{"request":{"ticket_budget_minor":0,"seed":0,"include_geometry":false},"base_place_ids":[3,1,2],"change":{"type":"` + string(op) + `","place_id":2}}`
		var req RecalculateRouteRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		if req.Request == nil || req.Change == nil || req.Change.Type != op || req.Change.PlaceID == nil || *req.Change.PlaceID != 2 || !reflect.DeepEqual(req.BasePlaceIDs, []int64{3, 1, 2}) {
			t.Fatalf("request %+v", req)
		}
		if req.Request.TicketBudgetMinor == nil || *req.Request.TicketBudgetMinor != 0 || req.Request.IncludeGeometry == nil || *req.Request.IncludeGeometry || req.Request.Seed == nil || *req.Request.Seed != 0 {
			t.Fatal("lost explicit defaults")
		}
		b, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		var again RecalculateRouteRequest
		if err := json.Unmarshal(b, &again); err != nil || !reflect.DeepEqual(req, again) {
			t.Fatalf("round trip: %v", err)
		}
	}
	var missing RecalculateRouteRequest
	if err := json.Unmarshal([]byte(`{}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Request != nil || missing.Change != nil || missing.BasePlaceIDs != nil {
		t.Fatal("lost missing fields")
	}
	var explicit RecalculateRouteRequest
	if err := json.Unmarshal([]byte(`{"request":{},"base_place_ids":[],"change":{"type":"add","place_id":0}}`), &explicit); err != nil {
		t.Fatal(err)
	}
	if explicit.Request == nil || explicit.BasePlaceIDs == nil || explicit.Change.PlaceID == nil || *explicit.Change.PlaceID != 0 {
		t.Fatal("lost explicit empty/zero")
	}
}

func TestRecalculateResponseSignedPrecisionAndEmptyRoute(t *testing.T) {
	response := RecalculateRouteResponse{Seed: 42, Timezone: "Europe/Minsk", Variant: RouteVariant{VariantID: "variant-1", Profile: "best_match", Totals: RouteTotals{Currency: "BYN", CostComplete: true}, Explanation: RouteExplanation{Text: "Маршрут пуст."}}, Diff: RouteDiff{TotalSecondsDelta: -660.123456789, DistanceMDelta: -60.987654321, CostMinorDelta: -2501, PricesConfirmed: true, Text: "Удалено место."}}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"total_seconds_delta":-660.123456789`, `"distance_m_delta":-60.987654321`, `"cost_minor_delta":-2501`, `"added_place_ids":[]`, `"removed_place_ids":[]`, `"stops":[]`, `"labels":[]`, `"geometry":null`, `"category_counts":{}`} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("missing %s in %s", fragment, data)
		}
	}
	if response.Diff.AddedPlaceIDs != nil || response.Diff.RemovedPlaceIDs != nil || response.Variant.Stops != nil {
		t.Fatal("marshal mutated model")
	}
	var again RecalculateRouteResponse
	if err := json.Unmarshal(data, &again); err != nil || again.Diff.TotalSecondsDelta != response.Diff.TotalSecondsDelta || again.Diff.DistanceMDelta != response.Diff.DistanceMDelta || again.Diff.CostMinorDelta != response.Diff.CostMinorDelta {
		t.Fatalf("precision: %v", err)
	}
	response.Diff.AddedPlaceIDs = []int64{8, 5}
	response.Diff.RemovedPlaceIDs = []int64{3, 1}
	data, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &again); err != nil || !reflect.DeepEqual(again.Diff.AddedPlaceIDs, []int64{8, 5}) || !reflect.DeepEqual(again.Diff.RemovedPlaceIDs, []int64{3, 1}) {
		t.Fatal("diff ID order lost")
	}
}
