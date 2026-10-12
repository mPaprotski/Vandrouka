package planner

import (
	"reflect"
	"testing"
)

func profileFixture() (Request, []Place, mockMatrix) {
	req := auditRequest()
	req.BudgetMinutes = 35
	places := []Place{auditPlace(1), auditPlace(2), auditPlace(3), auditPlace(4)}
	places[0].VisitMinutes = 25
	places[0].Importance = 5
	places[0].TicketCostMinor = 2000
	places[1].VisitMinutes = 10
	places[1].Importance = 1
	places[2].VisitMinutes = 10
	places[2].Importance = 1
	places[3].VisitMinutes = 10
	places[3].Importance = 1
	m := optMatrix(map[[2]int]float64{{0, 1}: 0, {0, 2}: 1, {0, 3}: 2, {0, 4}: 3, {2, 3}: 1, {3, 4}: 1, {4, 2}: 1, {3, 2}: 1, {4, 3}: 1, {2, 4}: 1})
	return req, places, m
}
func TestPreferenceProfiles(t *testing.T) {
	req, places, m := profileFixture()
	req.Profile = ProfileBestMatch
	best, err := New().Plan(req, places, m)
	if err != nil {
		t.Fatal(err)
	}
	req.Profile = ProfileMorePlaces
	more, err := New().Plan(req, places, m)
	if err != nil {
		t.Fatal(err)
	}
	req.Profile = ProfileBudget
	budget, err := New().Plan(req, places, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(best.Stops) != 1 || best.Stops[0].PlaceID != 1 || len(more.Stops) != 3 || budget.Metrics.CostMinor != 0 {
		t.Fatalf("profiles ineffective: best=%+v more=%+v budget=%+v", best, more, budget)
	}
	// Equal value/total time: less_walking prefers the nearby longer visit.
	req.Profile = ProfileLessWalking
	req.BudgetMinutes = 25
	a, b := auditPlace(10), auditPlace(20)
	a.VisitMinutes = 10
	b.VisitMinutes = 20
	got, err := New().Plan(req, []Place{a, b}, optMatrix(map[[2]int]float64{{0, 1}: 10, {0, 2}: 0}))
	if err != nil || len(got.Stops) != 1 || got.Stops[0].PlaceID != 20 {
		t.Fatalf("less_walking failed: %+v %v", got, err)
	}
}
func TestPlanVariantsDeterministicAndFeasible(t *testing.T) {
	req, places, m := profileFixture()
	result, err := New().PlanVariants(req, places, m, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) < 2 || len(result) > 3 {
		t.Fatalf("expected 2-3 distinct variants, got %d", len(result))
	}
	again, err := New().PlanVariants(req, places, m, true)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatal("non-deterministic variants")
	}
	for i, v := range result {
		if v.Route.Metrics.TotalMinutes > float64(req.BudgetMinutes) || v.Route.Seed != req.Seed || v.Route.Metrics.WalkMinutes > v.BeforeWalkMinutes {
			t.Fatal("constraint violated")
		}
		evaluateReq := req
		evaluateReq.Profile = v.Profile
		evaluated, err := New().EvaluateOrder(evaluateReq, places, m, ids(v.Route))
		if err != nil || !reflect.DeepEqual(v.Route.Stops, evaluated.Stops) {
			t.Fatalf("invalid variant: %v", err)
		}
		for j := 0; j < i; j++ {
			if SimilarRoutes(v.Route, result[j].Route) {
				t.Fatal("duplicate variants")
			}
		}
	}
	zero := int64(0)
	req.MaxTicketCostMinor = &zero
	result, err = New().PlanVariants(req, places, m, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range result {
		if v.Route.Metrics.CostMinor != 0 {
			t.Fatal("ticket constraint violated")
		}
	}
	req.Categories = []string{"missing"}
	result, err = New().PlanVariants(req, places, m, true)
	if err != nil || len(result) != 0 {
		t.Fatal("expected empty variants")
	}
}
func TestSimilarRoutesThreshold(t *testing.T) {
	for _, tc := range []struct {
		a, b Route
		want bool
	}{
		{orderedRoute(1, 2, 3), orderedRoute(3, 2, 1), true},
		{orderedRoute(1, 2, 3, 4), orderedRoute(1, 2, 3, 4, 5), false},
		{orderedRoute(1, 2, 3, 4, 5), orderedRoute(1, 2, 3, 4, 5, 6), true},
		{orderedRoute(1), orderedRoute(2), false},
		{Route{}, Route{}, true},
	} {
		if got := SimilarRoutes(tc.a, tc.b); got != tc.want {
			t.Fatalf("overlap=%v want=%v", got, tc.want)
		}
	}
}
