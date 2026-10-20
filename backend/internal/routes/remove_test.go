package routes_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

type removeRouter struct{ calls int }

func (r *removeRouter) Matrix(ctx context.Context, points []routing.Point) (routing.Matrix, error) {
	r.calls++
	m := make(routing.Matrix, len(points))
	for i := range m {
		m[i] = make([]routing.Leg, len(points))
		for j := range m[i] {
			m[i][j] = routing.Leg{Reachable: true, Seconds: 60, Kilometers: .1}
		}
	}
	return m, nil
}

type removeDirections struct {
	calls              int
	paths              [][]routing.Point
	replacementError   error
	replacementSeconds float64
}

func (d *removeDirections) Route(ctx context.Context, points []routing.Point) (routing.Route, error) {
	d.calls++
	d.paths = append(d.paths, append([]routing.Point(nil), points...))
	result := routing.Route{Shapes: []string{"confirmed"}}
	for _, point := range points[1:] {
		seconds := float64(60)
		if math.Abs(point.Lat-53.904) < 1e-9 {
			if d.replacementError != nil {
				return routing.Route{}, d.replacementError
			}
			if d.replacementSeconds != 0 {
				seconds = d.replacementSeconds
			}
		}
		result.Legs = append(result.Legs, routing.Leg{Reachable: true, Seconds: seconds, Kilometers: .1})
	}
	return result, nil
}
func removeFixture() (*catalogStub, planner.Request, *removeRouter, *removeDirections) {
	catalog := &catalogStub{cities: []places.City{city()}}
	for id := int64(1); id <= 4; id++ {
		p := card(id, "parks")
		p.Lat = 53.9 + float64(id)/1000
		p.VisitMinutes = 10
		p.PriceKnown = true
		catalog.cards = append(catalog.cards, p)
	}
	req := request()
	req.StartAt = time.Date(2026, 10, 24, 7, 0, 0, 0, time.UTC)
	req.BudgetMinutes = 33
	req.Categories = []string{"parks"}
	return catalog, req, &removeRouter{}, &removeDirections{}
}
func idsOf(verified routes.VerifiedVariant) []int64 {
	ids := []int64{}
	for _, stop := range verified.Variant.Route.Stops {
		ids = append(ids, stop.PlaceID)
	}
	return ids
}

func TestRemoveFirstMiddleLastAndRefill(t *testing.T) {
	for _, removed := range []int64{1, 2, 3} {
		t.Run(string(rune('0'+removed)), func(t *testing.T) {
			catalog, req, router, d := removeFixture()
			service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
			baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1, 2, 3}, removed, true)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Remove(context.Background(), baseline, removed, true)
			if err != nil {
				t.Fatal(err)
			}
			got := idsOf(result)
			if len(got) != 3 {
				t.Fatalf("refill failed: %v", got)
			}
			seen := map[int64]bool{}
			survivors := []int64{}
			for _, id := range got {
				if id == removed || seen[id] {
					t.Fatalf("removed/duplicate ID: %v", got)
				}
				seen[id] = true
				if id != 4 {
					survivors = append(survivors, id)
				}
			}
			expected := []int64{}
			for _, id := range []int64{1, 2, 3} {
				if id != removed {
					expected = append(expected, id)
				}
			}
			if !seen[4] || !reflect.DeepEqual(survivors, expected) {
				t.Fatal("survivor order changed")
			}
			metrics := result.Variant.Route.Metrics
			// Three 60s transitions and three 600s visits: 1980 seconds, .3km, no tickets.
			if metrics.TotalMinutes != 33 || metrics.WalkMinutes != 3 || metrics.VisitMinutes != 30 || metrics.CostMinor != 0 || metrics.WalkKM < .299999999 || metrics.WalkKM > .300000001 {
				t.Fatalf("metrics %+v", metrics)
			}
			if len(result.Shapes) != 1 || catalog.cityCalls != 1 || catalog.placeCalls != 1 || router.calls != 1 {
				t.Fatal("snapshot reloaded")
			}
			if !reflect.DeepEqual(baseline.Order, []int64{1, 2, 3}) || !reflect.DeepEqual(idsOf(baseline.Verified), []int64{1, 2, 3}) {
				t.Fatal("baseline mutated")
			}
			// Removed place is absent from every post-baseline Directions call.
			for _, path := range d.paths[1:] {
				for _, point := range path[1:] {
					if point.Lat == catalog.cards[removed-1].Lat {
						t.Fatal("removed place sent to Directions")
					}
				}
			}
			before := d.calls
			again, err := service.Remove(context.Background(), baseline, removed, false)
			if err != nil || !reflect.DeepEqual(idsOf(again), got) || again.Shapes != nil || d.calls <= before {
				t.Fatal("reproducibility/geometry")
			}
		})
	}
}

func TestRemoveEmptyAndRefillFromEmpty(t *testing.T) {
	for _, refill := range []bool{false, true} {
		catalog, req, router, d := removeFixture()
		if !refill {
			catalog.cards = catalog.cards[:1]
		}
		service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
		baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1}, 1, false)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Remove(context.Background(), baseline, 1, false)
		if err != nil {
			t.Fatal(err)
		}
		if !refill {
			if len(result.Variant.Route.Stops) != 0 || result.Variant.Route.Metrics.TotalMinutes != 0 || result.Shapes != nil || d.calls != 1 {
				t.Fatal("invalid empty result")
			}
		} else {
			if len(idsOf(result)) != 3 {
				t.Fatal("empty route not refilled")
			}
			for _, id := range idsOf(result) {
				if id == 1 {
					t.Fatal("removed returned")
				}
			}
		}
	}
}

func TestRemoveRefillConstraintsAndProviderErrors(t *testing.T) {
	for _, name := range []string{"Directions budget", "NoRoute", "tickets", "category", "closed", "outage"} {
		t.Run(name, func(t *testing.T) {
			catalog, req, router, d := removeFixture()
			switch name {
			case "Directions budget":
				d.replacementSeconds = 1800
			case "NoRoute":
				d.replacementError = routing.ErrNoRoute
			case "tickets":
				zero := int64(0)
				req.MaxTicketCostMinor = &zero
				catalog.cards[3].Prices = []places.Price{{Kind: "adult", Amount: 1, Currency: "BYN"}}
			case "category":
				catalog.cards[3].Categories = []string{"museums"}
			case "closed":
				catalog.cards[3].HoursKnown = true
				catalog.cards[3].OpeningExceptions = []places.Exception{{Date: "2026-10-24", Closed: true}}
			case "outage":
				d.replacementError = routing.ErrRateLimited
			}
			service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
			baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1, 2, 3}, 2, false)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Remove(context.Background(), baseline, 2, false)
			if name == "outage" {
				if !errors.Is(err, routes.ErrRoutingUnavailable) {
					t.Fatalf("outage %v", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(idsOf(result), []int64{1, 3}) || result.Variant.Route.Metrics.TotalMinutes != 22 {
				t.Fatalf("invalid fallback %v %v", idsOf(result), err)
			}
		})
	}
}

func TestRemoveInvalidOperationAndCancellation(t *testing.T) {
	catalog, req, router, d := removeFixture()
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
	baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1, 2, 3}, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	before := d.calls
	if _, err := service.Remove(context.Background(), baseline, 4, false); err == nil || d.calls != before {
		t.Fatal("invalid remove accepted/billed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Remove(ctx, baseline, 2, false); !errors.Is(err, context.Canceled) || d.calls != before {
		t.Fatalf("canceled: %v", err)
	}
}
