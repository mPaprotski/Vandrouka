package planner

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

type mockMatrix struct {
	legs map[[2]int]routing.Leg
}

func (m mockMatrix) Leg(from, to int) routing.Leg {
	if leg, ok := m.legs[[2]int{from, to}]; ok {
		return leg
	}
	return routing.Leg{Reachable: false}
}

func TestBudget(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	// Setup:
	// P1: visit 20 min, walk from start 10 min (600s). At normal pace: 10 + 20 = 30 min.
	// P2: visit 25 min, walk from P1 15 min (900s). At normal pace: 15 + 25 = 40 min.
	// P3: visit 10 min, walk from P2 5 min (300s). At normal pace: 5 + 10 = 15 min.
	placesList := []Place{
		{ID: 1, Name: "P1", Importance: 5, VisitMinutes: 20, Schedule: Schedule{Known: false}},
		{ID: 2, Name: "P2", Importance: 4, VisitMinutes: 25, Schedule: Schedule{Known: false}},
		{ID: 3, Name: "P3", Importance: 3, VisitMinutes: 10, Schedule: Schedule{Known: false}},
	}
	matrix := mockMatrix{
		legs: map[[2]int]routing.Leg{
			{0, 1}: {Seconds: 600, Kilometers: 0.8, Reachable: true},  // 10 min
			{0, 2}: {Seconds: 1800, Kilometers: 2.4, Reachable: true}, // 30 min
			{0, 3}: {Seconds: 2400, Kilometers: 3.2, Reachable: true}, // 40 min
			{1, 2}: {Seconds: 900, Kilometers: 1.2, Reachable: true},  // 15 min
			{2, 3}: {Seconds: 300, Kilometers: 0.4, Reachable: true},  // 5 min
		},
	}

	t.Run("exact budget hit single stop", func(t *testing.T) {
		// Budget exactly 30 min: P1 takes 10 min walk + 20 min visit = 30 min exactly.
		route, err := p.Plan(Request{
			BudgetMinutes: 30,
			StartAt:       startAt,
			Pace:          PaceNormal,
		}, placesList, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(route.Stops) != 1 || route.Stops[0].PlaceID != 1 {
			t.Fatalf("expected 1 stop (P1), got %d stops", len(route.Stops))
		}
		if math.Abs(route.Metrics.TotalMinutes-30.0) > 1e-6 {
			t.Fatalf("expected total minutes exactly 30, got %f", route.Metrics.TotalMinutes)
		}
	})

	t.Run("exceed by small amount is rejected", func(t *testing.T) {
		// Budget 29 min: P1 needs 30 min, cannot fit. Route must be empty.
		route, err := p.Plan(Request{
			BudgetMinutes: 29,
			StartAt:       startAt,
			Pace:          PaceNormal,
		}, placesList, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops for 29 min budget, got %d", len(route.Stops))
		}
	})

	t.Run("multiple stops exact fit", func(t *testing.T) {
		// P1 (10 + 20 = 30) + P2 (15 + 25 = 40) = 70 min.
		route, err := p.Plan(Request{
			BudgetMinutes: 70,
			StartAt:       startAt,
			Pace:          PaceNormal,
		}, placesList, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(route.Stops) != 2 {
			t.Fatalf("expected 2 stops, got %d", len(route.Stops))
		}
		if math.Abs(route.Metrics.TotalMinutes-70.0) > 1e-6 {
			t.Fatalf("expected total minutes exactly 70, got %f", route.Metrics.TotalMinutes)
		}
	})

	t.Run("different paces", func(t *testing.T) {
		// P1 walkSeconds = 600s (10 min normal). Visit = 20 min.
		// Fast pace (0.85): walk 8.5 min + visit 20 min = 28.5 min.
		// Budget 29 min:
		// Fast fits (28.5 <= 29). Normal (30 min) does NOT fit. Slow (12 + 20 = 32 min) does NOT fit.
		rFast, err := p.Plan(Request{
			BudgetMinutes: 29,
			StartAt:       startAt,
			Pace:          PaceFast,
		}, placesList, matrix)
		if err != nil || len(rFast.Stops) != 1 {
			t.Fatalf("fast pace should fit in 29m, err=%v, stops=%d", err, len(rFast.Stops))
		}
		if math.Abs(rFast.Metrics.WalkMinutes-8.5) > 1e-6 {
			t.Fatalf("expected walk minutes 8.5, got %f", rFast.Metrics.WalkMinutes)
		}

		rNormal, err := p.Plan(Request{
			BudgetMinutes: 29,
			StartAt:       startAt,
			Pace:          PaceNormal,
		}, placesList, matrix)
		if err != nil || len(rNormal.Stops) != 0 {
			t.Fatalf("normal pace should not fit in 29m, err=%v, stops=%d", err, len(rNormal.Stops))
		}

		// Slow pace (1.2): walk 12 min + visit 20 min = 32 min.
		// Budget 31 min: Normal fits (30 min), Slow does NOT fit (32 min).
		rSlow, err := p.Plan(Request{
			BudgetMinutes: 31,
			StartAt:       startAt,
			Pace:          PaceSlow,
		}, placesList, matrix)
		if err != nil || len(rSlow.Stops) != 0 {
			t.Fatalf("slow pace should not fit in 31m, err=%v, stops=%d", err, len(rSlow.Stops))
		}
	})
}

func TestClosedPlace(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	// Friday 2026-10-09
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	matrix := mockMatrix{
		legs: map[[2]int]routing.Leg{
			{0, 1}: {Seconds: 600, Kilometers: 0.8, Reachable: true}, // 10 min -> arrival 10:10
		},
	}

	t.Run("closed on arrival", func(t *testing.T) {
		// Opens at 11:00, arrival is 10:10 -> cannot visit
		place := Place{
			ID: 1, Name: "OpensLate", Importance: 5, VisitMinutes: 30,
			Schedule: Schedule{Known: true, Weekly: map[time.Weekday][]Interval{
				time.Friday: {{660, 1080}}, // 11:00-18:00
			}},
		}
		route, err := p.Plan(Request{BudgetMinutes: 120, StartAt: startAt}, []Place{place}, matrix)
		if err != nil || len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops for closed on arrival, got err=%v stops=%d", err, len(route.Stops))
		}
	})

	t.Run("closes during visit", func(t *testing.T) {
		// Opens 10:00-10:30. Arrival is 10:10, visit 30 min -> departure 10:40 > closing 10:30.
		place := Place{
			ID: 1, Name: "ClosesMidVisit", Importance: 5, VisitMinutes: 30,
			Schedule: Schedule{Known: true, Weekly: map[time.Weekday][]Interval{
				time.Friday: {{600, 630}}, // 10:00-10:30
			}},
		}
		route, err := p.Plan(Request{BudgetMinutes: 120, StartAt: startAt}, []Place{place}, matrix)
		if err != nil || len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops for closes during visit, got err=%v stops=%d", err, len(route.Stops))
		}
	})

	t.Run("lunch break crossing", func(t *testing.T) {
		// Intervals: 10:00-10:20 and 10:40-12:00.
		// Arrival 10:10, visit 20 min -> departs 10:30 (crosses 10:20-10:40 lunch break).
		place := Place{
			ID: 1, Name: "BreakCrossing", Importance: 5, VisitMinutes: 20,
			Schedule: Schedule{Known: true, Weekly: map[time.Weekday][]Interval{
				time.Friday: {{600, 620}, {640, 720}},
			}},
		}
		route, err := p.Plan(Request{BudgetMinutes: 120, StartAt: startAt}, []Place{place}, matrix)
		if err != nil || len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops when crossing break, got err=%v stops=%d", err, len(route.Stops))
		}
	})

	t.Run("holiday closure exception", func(t *testing.T) {
		// Weekly Friday is 10:00-18:00, but 2026-10-09 has exception: closed.
		place := Place{
			ID: 1, Name: "HolidayClosed", Importance: 5, VisitMinutes: 30,
			Schedule: Schedule{
				Known:  true,
				Weekly: map[time.Weekday][]Interval{time.Friday: {{600, 1080}}},
				Exceptions: map[string][]Interval{
					"2026-10-09": nil, // closed
				},
			},
		}
		route, err := p.Plan(Request{BudgetMinutes: 120, StartAt: startAt}, []Place{place}, matrix)
		if err != nil || len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops on holiday closure, got err=%v stops=%d", err, len(route.Stops))
		}
	})

	t.Run("date transition crossing midnight", func(t *testing.T) {
		// Start at 23:45. Walk 10 min -> arrival 23:55. Visit 20 min -> departure 00:15 next day.
		lateStart := time.Date(2026, 10, 9, 23, 45, 0, 0, minsk)
		place := Place{
			ID: 1, Name: "NightPlace", Importance: 5, VisitMinutes: 20,
			Schedule: Schedule{Known: true, Weekly: map[time.Weekday][]Interval{
				time.Friday: {{0, 1440}}, // full day Friday
			}},
		}
		route, err := p.Plan(Request{BudgetMinutes: 60, StartAt: lateStart}, []Place{place}, matrix)
		if err != nil || len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops when crossing midnight boundary, got err=%v stops=%d", err, len(route.Stops))
		}
	})
}

func TestTicketBudget(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	// Places:
	// P1: cost 1000 minor (10 BYN), visit 20
	// P2: cost 500 minor (5 BYN), visit 20
	// P3: cost 0 minor (0 BYN, free), visit 20
	// P4: cost unknown (PriceKnown: false), visit 20
	placesList := []Place{
		{ID: 1, Name: "P1_10BYN", Importance: 5, VisitMinutes: 20, PriceKnown: true, TicketCostMinor: 1000, Schedule: Schedule{Known: false}},
		{ID: 2, Name: "P2_5BYN", Importance: 4, VisitMinutes: 20, PriceKnown: true, TicketCostMinor: 500, Schedule: Schedule{Known: false}},
		{ID: 3, Name: "P3_Free", Importance: 3, VisitMinutes: 20, PriceKnown: true, TicketCostMinor: 0, Schedule: Schedule{Known: false}},
		{ID: 4, Name: "P4_Unknown", Importance: 2, VisitMinutes: 20, PriceKnown: false, TicketCostMinor: 0, Schedule: Schedule{Known: false}},
	}
	matrix := mockMatrix{
		legs: map[[2]int]routing.Leg{
			{0, 1}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{0, 2}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{0, 3}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{0, 4}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{1, 2}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{2, 3}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{3, 4}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
		},
	}

	t.Run("zero ticket budget", func(t *testing.T) {
		// Zero budget (0 BYN): only Free (P3) and Unknown price (P4) should be allowed.
		// Paid places (P1, P2) must be skipped.
		zeroCost := int64(0)
		route, err := p.Plan(Request{
			BudgetMinutes:      120,
			StartAt:            startAt,
			MaxTicketCostMinor: &zeroCost,
		}, placesList, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, stop := range route.Stops {
			if stop.PlaceID == 1 || stop.PlaceID == 2 {
				t.Fatalf("paid place %d included with zero ticket budget", stop.PlaceID)
			}
		}
		if route.Metrics.CostMinor != 0 {
			t.Fatalf("expected total cost 0, got %d", route.Metrics.CostMinor)
		}
	})

	t.Run("exact budget limit hit", func(t *testing.T) {
		// Budget exactly 1500 minor (15 BYN).
		// P1 (10 BYN) + P2 (5 BYN) = exactly 15 BYN.
		exactCost := int64(1500)
		route, err := p.Plan(Request{
			BudgetMinutes:      120,
			StartAt:            startAt,
			MaxTicketCostMinor: &exactCost,
		}, placesList, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if route.Metrics.CostMinor != 1500 {
			t.Fatalf("expected cost exactly 1500, got %d", route.Metrics.CostMinor)
		}
	})

	t.Run("no ticket budget limit", func(t *testing.T) {
		// Nil MaxTicketCostMinor: all places can be chosen.
		route, err := p.Plan(Request{
			BudgetMinutes:      180,
			StartAt:            startAt,
			MaxTicketCostMinor: nil,
		}, placesList, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(route.Stops) != 4 {
			t.Fatalf("expected all 4 places without budget limit, got %d", len(route.Stops))
		}
	})

	t.Run("negative ticket budget is rejected", func(t *testing.T) {
		negativeCost := int64(-100)
		_, err := p.Plan(Request{
			BudgetMinutes:      120,
			StartAt:            startAt,
			MaxTicketCostMinor: &negativeCost,
		}, placesList, matrix)
		if err == nil {
			t.Fatal("expected error for negative ticket budget")
		}
	})

	t.Run("unknown price handled with flag", func(t *testing.T) {
		// Unknown price place P4 must have PriceUnconfirmed = true.
		zeroCost := int64(0)
		route, err := p.Plan(Request{
			BudgetMinutes:      60,
			StartAt:            startAt,
			Categories:         []string{}, // all
			MaxTicketCostMinor: &zeroCost,
		}, []Place{placesList[3]}, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(route.Stops) != 1 || !route.Stops[0].PriceUnconfirmed {
			t.Fatalf("expected 1 stop with PriceUnconfirmed=true, got %+v", route.Stops)
		}
	})
}

func TestDuplicates(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	// places slice with duplicate PlaceID: ID 1 appears twice (index 0 and index 1).
	placesList := []Place{
		{ID: 1, Name: "PlaceOne_Original", Importance: 5, VisitMinutes: 15, Schedule: Schedule{Known: false}},
		{ID: 1, Name: "PlaceOne_Duplicate", Importance: 5, VisitMinutes: 15, Schedule: Schedule{Known: false}},
		{ID: 2, Name: "PlaceTwo", Importance: 4, VisitMinutes: 15, Schedule: Schedule{Known: false}},
	}
	matrix := mockMatrix{
		legs: map[[2]int]routing.Leg{
			{0, 1}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{0, 2}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{0, 3}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{1, 2}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{1, 3}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{2, 1}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{2, 3}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{3, 1}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
			{3, 2}: {Seconds: 120, Kilometers: 0.1, Reachable: true},
		},
	}

	route, err := p.Plan(Request{
		BudgetMinutes: 180,
		StartAt:       startAt,
	}, placesList, matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seenIDs := make(map[int64]int)
	for _, stop := range route.Stops {
		seenIDs[stop.PlaceID]++
	}

	for id, count := range seenIDs {
		if count > 1 {
			t.Fatalf("place ID %d appears %d times in route; must appear at most once", id, count)
		}
	}
	if len(route.Stops) != 2 {
		t.Fatalf("expected exactly 2 unique stops (ID 1 and ID 2), got %d", len(route.Stops))
	}
}

func TestDirectedMatrixTransit(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	// Asymmetric directed matrix:
	// Start -> P1 (600s = 10 min, 0.8 km)
	// Start -> P2 (1200s = 20 min, 1.5 km)
	// P1 -> P2 (300s = 5 min, 0.4 km)
	// P2 -> P1 (1800s = 30 min, 2.5 km)
	//
	// P1 importance 5, visit 20 min -> delta = 10 + 20 = 30 min, value = 5, eff = 5/30 = 0.1667
	// P2 importance 5, visit 20 min -> delta = 20 + 20 = 40 min, value = 5, eff = 5/40 = 0.125
	// P1 is chosen first!
	// Next from P1:
	// P1 -> P2 delta = 5 min walk + 20 min visit = 25 min.
	// Total walk time = 10 + 5 = 15 min.
	// Total visit time = 20 + 20 = 40 min.
	// Total time = 55 min.
	// Total walk km = 0.8 + 0.4 = 1.2 km.
	placesList := []Place{
		{ID: 1, Name: "P1", Importance: 5, VisitMinutes: 20, Schedule: Schedule{Known: false}},
		{ID: 2, Name: "P2", Importance: 5, VisitMinutes: 20, Schedule: Schedule{Known: false}},
	}
	matrix := mockMatrix{
		legs: map[[2]int]routing.Leg{
			{0, 1}: {Seconds: 600, Kilometers: 0.8, Reachable: true},
			{0, 2}: {Seconds: 1200, Kilometers: 1.5, Reachable: true},
			{1, 2}: {Seconds: 300, Kilometers: 0.4, Reachable: true},  // P1 -> P2 is short (5 min)
			{2, 1}: {Seconds: 1800, Kilometers: 2.5, Reachable: true}, // P2 -> P1 is long (30 min)
		},
	}

	route, err := p.Plan(Request{
		BudgetMinutes: 60,
		StartAt:       startAt,
		Pace:          PaceNormal,
	}, placesList, matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(route.Stops) != 2 {
		t.Fatalf("expected 2 stops, got %d", len(route.Stops))
	}
	if route.Stops[0].PlaceID != 1 || route.Stops[1].PlaceID != 2 {
		t.Fatalf("expected order P1 -> P2, got %d -> %d", route.Stops[0].PlaceID, route.Stops[1].PlaceID)
	}
	if math.Abs(route.Metrics.WalkMinutes-15.0) > 1e-6 {
		t.Fatalf("expected walk minutes exactly 15, got %f", route.Metrics.WalkMinutes)
	}
	if math.Abs(route.Metrics.WalkKM-1.2) > 1e-6 {
		t.Fatalf("expected walk km exactly 1.2, got %f", route.Metrics.WalkKM)
	}
	if route.Metrics.VisitMinutes != 40 {
		t.Fatalf("expected visit minutes 40, got %d", route.Metrics.VisitMinutes)
	}
	if math.Abs(route.Metrics.TotalMinutes-55.0) > 1e-6 {
		t.Fatalf("expected total minutes 55, got %f", route.Metrics.TotalMinutes)
	}
}

func TestUnreachableAndInvalidInput(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	t.Run("invalid budget", func(t *testing.T) {
		_, err := p.Plan(Request{BudgetMinutes: 0, StartAt: startAt}, nil, mockMatrix{})
		if err == nil {
			t.Fatal("expected error for budget <= 0")
		}
	})

	t.Run("zero start time", func(t *testing.T) {
		_, err := p.Plan(Request{BudgetMinutes: 60, StartAt: time.Time{}}, nil, mockMatrix{})
		if err == nil {
			t.Fatal("expected error for zero start time")
		}
	})

	t.Run("nil distance matrix", func(t *testing.T) {
		_, err := p.Plan(Request{BudgetMinutes: 60, StartAt: startAt}, nil, nil)
		if err == nil {
			t.Fatal("expected error for nil distance matrix")
		}
	})

	t.Run("all places unreachable", func(t *testing.T) {
		placesList := []Place{
			{ID: 1, Name: "P1", Importance: 5, VisitMinutes: 10, Schedule: Schedule{Known: false}},
		}
		matrix := mockMatrix{
			legs: map[[2]int]routing.Leg{
				{0, 1}: {Reachable: false},
			},
		}
		route, err := p.Plan(Request{BudgetMinutes: 60, StartAt: startAt}, placesList, matrix)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops, got %d", len(route.Stops))
		}
	})

	t.Run("empty places list", func(t *testing.T) {
		route, err := p.Plan(Request{BudgetMinutes: 60, StartAt: startAt}, []Place{}, mockMatrix{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(route.Stops) != 0 {
			t.Fatalf("expected 0 stops, got %d", len(route.Stops))
		}
	})
}

func TestPlanner_DeterminismAndTieBreaker(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	// Two identical candidates: same importance, same visit minutes, same distance from start
	placesList := []Place{
		{ID: 10, Name: "A", Importance: 5, VisitMinutes: 20, PriceKnown: true, Schedule: Schedule{Known: false}},
		{ID: 20, Name: "B", Importance: 5, VisitMinutes: 20, PriceKnown: true, Schedule: Schedule{Known: false}},
	}
	matrix := mockMatrix{
		legs: map[[2]int]routing.Leg{
			{0, 1}: {Seconds: 300, Kilometers: 0.3, Reachable: true},
			{0, 2}: {Seconds: 300, Kilometers: 0.3, Reachable: true},
			{1, 2}: {Seconds: 300, Kilometers: 0.3, Reachable: true},
			{2, 1}: {Seconds: 300, Kilometers: 0.3, Reachable: true},
		},
	}

	req1 := Request{BudgetMinutes: 60, StartAt: startAt, Seed: 42}
	r1, err := p.Plan(req1, placesList, matrix)
	if err != nil {
		t.Fatal(err)
	}

	req2 := Request{BudgetMinutes: 60, StartAt: startAt, Seed: 42}
	r2, err := p.Plan(req2, placesList, matrix)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(r1.Stops, r2.Stops) {
		t.Fatalf("runs with same seed must produce identical stops: %v vs %v", r1.Stops, r2.Stops)
	}
}

func TestPlanner_MetricsMatchStops(t *testing.T) {
	p := New()
	minsk := time.FixedZone("Europe/Minsk", 3*60*60)
	startAt := time.Date(2026, 10, 9, 10, 0, 0, 0, minsk)

	placesList := []Place{
		{ID: 1, Name: "P1", Categories: []string{"catA"}, Importance: 4, VisitMinutes: 20, PriceKnown: true, TicketCostMinor: 1000, Schedule: Schedule{Known: true, Weekly: map[time.Weekday][]Interval{time.Friday: {{0, 1440}}}}},
		{ID: 2, Name: "P2", Categories: []string{"catA", "catB"}, Importance: 5, MustSee: true, VisitMinutes: 30, PriceKnown: false, TicketCostMinor: 0, Schedule: Schedule{Known: false}},
	}

	matrix := mockMatrix{
		legs: map[[2]int]routing.Leg{
			{0, 1}: {Seconds: 600, Kilometers: 0.7, Reachable: true},
			{0, 2}: {Seconds: 600, Kilometers: 0.8, Reachable: true},
			{1, 2}: {Seconds: 300, Kilometers: 0.4, Reachable: true},
			{2, 1}: {Seconds: 300, Kilometers: 0.4, Reachable: true},
		},
	}

	route, err := p.Plan(Request{
		BudgetMinutes: 180,
		StartAt:       startAt,
		Pace:          PaceNormal,
	}, placesList, matrix)
	if err != nil {
		t.Fatal(err)
	}

	// Verify metrics match stops
	var expectedVisitMin int
	var expectedCostMinor int64
	var expectedUnverified int
	expectedCatCounts := make(map[string]int)

	placesMap := map[int64]Place{1: placesList[0], 2: placesList[1]}

	for _, stop := range route.Stops {
		pl := placesMap[stop.PlaceID]
		expectedVisitMin += pl.VisitMinutes
		expectedCostMinor += pl.TicketCostMinor
		if stop.HoursUnconfirmed || stop.PriceUnconfirmed {
			expectedUnverified++
		}
		for _, cat := range pl.Categories {
			expectedCatCounts[cat]++
		}
	}

	if route.Metrics.VisitMinutes != expectedVisitMin {
		t.Errorf("visit minutes mismatch: got %d, want %d", route.Metrics.VisitMinutes, expectedVisitMin)
	}
	if route.Metrics.CostMinor != expectedCostMinor {
		t.Errorf("cost mismatch: got %d, want %d", route.Metrics.CostMinor, expectedCostMinor)
	}
	if route.Metrics.UnverifiedCount != expectedUnverified {
		t.Errorf("unverified mismatch: got %d, want %d", route.Metrics.UnverifiedCount, expectedUnverified)
	}
	if !reflect.DeepEqual(route.Metrics.CategoryCounts, expectedCatCounts) {
		t.Errorf("cat counts mismatch: got %v, want %v", route.Metrics.CategoryCounts, expectedCatCounts)
	}
	if route.Metrics.TotalMinutes != route.Metrics.WalkMinutes+float64(route.Metrics.VisitMinutes) {
		t.Errorf("total minutes mismatch: got %f, want walk+visit", route.Metrics.TotalMinutes)
	}
}

func TestFromCatalogPlace(t *testing.T) {
	catPlace := places.Place{
		ID:           42,
		Name:         "Тест",
		Lat:          53.9,
		Lon:          27.5,
		Importance:   5,
		MustSee:      true,
		VisitMinutes: 45,
		HoursKnown:   true,
		PriceKnown:   true,
		Categories:   []string{"museums"},
		OpeningHours: []places.Hours{
			{Weekday: 5, OpenTime: "11:00", CloseTime: "19:00"},
		},
		OpeningExceptions: []places.Exception{
			{Date: "2026-10-10", Closed: true},
		},
		Prices: []places.Price{
			{Kind: "adult", Amount: 15.50, Currency: "BYN"},
		},
	}

	p := FromCatalogPlace(catPlace)
	if p.ID != 42 || p.Name != "Тест" || p.Importance != 5 || !p.MustSee || p.VisitMinutes != 45 {
		t.Fatalf("scalar fields mismatch: %+v", p)
	}
	if p.TicketCostMinor != 1550 {
		t.Fatalf("ticket cost mismatch: got %d, want 1550", p.TicketCostMinor)
	}
	if !p.Schedule.Known {
		t.Fatal("schedule must be known")
	}
	if len(p.Schedule.Weekly[time.Friday]) != 1 {
		t.Fatalf("expected 1 weekly interval, got %d", len(p.Schedule.Weekly[time.Friday]))
	}
	interval := p.Schedule.Weekly[time.Friday][0]
	if interval.OpenMinute != 660 || interval.CloseMinute != 1140 {
		t.Fatalf("expected 660-1140, got %d-%d", interval.OpenMinute, interval.CloseMinute)
	}
	if _, ok := p.Schedule.Exceptions["2026-10-10"]; !ok {
		t.Fatal("expected exception for 2026-10-10")
	}
}
