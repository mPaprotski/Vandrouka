package apihttp

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

func recalculationFixture(t *testing.T) routes.Baseline {
	t.Helper()
	r := responseFixture(t, planner.PaceNormal)
	return routes.Baseline{Prepared: r.Prepared, Matrix: r.Variants[0].Matrix, Order: []int64{10, 20}, Verified: r.Variants[0]}
}
func checkedRecalculation(t *testing.T, b routes.Baseline, ids []int64) routes.VerifiedVariant {
	t.Helper()
	route, err := planner.New().EvaluateOrder(b.Prepared.Request, b.Prepared.Places, b.Matrix, ids)
	if err != nil {
		t.Fatal(err)
	}
	return routes.VerifiedVariant{Variant: planner.Variant{Profile: planner.ProfileBestMatch, Route: route}, Matrix: b.Matrix}
}
func nearDelta(a, b float64) bool { return math.Abs(a-b) < 1e-8 }

func TestRecalculateResponseRemovalAndEmpty(t *testing.T) {
	for _, empty := range []bool{false, true} {
		baseline := recalculationFixture(t)
		ids := []int64{10}
		if empty {
			ids = nil
		}
		after := checkedRecalculation(t, baseline, ids)
		response, err := BuildRecalculateRouteResponse(baseline, after)
		if err != nil {
			t.Fatal(err)
		}
		if response.Seed != 42 || response.Timezone != "Europe/Minsk" || response.Diff.PricesConfirmed || len(response.Diff.AddedPlaceIDs) != 0 {
			t.Fatal("metadata or unknown-price comparison")
		}
		if empty {
			if !nearDelta(response.Diff.TotalSecondsDelta, -1922.3579) || !nearDelta(response.Diff.DistanceMDelta, -600) || response.Diff.CostMinorDelta != -150 || len(response.Diff.RemovedPlaceIDs) != 2 || response.Diff.RemovedPlaceIDs[0] != 10 || response.Diff.RemovedPlaceIDs[1] != 20 || response.Variant.Totals.TotalSeconds != 0 || !response.Variant.Totals.CostComplete || response.Variant.Geometry != nil || len(response.Variant.Stops) != 0 {
				t.Fatalf("empty response %+v", response)
			}
		} else {
			// Removed park costs nothing known: delta excludes 900s visit +121.2345s walking.
			if !nearDelta(response.Diff.TotalSecondsDelta, -1021.2345) || !nearDelta(response.Diff.DistanceMDelta, -200) || response.Diff.CostMinorDelta != 0 || len(response.Diff.RemovedPlaceIDs) != 1 || response.Diff.RemovedPlaceIDs[0] != 20 || !strings.Contains(response.Diff.Text, "Парк") {
				t.Fatalf("diff %+v", response.Diff)
			}
		}
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"added_place_ids":[]`) {
			t.Fatal("null collection")
		}
		if empty && !strings.Contains(string(body), `"stops":[]`) {
			t.Fatal("empty stops")
		}
		if directory := os.Getenv("ROUTE_RESPONSE_EXAMPLES_DIR"); directory != "" && empty {
			if err := os.WriteFile(filepath.Join(directory, "recalculate-response.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		response.Diff.RemovedPlaceIDs[0] = 999
		response.Variant.Explanation.CategoryCounts["museums"] = 99
		if baseline.Order[0] != 10 || baseline.Verified.Variant.Route.Metrics.CategoryCounts["museums"] != 1 {
			t.Fatal("response aliases baseline")
		}
	}
}

func TestRecalculateResponseAdditionAndOrderOnly(t *testing.T) {
	b := recalculationFixture(t)
	old := b.Matrix.(planner.SliceMatrix)
	matrix := make(planner.SliceMatrix, 4)
	for i := range matrix {
		matrix[i] = make([]routing.Leg, 4)
		if i < len(old) {
			copy(matrix[i], old[i])
		}
	}
	matrix[0][2] = routing.Leg{Reachable: true, Seconds: 100, Kilometers: .25}
	matrix[2][1] = routing.Leg{Reachable: true, Seconds: 200, Kilometers: .15}
	matrix[2][3] = routing.Leg{Reachable: true, Seconds: 61.3456, Kilometers: .3}
	b.Matrix = matrix
	b.Verified.Matrix = matrix
	b.Prepared.Places = append(b.Prepared.Places, planner.Place{ID: 30, Name: "Новое место", Categories: []string{"parks"}, Importance: 3, VisitMinutes: 5, PriceKnown: true, TicketCostMinor: 275})
	b.Prepared.Cards[30] = places.Place{ID: 30, Name: "Новое место"}
	b.Prepared.MatrixIndices[30] = 3
	response, err := BuildRecalculateRouteResponse(b, checkedRecalculation(t, b, []int64{10, 20, 30}))
	if err != nil {
		t.Fatal(err)
	}
	if !nearDelta(response.Diff.TotalSecondsDelta, 361.3456) || !nearDelta(response.Diff.DistanceMDelta, 300) || response.Diff.CostMinorDelta != 275 || len(response.Diff.AddedPlaceIDs) != 1 || response.Diff.AddedPlaceIDs[0] != 30 || len(response.Diff.RemovedPlaceIDs) != 0 || !strings.Contains(response.Diff.Text, "Новое место") {
		t.Fatalf("addition %+v", response.Diff)
	}
	response, err = BuildRecalculateRouteResponse(b, checkedRecalculation(t, b, []int64{20, 10}))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Diff.AddedPlaceIDs) != 0 || len(response.Diff.RemovedPlaceIDs) != 0 || !nearDelta(response.Diff.TotalSecondsDelta, -122.3579) || !nearDelta(response.Diff.DistanceMDelta, -200) || !strings.Contains(response.Diff.Text, "Набор мест не изменился") {
		t.Fatalf("order diff %+v", response.Diff)
	}
}

func TestRecalculateResponseKnownPricesAndInvalidData(t *testing.T) {
	b := recalculationFixture(t)
	single := checkedRecalculation(t, b, []int64{10})
	b.Order = []int64{10}
	b.Verified = single
	response, err := BuildRecalculateRouteResponse(b, single)
	if err != nil || !response.Diff.PricesConfirmed || response.Diff.CostMinorDelta != 0 {
		t.Fatalf("known prices %v", err)
	}
	for _, name := range []string{"baseline order", "after metrics", "seed", "empty geometry", "empty cost"} {
		t.Run(name, func(t *testing.T) {
			base := recalculationFixture(t)
			after := checkedRecalculation(t, base, nil)
			switch name {
			case "baseline order":
				base.Order = []int64{20, 10}
			case "after metrics":
				after = checkedRecalculation(t, base, []int64{10})
				after.Variant.Route.Metrics.TotalMinutes++
			case "seed":
				after.Variant.Route.Seed++
			case "empty geometry":
				after.Shapes = []string{"incorrect"}
			case "empty cost":
				after.Variant.Route.Metrics.CostMinor = 1
			}
			if _, err := BuildRecalculateRouteResponse(base, after); err == nil {
				t.Fatal("invalid data accepted")
			}
		})
	}
	// The original generate contract must continue rejecting empty variants.
	empty := checkedRecalculation(t, b, nil)
	if _, err := BuildGenerateRouteResponse(routes.Result{Prepared: b.Prepared, Variants: []routes.VerifiedVariant{empty}}); err == nil {
		t.Fatal("generation now accepts empty variant")
	}
}
