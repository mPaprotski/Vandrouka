package apihttp

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
)

func responseFixture(t *testing.T, pace planner.Pace) routes.Result {
	t.Helper()
	location, err := time.LoadLocation("Europe/Minsk")
	if err != nil {
		t.Fatal(err)
	}
	req := planner.Request{BudgetMinutes: 40, Start: planner.Point{Lat: 53.9, Lon: 27.5}, StartAt: time.Date(2026, 10, 24, 10, 0, 0, 0, location), Pace: pace, Seed: 42}
	a := planner.Place{ID: 10, Name: "Музей", Location: planner.Point{Lat: 53.901, Lon: 27.501}, Categories: []string{"museums", "architecture"}, Importance: 5, VisitMinutes: 10, PriceKnown: true, TicketCostMinor: 150, Schedule: planner.Schedule{Known: true, Weekly: map[time.Weekday][]planner.Interval{time.Saturday: {{OpenMinute: 600, CloseMinute: 660}}}}}
	b := planner.Place{ID: 20, Name: "Парк", Location: planner.Point{Lat: 53.902, Lon: 27.502}, Categories: []string{"parks"}, Importance: 3, VisitMinutes: 15, TicketCostMinor: 9999}
	matrix := planner.SliceMatrix{{{Reachable: true}, {Reachable: true, Seconds: 301.1234, Kilometers: 0.4}, {}}, {{}, {Reachable: true}, {Reachable: true, Seconds: 121.2345, Kilometers: 0.2}}, {{}, {Reachable: true, Seconds: 900, Kilometers: 2}, {Reachable: true}}}
	route, err := planner.New().EvaluateOrder(req, []planner.Place{a, b}, matrix, []int64{10, 20})
	if err != nil {
		t.Fatal(err)
	}
	return routes.Result{Prepared: routes.Prepared{City: places.City{Slug: "minsk", Timezone: "Europe/Minsk"}, Request: req, Places: []planner.Place{a, b}, Cards: map[int64]places.Place{10: {ID: 10, Name: a.Name, Lat: a.Location.Lat, Lon: a.Location.Lon}, 20: {ID: 20, Name: b.Name, Lat: b.Location.Lat, Lon: b.Location.Lon}}, MatrixIndices: map[int64]int{10: 1, 20: 2}}, Variants: []routes.VerifiedVariant{{Variant: planner.Variant{Profile: planner.ProfileBestMatch, Route: route}, Matrix: matrix, Shapes: []string{"first", "second"}}}}
}
func TestBuildRouteResponseExactTotalsAndUncertainty(t *testing.T) {
	result := responseFixture(t, planner.PaceNormal)
	response, err := BuildGenerateRouteResponse(result)
	if err != nil {
		t.Fatal(err)
	}
	if response.Seed != 42 || response.Timezone != "Europe/Minsk" || len(response.Variants) != 1 {
		t.Fatal("response metadata differs")
	}
	v := response.Variants[0]
	totals := v.Totals
	// Independent expectation: 301.1234+121.2345=422.3579 seconds walking,
	// 600+900=1500 seconds visiting, 1922.3579 total, .4+.2=600 meters.
	if math.Abs(totals.WalkSeconds-422.3579) > 1e-9 || totals.VisitSeconds != 1500 || math.Abs(totals.TotalSeconds-1922.3579) > 1e-9 || math.Abs(totals.DistanceM-600) > 1e-9 || totals.CostMinor != 150 {
		t.Fatalf("conversion/precision changed: %+v", totals)
	}
	if totals.Currency != "BYN" || totals.CostComplete || totals.StopsCount != 2 || totals.UnverifiedStopsCount != 1 {
		t.Fatal("unknown costs or counts misrepresented")
	}
	if v.Explanation.HoursUnconfirmedCount != 1 || v.Explanation.PricesUnconfirmedCount != 1 || len(v.Explanation.CategoryCounts) != 3 || !strings.Contains(v.Explanation.Text, "не подтверждены") {
		t.Fatal("missing uncertainty or overlapping categories")
	}
	if v.Stops[0].PlaceID != 10 || v.Stops[0].Name != "Музей" || v.Stops[0].IncomingLeg.DistanceM != 400 || v.Stops[1].IncomingLeg.DistanceM != 200 || v.Stops[1].IncomingLeg.WalkSeconds != 121.2345 {
		t.Fatal("stop card/order/confirmed transition differs")
	}
	if v.Stops[1].Departure.Sub(result.Prepared.Request.StartAt).Seconds() != totals.TotalSeconds {
		t.Fatal("end time differs; return leg must not be included")
	}
	if len(v.Labels) != 0 || v.VariantID != "variant-1" || v.Geometry.Format != "polyline6" || !reflect.DeepEqual(v.Geometry.Segments, []string{"first", "second"}) {
		t.Fatal("variant labels/geometry differ")
	}
	v.Geometry.Segments[0] = "changed"
	v.Explanation.CategoryCounts["museums"] = 99
	if result.Variants[0].Shapes[0] != "first" || result.Variants[0].Variant.Route.Metrics.CategoryCounts["museums"] != 1 {
		t.Fatal("serializer mutated service result")
	}
	// Optionally emit the actual DTO for the OpenAPI CI validator.
	if directory := os.Getenv("ROUTE_RESPONSE_EXAMPLES_DIR"); directory != "" {
		fresh, err := BuildGenerateRouteResponse(result)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(fresh)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "route-response.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestBuildRouteResponseGeometryOffAndCompleteCost(t *testing.T) {
	result := responseFixture(t, planner.PaceFast)
	result.Variants[0].Shapes = nil
	response, err := BuildGenerateRouteResponse(result)
	if err != nil {
		t.Fatal(err)
	}
	if response.Variants[0].Geometry != nil || math.Abs(response.Variants[0].Totals.WalkSeconds-422.3579*0.85) > 1e-8 {
		t.Fatal("pace or absent geometry lost")
	}
	// Same verified result with only the known-price stop.
	req := result.Prepared.Request
	matrix := result.Variants[0].Matrix
	route, err := planner.New().EvaluateOrder(req, result.Prepared.Places, matrix, []int64{10})
	if err != nil {
		t.Fatal(err)
	}
	result.Variants[0].Variant.Route = route
	response, err = BuildGenerateRouteResponse(result)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Variants[0].Totals.CostComplete || response.Variants[0].Totals.UnverifiedStopsCount != 0 {
		t.Fatal("fully known cost marked incomplete")
	}
	data, err := json.Marshal(response)
	if err != nil || !strings.Contains(string(data), `"geometry":null`) {
		t.Fatal("geometry should serialize as null", err)
	}
}
func TestBuildRouteResponseEmpty(t *testing.T) {
	result := responseFixture(t, planner.PaceNormal)
	result.Variants = nil
	response, err := BuildGenerateRouteResponse(result)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(response)
	if err != nil || !strings.Contains(string(data), `"variants":[]`) {
		t.Fatal("empty successful response changed", err)
	}
}
func TestBuildRouteResponseRejectsInconsistentResults(t *testing.T) {
	for _, name := range []string{"unknown stop", "duplicate stop", "timestamp", "total", "cost", "distance", "count", "unknown flag", "missing matrix", "empty geometry", "single label", "time budget", "ticket budget"} {
		t.Run(name, func(t *testing.T) {
			result := responseFixture(t, planner.PaceNormal)
			r := &result.Variants[0].Variant.Route
			switch name {
			case "unknown stop":
				r.Stops[0].PlaceID = 999
			case "duplicate stop":
				r.Stops[1].PlaceID = 10
			case "timestamp":
				r.Stops[1].Arrival = r.Stops[1].Arrival.Add(time.Millisecond)
			case "total":
				r.Metrics.TotalMinutes++
			case "cost":
				r.Metrics.CostMinor++
			case "distance":
				r.Metrics.WalkKM++
			case "count":
				r.Metrics.UnverifiedCount = 2
			case "unknown flag":
				r.Stops[1].PriceUnconfirmed = false
			case "missing matrix":
				result.Variants[0].Matrix = nil
			case "empty geometry":
				result.Variants[0].Shapes = []string{""}
			case "single label":
				result.Variants[0].Variant.Labels = []planner.Label{planner.LabelBudget}
			case "time budget":
				result.Prepared.Request.BudgetMinutes = 32
			case "ticket budget":
				budget := int64(149)
				result.Prepared.Request.MaxTicketCostMinor = &budget
			}
			if _, err := BuildGenerateRouteResponse(result); err == nil {
				t.Fatal("inconsistent service result accepted")
			}
		})
	}
}
