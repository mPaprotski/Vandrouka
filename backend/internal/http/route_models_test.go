package apihttp

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGenerateRouteRequestOptionalValues(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		budget     *int64
	}{
		{name: "omitted", body: `{}`}, {name: "null", body: `{"ticket_budget_minor":null}`},
		{name: "zero", body: `{"ticket_budget_minor":0}`, budget: modelPointer(int64(0))},
		{name: "kopecks", body: `{"ticket_budget_minor":2501}`, budget: modelPointer(int64(2501))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request GenerateRouteRequest
			if err := json.Unmarshal([]byte(tc.body), &request); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(request.TicketBudgetMinor, tc.budget) {
				t.Fatalf("budget presence/value differs")
			}
			data, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var again GenerateRouteRequest
			if err := json.Unmarshal(data, &again); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(again.TicketBudgetMinor, tc.budget) {
				t.Fatal("budget presence lost on roundtrip")
			}
		})
	}
	var request GenerateRouteRequest
	if err := json.Unmarshal([]byte(`{"seed":0,"include_geometry":false,"budget_minutes":0,"city":""}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.Seed == nil || *request.Seed != 0 || request.IncludeGeometry == nil || *request.IncludeGeometry || request.BudgetMinutes == nil || *request.BudgetMinutes != 0 || request.City == nil || *request.City != "" {
		t.Fatal("explicit zero/false/empty lost")
	}
	var missing GenerateRouteRequest
	if missing.Seed != nil || missing.IncludeGeometry != nil || missing.BudgetMinutes != nil || missing.City != nil || missing.Start != nil || missing.StartAt != nil {
		t.Fatal("missing values must remain detectable")
	}
}
func TestRouteResponsePreservesPrecisionAndJSONNames(t *testing.T) {
	arrival := time.Date(2026, 10, 24, 11, 0, 0, 123456789, time.FixedZone("Minsk", 10800))
	response := GenerateRouteResponse{Seed: 42, Timezone: "Europe/Minsk", Variants: []RouteVariant{{
		VariantID: "variant-1", Profile: "best_match",
		Stops:       []RouteStop{{PlaceID: 1, Name: "Пример", Location: Coordinates{Lat: 53.9, Lon: 27.5}, Arrival: arrival, Departure: arrival.Add(10 * time.Minute), VisitSeconds: 600, IncomingLeg: RouteLeg{WalkSeconds: 0.123456789, DistanceM: 12.3456789}, HoursUnconfirmed: true, PriceUnconfirmed: true}},
		Totals:      RouteTotals{WalkSeconds: 0.123456789, VisitSeconds: 600, TotalSeconds: 600.123456789, DistanceM: 12.3456789, CostMinor: 2501, Currency: "BYN", StopsCount: 1, UnverifiedStopsCount: 1},
		Explanation: RouteExplanation{Text: "Цена и часы не подтверждены.", HoursUnconfirmedCount: 1, PricesUnconfirmedCount: 1},
	}}}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var actual GenerateRouteResponse
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	stop := actual.Variants[0].Stops[0]
	totals := actual.Variants[0].Totals
	if stop.IncomingLeg.WalkSeconds != 0.123456789 || totals.TotalSeconds != 600.123456789 || totals.DistanceM != 12.3456789 || totals.CostMinor != 2501 {
		t.Fatalf("units/precision lost: %s", data)
	}
	if !stop.Arrival.Equal(arrival) || stop.Departure.Sub(stop.Arrival) != 10*time.Minute {
		t.Fatal("timestamps lost precision")
	}
	for _, field := range []string{`"walk_seconds":0.123456789`, `"total_seconds":600.123456789`, `"distance_m":12.3456789`, `"cost_minor":2501`, `"hours_unconfirmed":true`, `"price_unconfirmed":true`, `"cost_complete":false`, `"geometry":null`, `"labels":[]`, `"category_counts":{}`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("missing %s in %s", field, data)
		}
	}
	if response.Variants[0].Labels != nil || response.Variants[0].Explanation.CategoryCounts != nil {
		t.Fatal("serialization mutated the source")
	}
	response.Variants[0].Geometry = &RouteGeometry{Format: "polyline6", Segments: []string{"first", "second"}}
	data, err = json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"geometry":{"format":"polyline6","segments":["first","second"]}`) {
		t.Fatal("geometry format/order changed")
	}
}
func TestRequiredRouteCollectionsAreNotNull(t *testing.T) {
	data, err := json.Marshal(GenerateRouteResponse{Seed: 42, Timezone: "Europe/Minsk"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"variants":[]`) {
		t.Fatalf("empty result must contain []: %s", data)
	}
	data, err = json.Marshal(RouteVariant{})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"labels":[]`, `"stops":[]`, `"category_counts":{}`, `"geometry":null`} {
		if !strings.Contains(string(data), value) {
			t.Fatalf("missing %s in %s", value, data)
		}
	}
}
func TestAPIErrorJSON(t *testing.T) {
	data, err := json.Marshal(APIError{Error: ErrorDetail{Code: "invalid_request", Message: "Некорректный запрос"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"error":{"code":"invalid_request","message":"Некорректный запрос"}}` {
		t.Fatalf("error contract changed: %s", data)
	}
}
func TestRouteModelsRejectNonFiniteJSON(t *testing.T) {
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := json.Marshal(RouteLeg{WalkSeconds: n}); err == nil {
			t.Fatal("invalid JSON number accepted")
		}
	}
}
func modelPointer[T any](value T) *T { return &value }
