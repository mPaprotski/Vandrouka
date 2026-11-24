package planner

import (
	"errors"
	"testing"
)

func TestWalkLimitsAndCycling(t *testing.T) {
	req := auditRequest()
	req.BudgetMinutes = 120
	places := []Place{auditPlace(1), auditPlace(2)}
	matrix := optMatrix(map[[2]int]float64{{0, 1}: 10, {1, 2}: 10, {0, 2}: 10, {2, 1}: 10})
	limit := 1000.0
	count := 1
	req.MaxDistanceM = &limit
	req.MaxStops = &count
	route, err := New().Plan(req, places, matrix)
	if err != nil || len(route.Stops) != 1 || route.Metrics.WalkKM > 1 {
		t.Fatalf("limits not enforced: %+v %v", route, err)
	}
	_, err = New().EvaluateOrder(req, places, matrix, []int64{1, 2})
	if !errors.Is(err, ErrInfeasible) {
		t.Fatalf("expected stop constraint, got %v", err)
	}
	req.MaxStops = nil
	limit = 999
	_, err = New().EvaluateOrder(req, places, matrix, []int64{1})
	if !errors.Is(err, ErrInfeasible) {
		t.Fatalf("expected distance constraint, got %v", err)
	}
	limit = 1000
	req.TravelMode = "cycling"
	req.Pace = PaceSlow
	route, err = New().EvaluateOrder(req, places, matrix, []int64{1})
	if err != nil || route.Metrics.WalkMinutes != 10 {
		t.Fatalf("cycling must use provider time: %+v %v", route, err)
	}
	req.TravelMode = "walking"
	route, err = New().EvaluateOrder(req, places, matrix, []int64{1})
	if err != nil || route.Metrics.WalkMinutes != 12 {
		t.Fatalf("walking pace lost: %+v %v", route, err)
	}
}
