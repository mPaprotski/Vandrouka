package main

import (
	"errors"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routing"
	"strings"
	"testing"
	"time"
)

func TestInvalidOptionsBeforeServices(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		contains string
	}{
		{[]string{"--budget", "0"}, "бюджет времени"},
		{[]string{"--profile", "unknown"}, "неизвестный профиль"},
		{[]string{"--pace", "unknown"}, "неизвестный темп"},
		{[]string{"--ticket-budget", "NaN"}, "бюджет билетов"},
		{[]string{"--ticket-budget", "+Inf"}, "бюджет билетов"},
		{[]string{"--ticket-budget", "-2"}, "бюджет билетов"},
		{[]string{"--start", "NaN,27"}, "координаты"},
	} {
		err := run(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.contains) {
			t.Fatalf("args=%v err=%v", tc.args, err)
		}
	}
}
func TestTokenRequiredBeforeDatabase(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://unused:unused@127.0.0.1:1/unused")
	t.Setenv("MAPBOX_ACCESS_TOKEN", "")
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "MAPBOX_ACCESS_TOKEN") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDirectionsRecheckAppliesPaceOnce(t *testing.T) {
	req := planner.Request{BudgetMinutes: 25, StartAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.FixedZone("Minsk", 10800)), Pace: planner.PaceFast}
	places := []planner.Place{{ID: 1, Importance: 5, VisitMinutes: 10, PriceKnown: true}}
	original := planner.SliceMatrix{{{}, {Seconds: 600, Reachable: true}}, {{}, {}}}
	confirmed := routeMatrix{base: original, legs: map[[2]int]routing.Leg{{0, 1}: {Seconds: 1200, Kilometers: 1, Reachable: true}}}
	// Matrix: 10*0.85+10=18.5 fits. Directions: 20*0.85+10=27 exceeds.
	if _, err := planner.New().EvaluateOrder(req, places, original, []int64{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := planner.New().EvaluateOrder(req, places, confirmed, []int64{1}); !errors.Is(err, planner.ErrInfeasible) {
		t.Fatalf("Directions budget violation ignored: %v", err)
	}
	req.BudgetMinutes = 30
	route, err := planner.New().EvaluateOrder(req, places, confirmed, []int64{1})
	if err != nil || route.Metrics.WalkMinutes != 17 || route.Metrics.TotalMinutes != 27 {
		t.Fatalf("pace applied incorrectly: %+v %v", route, err)
	}
}
