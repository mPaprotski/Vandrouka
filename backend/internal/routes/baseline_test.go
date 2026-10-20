package routes_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

func TestBaselineFreshDirectionsAndStableOrder(t *testing.T) {
	for _, geometry := range []bool{false, true} {
		catalog, req, m := candidateFixture()
		catalog.cards = catalog.cards[1:]
		// Matrix is only an estimate: do not reject before actual Directions verification.
		m[0][1].Seconds = 9000
		router := &matrixRouter{matrix: m}
		d := &directionsStub{paths: []routing.Route{directionPath(301.1234, 121.2345)}}
		order := []int64{70, 20}
		baseline, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d)).PrepareBaseline(context.Background(), "minsk", req, order, 70, geometry)
		if err != nil {
			t.Fatal(err)
		}
		if catalog.cityCalls != 1 || catalog.placeCalls != 1 || router.calls != 1 || d.calls != 1 {
			t.Fatal("dependencies not shared")
		}
		checked := baseline.Verified.Variant.Route
		if len(checked.Stops) != 2 || checked.Stops[0].PlaceID != 70 || checked.Stops[1].PlaceID != 20 || !reflect.DeepEqual(baseline.Order, order) {
			t.Fatal("order changed")
		}
		// 422.3579 walking seconds + 1500 visiting seconds, .4+.2 km, 150+100 kopecks.
		if math.Abs(checked.Metrics.TotalMinutes*60-1922.3579) > 1e-7 || math.Abs(checked.Metrics.WalkKM-.6) > 1e-9 || checked.Metrics.CostMinor != 250 {
			t.Fatalf("incorrect fresh baseline %+v", checked.Metrics)
		}
		if checked.Stops[0].Arrival.Location().String() != "Europe/Minsk" || len(d.points[0]) != 3 || d.points[0][1].Lat != catalog.cards[0].Lat || d.points[0][2].Lat != catalog.cards[1].Lat {
			t.Fatal("timezone or ordered path")
		}
		if geometry && len(baseline.Verified.Shapes) != 2 || !geometry && baseline.Verified.Shapes != nil {
			t.Fatal("geometry")
		}
		if baseline.Verified.Matrix.Leg(0, 1).Seconds != 301.1234 || baseline.Matrix.Leg(0, 1).Seconds != 9000 {
			t.Fatal("matrix was mutated")
		}
		order[0] = 999
		if baseline.Order[0] != 70 {
			t.Fatal("baseline aliases order")
		}
	}
}

func TestBaselineRejectsIDsBeforeRouting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		order  []int64
		target int64
		want   error
	}{
		{"inactive or unknown baseline", []int64{888}, 70, routes.ErrPlaceNotFound},
		{"unknown add target", []int64{70, 20}, 888, routes.ErrPlaceNotFound},
		{"duplicate", []int64{70, 70}, 70, nil}, {"empty", nil, 70, nil},
		{"invalid target", []int64{70}, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog, req, m := candidateFixture()
			catalog.cards = catalog.cards[1:]
			router := &matrixRouter{matrix: m}
			d := &directionsStub{}
			_, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d)).PrepareBaseline(context.Background(), "minsk", req, tc.order, tc.target, false)
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error %v", err)
			}
			if router.calls != 0 || d.calls != 0 {
				t.Fatal("invalid IDs incurred routing")
			}
		})
	}
}

func TestBaselineConstraintsAndProviderFailures(t *testing.T) {
	for _, name := range []string{"budget", "tickets", "closed", "unreachable", "NoRoute", "quota", "bad legs", "category", "Matrix"} {
		t.Run(name, func(t *testing.T) {
			catalog, req, m := candidateFixture()
			catalog.cards = catalog.cards[1:]
			d := &directionsStub{paths: []routing.Route{directionPath(301.1234, 121.2345)}}
			router := &matrixRouter{matrix: m}
			want := routes.ErrConstraintsViolation
			switch name {
			case "budget":
				req.BudgetMinutes = 32
			case "tickets":
				cost := int64(249)
				req.MaxTicketCostMinor = &cost
			case "closed":
				catalog.cards[1].OpeningExceptions = catalog.cards[2].OpeningExceptions
			case "unreachable":
				d.paths[0].Legs[1].Reachable = false
			case "NoRoute":
				d.errs = []error{routing.ErrNoRoute}
			case "quota":
				d.errs = []error{routing.ErrRateLimited}
				want = routes.ErrRoutingUnavailable
			case "bad legs":
				d.paths[0].Legs = d.paths[0].Legs[:1]
				want = routes.ErrRoutingUnavailable
			case "category":
				req.Categories = []string{"religious"}
			case "Matrix":
				router.err = routing.ErrRateLimited
				want = routes.ErrRoutingUnavailable
			}
			_, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d)).PrepareBaseline(context.Background(), "minsk", req, []int64{70, 20}, 70, false)
			if !errors.Is(err, want) {
				t.Fatalf("want %v got %v", want, err)
			}
			if name == "category" && (router.calls != 0 || d.calls != 0) {
				t.Fatal("category mismatch billed")
			}
		})
	}
}

func TestBaselineCanceledContext(t *testing.T) {
	catalog, req, m := candidateFixture()
	catalog.cards = catalog.cards[1:]
	router := &matrixRouter{matrix: m}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(&directionsStub{})).PrepareBaseline(ctx, "minsk", req, []int64{70}, 70, false)
	if !errors.Is(err, context.Canceled) || catalog.cityCalls != 0 || router.calls != 0 {
		t.Fatalf("cancellation %v", err)
	}
}

func TestBaselinePreservesReverseOrderAndExactTimeBudget(t *testing.T) {
	catalog, req, m := candidateFixture()
	catalog.cards = catalog.cards[1:]
	router := &matrixRouter{matrix: m}
	d := &directionsStub{paths: []routing.Route{directionPath(360, 240)}}
	baseline, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d)).PrepareBaseline(context.Background(), "minsk", req, []int64{20, 70}, 70, false)
	if err != nil {
		t.Fatal(err)
	}
	checked := baseline.Verified.Variant.Route
	if checked.Stops[0].PlaceID != 20 || checked.Stops[1].PlaceID != 70 || checked.Metrics.TotalMinutes != 35 || d.points[0][1].Lat != catalog.cards[1].Lat {
		t.Fatal("order or exact budget differs")
	}
	d = &directionsStub{paths: []routing.Route{directionPath(360.000001, 240)}}
	_, err = routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d)).PrepareBaseline(context.Background(), "minsk", req, []int64{20, 70}, 70, false)
	if !errors.Is(err, routes.ErrConstraintsViolation) {
		t.Fatalf("small excess hidden: %v", err)
	}
}
