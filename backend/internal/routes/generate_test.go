package routes_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/plannercheck"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

type matrixRouter struct {
	calls  int
	points []routing.Point
	matrix routing.Matrix
	err    error
	cancel context.CancelFunc
}

func (r *matrixRouter) Matrix(ctx context.Context, points []routing.Point) (routing.Matrix, error) {
	r.calls++
	r.points = append([]routing.Point(nil), points...)
	if r.cancel != nil {
		r.cancel()
	}
	return r.matrix, r.err
}

type engineSpy struct {
	calls    int
	optimize bool
	err      error
}

func (e *engineSpy) PlanVariants(req planner.Request, p []planner.Place, m planner.DistanceMatrix, optimize bool) ([]planner.Variant, error) {
	e.calls++
	e.optimize = optimize
	if e.err != nil {
		return nil, e.err
	}
	return planner.New().PlanVariants(req, p, m, optimize)
}
func candidateFixture() (*catalogStub, planner.Request, routing.Matrix) {
	a, b := card(70, "museums"), card(20, "parks")
	a.VisitMinutes = 10
	b.VisitMinutes = 15
	a.PriceKnown = true
	b.PriceKnown = true
	a.Prices = []places.Price{{Kind: "adult", Amount: 1.50, Currency: "BYN"}}
	b.Prices = []places.Price{{Kind: "adult", Amount: 1.00, Currency: "BYN"}}
	a.HoursKnown = true
	b.HoursKnown = true
	a.OpeningHours = []places.Hours{{Weekday: 6, OpenTime: "10:00", CloseTime: "11:00"}}
	b.OpeningHours = append([]places.Hours(nil), a.OpeningHours...)
	closed := card(40, "parks")
	closed.HoursKnown = true
	closed.OpeningExceptions = []places.Exception{{Date: "2026-10-24", Closed: true}}
	unreachable := card(90, "parks")
	catalog := &catalogStub{cities: []places.City{city()}, cards: []places.Place{card(999, "streets"), a, b, closed, unreachable}}
	req := request()
	req.StartAt = time.Date(2026, 10, 24, 7, 0, 0, 0, time.UTC)
	req.BudgetMinutes = 35
	req.Categories = []string{"museums", "parks"}
	cost := int64(250)
	req.MaxTicketCostMinor = &cost
	m := make(routing.Matrix, 5)
	for i := range m {
		m[i] = make([]routing.Leg, 5)
	}
	m[0][1] = routing.Leg{Reachable: true, Seconds: 360, Kilometers: 0.5}
	m[0][2] = routing.Leg{Reachable: true, Seconds: 1200, Kilometers: 2}
	m[0][3] = routing.Leg{Reachable: true}
	m[1][2] = routing.Leg{Reachable: true, Seconds: 240, Kilometers: 0.25}
	m[2][1] = routing.Leg{Reachable: true, Seconds: 900, Kilometers: 1.5}
	return catalog, req, m
}
func TestGenerateCandidatesDirectedMatrixAndConstraints(t *testing.T) {
	catalog, req, m := candidateFixture()
	router := &matrixRouter{matrix: m}
	engine := &engineSpy{}
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithVariantPlanner(engine))
	result, err := service.GenerateCandidates(context.Background(), "minsk", req)
	if err != nil {
		t.Fatal(err)
	}
	if router.calls != 1 || engine.calls != 1 || !engine.optimize || len(router.points) != 5 {
		t.Fatal("one shared matrix and optimized PlanVariants expected")
	}
	for i, p := range result.Prepared.Places {
		if router.points[i+1] != (routing.Point{Lat: p.Location.Lat, Lon: p.Location.Lon}) {
			t.Fatal("routing point order differs")
		}
	}
	if router.points[0] != (routing.Point{Lat: req.Start.Lat, Lon: req.Start.Lon}) {
		t.Fatal("matrix must begin at request start")
	}
	if len(result.Variants) != 1 {
		t.Fatalf("expected one deduplicated variant, got %d", len(result.Variants))
	}
	route := result.Variants[0].Route
	// Independent calculation: 6+4 walking, 10+15 visits = 35 min, .75km,
	// tickets 150+100=250 kopecks. The reverse transition is deliberately longer.
	if len(route.Stops) != 2 || route.Stops[0].PlaceID != 70 || route.Stops[1].PlaceID != 20 || route.Metrics.TotalMinutes != 35 || route.Metrics.WalkKM != 0.75 || route.Metrics.CostMinor != 250 {
		t.Fatalf("unexpected route: %+v", route)
	}
	if err := plannercheck.Audit(result.Prepared.Request, plannercheck.Catalog{Places: result.Prepared.Places}, result.Matrix, route); err != nil {
		t.Fatal(err)
	}
	again, err := service.GenerateCandidates(context.Background(), "minsk", req)
	if err != nil || !reflect.DeepEqual(result.Variants, again.Variants) {
		t.Fatal("fixed matrix and seed must reproduce variants")
	}
}
func TestGenerateCandidatesEmptyAndNoFeasibleRoute(t *testing.T) {
	for _, name := range []string{"empty catalog", "unreachable"} {
		t.Run(name, func(t *testing.T) {
			catalog := &catalogStub{cities: []places.City{city()}}
			router := &matrixRouter{matrix: routing.Matrix{{{}, {}}, {{}, {}}}}
			if name == "unreachable" {
				catalog.cards = []places.Place{card(1, "parks")}
			}
			service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)))
			result, err := service.GenerateCandidates(context.Background(), "minsk", request())
			if err != nil || result.Variants == nil || len(result.Variants) != 0 {
				t.Fatal("empty successful result expected", err)
			}
			expectedCalls := 0
			if name == "unreachable" {
				expectedCalls = 1
			}
			if router.calls != expectedCalls {
				t.Fatal("empty catalog must not call Matrix")
			}
		})
	}
}
func TestGenerateCandidatesErrorsAndCancellation(t *testing.T) {
	catalog, req, m := candidateFixture()
	boom := errors.New("matrix failure")
	router := &matrixRouter{err: boom}
	engine := &engineSpy{}
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithVariantPlanner(engine))
	if _, err := service.GenerateCandidates(context.Background(), "minsk", req); !errors.Is(err, routes.ErrRoutingUnavailable) || !errors.Is(err, boom) || engine.calls != 0 {
		t.Fatal("routing failure must stop planning", err)
	}
	router.err = nil
	router.matrix = m
	engine.err = boom
	if _, err := service.GenerateCandidates(context.Background(), "minsk", req); !errors.Is(err, boom) {
		t.Fatal("Planner error lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router.cancel = cancel
	engine.calls = 0
	if _, err := service.GenerateCandidates(ctx, "minsk", req); !errors.Is(err, context.Canceled) || engine.calls != 0 {
		t.Fatal("cancelled matrix must stop planning", err)
	}
	if _, err := routes.NewService(catalog).GenerateCandidates(context.Background(), "minsk", req); err == nil {
		t.Fatal("missing matrix dependency accepted")
	}
}

func TestGenerateCandidatesPace(t *testing.T) {
	for _, tc := range []struct {
		pace    planner.Pace
		budget  int
		minutes float64
	}{
		{planner.PaceSlow, 40, 37}, {planner.PaceNormal, 35, 35}, {planner.PaceFast, 34, 33.5},
	} {
		t.Run(string(tc.pace), func(t *testing.T) {
			catalog, req, m := candidateFixture()
			req.Pace = tc.pace
			req.BudgetMinutes = tc.budget
			router := &matrixRouter{matrix: m}
			result, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router))).GenerateCandidates(context.Background(), "minsk", req)
			if err != nil || len(result.Variants) != 1 {
				t.Fatal("expected a feasible candidate", err)
			}
			r := result.Variants[0].Route
			if len(r.Stops) != 2 || r.Metrics.TotalMinutes != tc.minutes {
				t.Fatalf("pace duration differs: %+v", r)
			}
			if err := plannercheck.Audit(result.Prepared.Request, plannercheck.Catalog{Places: result.Prepared.Places}, result.Matrix, r); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestGenerateCandidatesRejectsBrokenMatrix(t *testing.T) {
	for _, matrix := range []routing.Matrix{nil, {{{Reachable: true}}}, make(routing.Matrix, 5)} {
		catalog, req, _ := candidateFixture()
		router := &matrixRouter{matrix: matrix}
		engine := &engineSpy{}
		_, err := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithVariantPlanner(engine)).GenerateCandidates(context.Background(), "minsk", req)
		if !errors.Is(err, routes.ErrRoutingUnavailable) || engine.calls != 0 {
			t.Fatal("incomplete matrix must not become an empty successful route", err)
		}
	}
}
