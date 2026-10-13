package routes_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/plannercheck"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

type directionsStub struct {
	calls  int
	points [][]routing.Point
	paths  []routing.Route
	errs   []error
	cancel context.CancelFunc
}

func (d *directionsStub) Route(ctx context.Context, points []routing.Point) (routing.Route, error) {
	i := d.calls
	d.calls++
	d.points = append(d.points, append([]routing.Point(nil), points...))
	if d.cancel != nil {
		d.cancel()
	}
	if i < len(d.errs) && d.errs[i] != nil {
		return routing.Route{}, d.errs[i]
	}
	if i >= len(d.paths) {
		return routing.Route{}, errors.New("unexpected Directions call")
	}
	return d.paths[i], nil
}
func directionPath(a, b float64) routing.Route {
	return routing.Route{Legs: []routing.Leg{{Reachable: true, Seconds: a, Kilometers: 0.4}, {Reachable: true, Seconds: b, Kilometers: 0.2}}, Shapes: []string{"segment-one", "segment-two"}, Seconds: 9999, Kilometers: 9999}
}
func TestGenerateUsesDirectionsEvenWithoutGeometry(t *testing.T) {
	for _, include := range []bool{false, true} {
		catalog, req, m := candidateFixture()
		directions := &directionsStub{paths: []routing.Route{directionPath(301.1234, 121.2345)}}
		router := &matrixRouter{matrix: m}
		result, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(directions)).Generate(context.Background(), "minsk", req, include)
		if err != nil || len(result.Variants) != 1 || directions.calls != 1 || router.calls != 1 {
			t.Fatal("one verified result expected", err)
		}
		v := result.Variants[0]
		route := v.Variant.Route
		// Directions legs override Matrix: walk 422.3579 sec, visits 1500 sec,
		// total 1922.3579 sec, distance .6 km; aggregate provider totals are ignored.
		if math.Abs(route.Metrics.TotalMinutes-1922.3579/60) > 1e-9 || math.Abs(route.Metrics.WalkKM-0.6) > 1e-12 || route.Metrics.CostMinor != 250 {
			t.Fatalf("metrics still use Matrix/aggregate: %+v", route.Metrics)
		}
		if len(directions.points[0]) != 3 || directions.points[0][0] != (routing.Point{Lat: req.Start.Lat, Lon: req.Start.Lon}) {
			t.Fatal("Directions must contain start and stops, without return")
		}
		for i, stop := range route.Stops {
			p := result.Prepared.Places[result.Prepared.MatrixIndices[stop.PlaceID]-1]
			if directions.points[0][i+1] != (routing.Point{Lat: p.Location.Lat, Lon: p.Location.Lon}) {
				t.Fatal("Directions stop order differs")
			}
		}
		if v.Matrix.Leg(0, 1).Seconds != 301.1234 || m[0][1].Seconds != 360 || v.Matrix.Leg(2, 1).Seconds != 900 {
			t.Fatal("confirmed matrix corrupted base or reverse leg")
		}
		if !include && v.Shapes != nil || include && !reflect.DeepEqual(v.Shapes, directions.paths[0].Shapes) {
			t.Fatal("geometry output policy differs")
		}
		if len(v.Variant.Labels) != 0 {
			t.Fatal("single final variant must have no comparison labels")
		}
		if err := plannercheck.Audit(result.Prepared.Request, plannercheck.Catalog{Places: result.Prepared.Places}, v.Matrix, route); err != nil {
			t.Fatal(err)
		}
	}
}
func TestGenerateDiscardsInfeasibleDirections(t *testing.T) {
	for _, name := range []string{"tiny budget excess", "closed during visit", "unreachable", "no route"} {
		t.Run(name, func(t *testing.T) {
			catalog, req, m := candidateFixture()
			path := directionPath(360, 240)
			directions := &directionsStub{}
			switch name {
			case "tiny budget excess":
				path.Legs[0].Seconds = 360.000001
			case "closed during visit":
				req.BudgetMinutes = 90
				path.Legs[1].Seconds = 2400
			case "unreachable":
				path.Legs[1].Reachable = false
			case "no route":
				directions.errs = []error{routing.ErrNoRoute}
			}
			directions.paths = []routing.Route{path}
			result, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(&matrixRouter{matrix: m})), routes.WithDirections(directions)).Generate(context.Background(), "minsk", req, true)
			if err != nil || result.Variants == nil || len(result.Variants) != 0 || directions.calls != 1 {
				t.Fatal("infeasible final variant must yield empty success", err)
			}
		})
	}
}
func TestGenerateDirectionsErrors(t *testing.T) {
	for _, name := range []string{"provider error", "missing legs", "invalid seconds", "invalid distance", "missing geometry", "empty segment"} {
		t.Run(name, func(t *testing.T) {
			catalog, req, m := candidateFixture()
			path := directionPath(360, 240)
			directions := &directionsStub{}
			switch name {
			case "provider error":
				directions.errs = []error{routing.ErrRateLimited}
			case "missing legs":
				path.Legs = path.Legs[:1]
			case "invalid seconds":
				path.Legs[0].Seconds = math.NaN()
			case "invalid distance":
				path.Legs[0].Kilometers = -1
			case "missing geometry":
				path.Shapes = nil
			case "empty segment":
				path.Shapes = []string{""}
			}
			directions.paths = []routing.Route{path}
			_, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(&matrixRouter{matrix: m})), routes.WithDirections(directions)).Generate(context.Background(), "minsk", req, true)
			if !errors.Is(err, routes.ErrRoutingUnavailable) {
				t.Fatal("provider failures must not become empty success", err)
			}
			if name == "provider error" && !errors.Is(err, routing.ErrRateLimited) {
				t.Fatal("provider cause lost")
			}
		})
	}
	catalog, req, m := candidateFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	directions := &directionsStub{paths: []routing.Route{directionPath(360, 240)}, cancel: cancel}
	if _, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(&matrixRouter{matrix: m})), routes.WithDirections(directions)).Generate(ctx, "minsk", req, true); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
func TestGenerateEmptySkipsDirections(t *testing.T) {
	catalog := &catalogStub{cities: []places.City{city()}}
	directions := &directionsStub{}
	result, err := routes.NewService(catalog, routes.WithDirections(directions)).Generate(context.Background(), "minsk", request(), true)
	if err != nil || len(result.Variants) != 0 || result.Variants == nil || directions.calls != 0 {
		t.Fatal("empty catalog must skip provider calls", err)
	}
}
func TestGeneratePreservesConfiguredScoring(t *testing.T) {
	catalog, req, m := candidateFixture()
	engine, err := planner.NewWithWeights(planner.ScoringWeights{WalkKM: 2, CostBYN: 3, Unverified: 4})
	if err != nil {
		t.Fatal(err)
	}
	directions := &directionsStub{paths: []routing.Route{directionPath(360, 240)}}
	result, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(&matrixRouter{matrix: m})), routes.WithVariantPlanner(engine), routes.WithDirections(directions)).Generate(context.Background(), "minsk", req, false)
	if err != nil || len(result.Variants) != 1 {
		t.Fatal(err)
	}
	// Interest 6 - distance .6*2 - BYN 2.5*3 = -2.7.
	if math.Abs(result.Variants[0].Variant.Route.Metrics.Score-(-2.7)) > 1e-9 {
		t.Fatal("Directions evaluation reset scoring weights")
	}
}

// A fixed candidate generator isolates final ranking and label recomputation.
type fixedCandidates struct{ variants []planner.Variant }

func (e fixedCandidates) PlanVariants(planner.Request, []planner.Place, planner.DistanceMatrix, bool) ([]planner.Variant, error) {
	return e.variants, nil
}
func TestGenerateLabelsOnlyFinalVariants(t *testing.T) {
	catalog, req, m := candidateFixture()
	req.BudgetMinutes = 60
	engine := fixedCandidates{variants: []planner.Variant{
		{Profile: planner.ProfileBestMatch, Route: planner.Route{Stops: []planner.Stop{{PlaceID: 70}}}, Labels: []planner.Label{planner.LabelLessWalking}},
		{Profile: planner.ProfileMorePlaces, Route: planner.Route{Stops: []planner.Stop{{PlaceID: 20}}}, Labels: []planner.Label{planner.LabelBestMatch}},
	}}
	directions := &directionsStub{paths: []routing.Route{
		{Legs: []routing.Leg{{Reachable: true, Seconds: 600, Kilometers: 2}}, Shapes: []string{"a"}},
		{Legs: []routing.Leg{{Reachable: true, Seconds: 300, Kilometers: 0.5}}, Shapes: []string{"b"}},
	}}
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(&matrixRouter{matrix: m})), routes.WithVariantPlanner(engine), routes.WithRouteEvaluator(planner.New()), routes.WithDirections(directions))
	result, err := service.Generate(context.Background(), "minsk", req, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Variants) != 2 || result.Variants[0].Variant.Route.Stops[0].PlaceID != 20 || result.Variants[0].Shapes[0] != "b" {
		t.Fatal("final score ranking lost associated geometry")
	}
	if reflect.DeepEqual(result.Variants[0].Variant.Labels, engine.variants[1].Labels) {
		t.Fatal("preliminary labels were retained")
	}
	expected := planner.AssignLabels([]planner.Variant{result.Variants[0].Variant, result.Variants[1].Variant})
	for i := range expected {
		if !reflect.DeepEqual(expected[i].Labels, result.Variants[i].Variant.Labels) {
			t.Fatal("labels do not use final metrics")
		}
	}
	// After the second variant disappears, old comparative labels must be cleared.
	directions.calls = 0
	directions.errs = []error{nil, routing.ErrNoRoute}
	result, err = service.Generate(context.Background(), "minsk", req, false)
	if err != nil || len(result.Variants) != 1 || len(result.Variants[0].Variant.Labels) != 0 {
		t.Fatal("remaining variant retained comparative labels", err)
	}
}

func TestGenerateMissingVerificationDependencyDoesNotCallMatrix(t *testing.T) {
	catalog, req, m := candidateFixture()
	router := &matrixRouter{matrix: m}
	_, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router))).Generate(context.Background(), "minsk", req, true)
	if err == nil || router.calls != 0 {
		t.Fatal("missing Directions must fail before a billed Matrix call", err)
	}
}
