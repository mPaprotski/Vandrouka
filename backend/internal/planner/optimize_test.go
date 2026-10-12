package planner

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/routing"
)

func orderedRoute(ids ...int64) Route {
	r := Route{Seed: 42}
	for _, id := range ids {
		r.Stops = append(r.Stops, Stop{PlaceID: id})
	}
	return r
}
func optMatrix(edges map[[2]int]float64) mockMatrix {
	m := mockMatrix{legs: make(map[[2]int]routing.Leg)}
	for edge, minutes := range edges {
		m.legs[edge] = routing.Leg{Seconds: minutes * 60, Kilometers: minutes / 10, Reachable: true}
	}
	return m
}
func ids(r Route) []int64 {
	result := make([]int64, len(r.Stops))
	for i, s := range r.Stops {
		result[i] = s.PlaceID
	}
	return result
}

func TestOptimizeTwoOptDirected(t *testing.T) {
	req := auditRequest()
	req.BudgetMinutes = 120
	places := []Place{auditPlace(1), auditPlace(2), auditPlace(3)}
	places[0].TicketCostMinor = 100
	places[1].TicketCostMinor = 200
	places[2].TicketCostMinor = 300
	for i := range places {
		places[i].Categories = []string{"architecture"}
	}
	// Baseline 1->2->3: 10+20+20=50 walking +30 visit=80.
	// Reverse segment [2,3]: 1->3->2: 10+5+5=20 walking +30 visit=50.
	// Return edges and all other starts are unreachable.
	m := optMatrix(map[[2]int]float64{{0, 1}: 10, {1, 2}: 20, {2, 3}: 20, {1, 3}: 5, {3, 2}: 5})
	baseline, err := New().EvaluateOrder(req, places, m, []int64{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	saved := append([]Stop(nil), baseline.Stops...)
	got, err := New().Optimize(req, places, m, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(got), []int64{1, 3, 2}) {
		t.Fatalf("unexpected order: %v", ids(got))
	}
	if got.Metrics.WalkMinutes != 20 || got.Metrics.TotalMinutes != 50 || got.Metrics.VisitMinutes != 30 || got.Metrics.WalkKM != 2 || got.Metrics.CostMinor != 600 || got.Metrics.CategoryCounts["architecture"] != 3 || got.Seed != 42 {
		t.Fatalf("incorrect metrics: %+v", got)
	}
	for i, minutes := range []int{10, 25, 40} {
		if !got.Stops[i].Arrival.Equal(req.StartAt.Add(time.Duration(minutes)*time.Minute)) || !got.Stops[i].Departure.Equal(req.StartAt.Add(time.Duration(minutes+10)*time.Minute)) {
			t.Fatalf("incorrect stop %d: %+v", i, got.Stops[i])
		}
	}
	if !reflect.DeepEqual(baseline.Stops, saved) {
		t.Fatal("mutated baseline")
	}
	again, err := New().Optimize(req, places, m, baseline)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("non-deterministic result")
	}
	already, err := New().Optimize(req, places, m, got)
	if err != nil || !reflect.DeepEqual(got, already) {
		t.Fatal("already optimal route changed")
	}
}

func TestOptimizeSwap(t *testing.T) {
	req := auditRequest()
	req.BudgetMinutes = 120
	places := []Place{auditPlace(1), auditPlace(2), auditPlace(3), auditPlace(4)}
	// Only nonadjacent endpoint swap is feasible. No segment reversal improves.
	m := optMatrix(map[[2]int]float64{{0, 1}: 10, {1, 2}: 10, {2, 3}: 10, {3, 4}: 10, {0, 4}: 1, {4, 2}: 1, {3, 1}: 1})
	got, err := New().Optimize(req, places, m, orderedRoute(1, 2, 3, 4))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(got), []int64{4, 2, 3, 1}) || got.Metrics.WalkMinutes != 13 {
		t.Fatalf("swap failed: %+v", got)
	}
}

func TestOptimizeRejectsShorterClosedOrder(t *testing.T) {
	req := auditRequest()
	req.BudgetMinutes = 120
	places := []Place{auditPlace(1), auditPlace(2), auditPlace(3)}
	places[2].Schedule = Schedule{Known: true, Weekly: map[time.Weekday][]Interval{time.Monday: {{660, 720}}}}
	m := optMatrix(map[[2]int]float64{{0, 1}: 10, {1, 2}: 20, {2, 3}: 20, {1, 3}: 5, {3, 2}: 5})
	// Baseline C arrives 11:10; shortcut C would arrive 10:25 before opening.
	got, err := New().Optimize(req, places, m, orderedRoute(1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(got), []int64{1, 2, 3}) || got.Metrics.WalkMinutes != 50 {
		t.Fatalf("closed shortcut accepted: %+v", got)
	}
}

func TestOptimizeDoesNotAssumeSymmetricEdges(t *testing.T) {
	req := auditRequest()
	places := []Place{auditPlace(1), auditPlace(2), auditPlace(3)}
	// No reverse edges exist. A symmetric 2-opt shortcut calculation is invalid.
	m := optMatrix(map[[2]int]float64{{0, 1}: 1, {1, 2}: 2, {2, 3}: 3, {0, 3}: 0, {1, 3}: 0})
	got, err := New().Optimize(req, places, m, orderedRoute(1, 2, 3))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(got), []int64{1, 2, 3}) || got.Metrics.WalkMinutes != 6 {
		t.Fatalf("unreachable reverse path accepted: %+v", got)
	}
}

func TestEvaluateOrderConstraints(t *testing.T) {
	req := auditRequest()
	places := []Place{auditPlace(1), auditPlace(2)}
	places[0].TicketCostMinor = 100
	places[1].TicketCostMinor = 200
	m := optMatrix(map[[2]int]float64{{0, 1}: 5, {1, 2}: 5})
	for _, tc := range []struct {
		name       string
		request    Request
		order      []int64
		infeasible bool
	}{
		{"time", Request{BudgetMinutes: 29, StartAt: req.StartAt}, []int64{1, 2}, true},
		{"unknown ID", req, []int64{99}, false},
		{"duplicate", req, []int64{1, 1}, false},
		{"unreachable", req, []int64{2, 1}, true},
		{"ticket limit", Request{BudgetMinutes: 60, StartAt: req.StartAt, MaxTicketCostMinor: minorPointer(299)}, []int64{1, 2}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().EvaluateOrder(tc.request, places, m, tc.order)
			if err == nil || errors.Is(err, ErrInfeasible) != tc.infeasible {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}
func minorPointer(n int64) *int64 { return &n }

func TestOptimizeEmptyAndSingle(t *testing.T) {
	req := auditRequest()
	places := []Place{auditPlace(1)}
	m := optMatrix(map[[2]int]float64{{0, 1}: 5})
	for _, order := range [][]int64{nil, {1}} {
		expected, err := New().EvaluateOrder(req, places, m, order)
		if err != nil {
			t.Fatal(err)
		}
		got, err := New().Optimize(req, places, m, expected)
		if err != nil || !reflect.DeepEqual(expected, got) {
			t.Fatalf("unexpected change: %+v %v", got, err)
		}
	}
}

func TestEvaluateOrderMatchesGreedy(t *testing.T) {
	for _, pace := range []Pace{PaceSlow, PaceNormal, PaceFast} {
		req := auditRequest()
		req.Pace = pace
		places := []Place{auditPlace(1), auditPlace(2)}
		places[0].PriceKnown = false
		places[0].TicketCostMinor = 500
		places[1].MustSee = true
		m := optMatrix(map[[2]int]float64{{0, 1}: 5, {0, 2}: 15, {1, 2}: 10, {2, 1}: 10})
		greedy, err := New().Plan(req, places, m)
		if err != nil {
			t.Fatal(err)
		}
		evaluated, err := New().EvaluateOrder(req, places, m, ids(greedy))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(greedy, evaluated) {
			t.Fatalf("pace %s: greedy=%+v evaluated=%+v", pace, greedy, evaluated)
		}
	}
}
