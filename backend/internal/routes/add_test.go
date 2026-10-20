package routes_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

func TestAddMandatoryWithPreservationAndReplacement(t *testing.T) {
	for _, budget := range []int{44, 33, 11} {
		t.Run(string(rune('A'+budget)), func(t *testing.T) {
			catalog, req, router, d := removeFixture()
			// Baseline is always feasible; the new request budget governs both routes.
			baseOrder := []int64{1, 2, 3}
			if budget == 11 {
				baseOrder = []int64{1}
			}
			req.BudgetMinutes = budget
			catalog.cards[0].Importance = 1
			catalog.cards[1].MustSee = true
			service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
			baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, baseOrder, 4, true)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Add(context.Background(), baseline, 4, true)
			if err != nil {
				t.Fatal(err)
			}
			got := idsOf(result)
			wantCount := budget / 11
			if len(got) != wantCount {
				t.Fatalf("%v", got)
			}
			seen := map[int64]bool{}
			survivors := []int64{}
			for _, id := range got {
				if seen[id] {
					t.Fatal("duplicate")
				}
				seen[id] = true
				if id != 4 {
					survivors = append(survivors, id)
				}
			}
			if !seen[4] || result.Variant.Route.Metrics.TotalMinutes != float64(wantCount*11) || len(result.Shapes) != 1 {
				t.Fatal("target or metrics")
			}
			if budget == 44 && !reflect.DeepEqual(survivors, baseOrder) {
				t.Fatal("unnecessary replacements")
			}
			if budget == 33 && (!seen[2] || !seen[3] || seen[1]) {
				t.Fatal("replacement priority")
			}
			if catalog.cityCalls != 1 || catalog.placeCalls != 1 || router.calls != 1 || !reflect.DeepEqual(baseline.Order, baseOrder) {
				t.Fatal("snapshot mutated/reloaded")
			}
			again, err := service.Add(context.Background(), baseline, 4, false)
			if err != nil || !reflect.DeepEqual(idsOf(again), got) || again.Shapes != nil {
				t.Fatal("reproducibility/geometry")
			}
			for _, path := range d.paths[1:] {
				found := false
				for _, point := range path[1:] {
					if point.Lat == catalog.cards[3].Lat {
						found = true
					}
				}
				if !found {
					t.Fatal("trial omitted mandatory target")
				}
			}
		})
	}
}

func TestAddConstraintsAndRoutingFailures(t *testing.T) {
	for _, name := range []string{"visit too long", "ticket limit", "closed", "NoRoute", "quota", "Directions too long", "category", "unknown", "existing"} {
		t.Run(name, func(t *testing.T) {
			catalog, req, router, d := removeFixture()
			want := routes.ErrConstraintsViolation
			switch name {
			case "visit too long":
				catalog.cards[3].VisitMinutes = 34
			case "ticket limit":
				zero := int64(0)
				req.MaxTicketCostMinor = &zero
				catalog.cards[3].Prices = []places.Price{{Kind: "adult", Amount: 1, Currency: "BYN"}}
			case "closed":
				catalog.cards[3].HoursKnown = true
				catalog.cards[3].OpeningExceptions = []places.Exception{{Date: "2026-10-24", Closed: true}}
			case "NoRoute":
				d.replacementError = routing.ErrNoRoute
			case "quota":
				d.replacementError = routing.ErrRateLimited
				want = routes.ErrRoutingUnavailable
			case "Directions too long":
				d.replacementSeconds = 1440
			case "category":
				catalog.cards[3].Categories = []string{"museums"}
			case "unknown":
				want = routes.ErrPlaceNotFound
			}
			service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
			baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1, 2, 3}, 4, false)
			if err != nil {
				t.Fatal(err)
			}
			target := int64(4)
			if name == "unknown" {
				target = 999
			}
			if name == "existing" {
				target = 1
			}
			before := d.calls
			_, err = service.Add(context.Background(), baseline, target, false)
			if name == "existing" {
				if err == nil || d.calls != before {
					t.Fatal("existing target accepted")
				}
				return
			}
			if !errors.Is(err, want) {
				t.Fatalf("want %v got %v", want, err)
			}
			if (name == "visit too long" || name == "ticket limit" || name == "category" || name == "unknown") && d.calls != before {
				t.Fatal("impossible target incurred calls")
			}
		})
	}
}

func TestAddTicketReplacementAndUnknownPrice(t *testing.T) {
	for _, known := range []bool{true, false} {
		catalog, req, router, d := removeFixture()
		limit := int64(100)
		req.MaxTicketCostMinor = &limit
		catalog.cards[0].Prices = []places.Price{{Kind: "adult", Amount: 1, Currency: "BYN"}}
		catalog.cards[0].Importance = 1
		catalog.cards[3].PriceKnown = known
		catalog.cards[3].Prices = []places.Price{{Kind: "adult", Amount: 1, Currency: "BYN"}}
		service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
		baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1, 2}, 4, false)
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Add(context.Background(), baseline, 4, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Variant.Route.Metrics.CostMinor != 100 {
			t.Fatal("ticket totals")
		}
		got := idsOf(result)
		if known && len(got) != 2 || !known && len(got) != 3 {
			t.Fatalf("unexpected replacement %v", got)
		}
		for _, stop := range result.Variant.Route.Stops {
			if stop.PlaceID == 4 && stop.PriceUnconfirmed == known {
				t.Fatal("unknown price flag")
			}
		}
	}
}

func TestAddCanceled(t *testing.T) {
	catalog, req, router, d := removeFixture()
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
	baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1, 2, 3}, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := d.calls
	if _, err := service.Add(ctx, baseline, 4, false); !errors.Is(err, context.Canceled) || d.calls != before {
		t.Fatal("cancellation")
	}
}

type lastTargetDirections struct{ inner *removeDirections }

func (d lastTargetDirections) Route(ctx context.Context, points []routing.Point) (routing.Route, error) {
	result, err := d.inner.Route(ctx, points)
	if err != nil {
		return result, err
	}
	hasTarget := false
	for _, point := range points[1:] {
		if point.Lat == 53.9+float64(4)/1000 {
			hasTarget = true
		}
	}
	if hasTarget && points[len(points)-1].Lat != 53.9+float64(4)/1000 {
		return routing.Route{}, routing.ErrNoRoute
	}
	return result, nil
}
func TestAddTriesOtherPositionsAfterDirectionsRejects(t *testing.T) {
	catalog, req, router, d := removeFixture()
	req.BudgetMinutes = 44
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(lastTargetDirections{inner: d}))
	baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1, 2, 3}, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Add(context.Background(), baseline, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(idsOf(result), []int64{1, 2, 3, 4}) || d.calls != 5 {
		t.Fatalf("positions not tried %v calls %d", idsOf(result), d.calls)
	}
}
func TestAddSmallDirectionsExcessAndOverestimatedMatrix(t *testing.T) {
	for _, excess := range []bool{false, true} {
		catalog, req, router, d := removeFixture()
		req.BudgetMinutes = 11
		service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(router)), routes.WithDirections(d))
		baseline, err := service.PrepareBaseline(context.Background(), "minsk", req, []int64{1}, 4, false)
		if err != nil {
			t.Fatal(err)
		}
		// Matrix cannot veto a feasible mandatory singleton; Directions is final.
		baseline.Matrix.(planner.SliceMatrix)[0][4].Seconds = 9000
		if excess {
			d.replacementSeconds = 60.000001
		}
		result, err := service.Add(context.Background(), baseline, 4, false)
		if excess {
			if !errors.Is(err, routes.ErrConstraintsViolation) {
				t.Fatalf("microsecond excess hidden %v", err)
			}
		} else {
			if err != nil || !reflect.DeepEqual(idsOf(result), []int64{4}) || result.Variant.Route.Metrics.TotalMinutes != 11 {
				t.Fatalf("overestimate rejected %v %v", idsOf(result), err)
			}
		}
	}
}
