package plannercheck

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

func fixtures(t testing.TB) ([]Scenario, Catalog) {
	t.Helper()
	scenarios, err := LoadScenarios("../../testdata/golden_requests.yaml")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog("../../testdata/golden_catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return scenarios, catalog
}
func TestGoldenRequests(t *testing.T) {
	scenarios, catalog := fixtures(t)
	if len(scenarios) != 10 || len(catalog.Places) != 60 {
		t.Fatal("review acceptance coverage when changing the frozen fixtures")
	}
	data, err := os.ReadFile("../../testdata/golden_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string][]planner.Variant
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected) != len(scenarios) {
		t.Fatal("golden scenario count differs")
	}
	for _, s := range scenarios {
		t.Run(s.Name, func(t *testing.T) {
			req := s.Request()
			matrix := SyntheticMatrix(req, catalog)
			p := planner.New()
			variants, err := p.PlanVariants(req, catalog.Places, matrix, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(variants) == 0 || len(variants) > 3 {
				t.Fatal("expected 1..3 variants")
			}
			for _, v := range variants {
				if err := Audit(req, catalog, matrix, v.Route); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 3; i++ {
				again, err := p.PlanVariants(req, catalog.Places, matrix, true)
				if err != nil || !reflect.DeepEqual(variants, again) {
					t.Fatal("not reproducible")
				}
			}
			if !sameGolden(variants, expected[s.Name]) {
				t.Fatalf("golden changed for %s; inspect report before updating", s.Name)
			}
		})
	}
}
func BenchmarkPlannerGolden(b *testing.B) {
	scenarios, catalog := fixtures(b)
	for _, s := range scenarios {
		b.Run(s.Name, func(b *testing.B) {
			req := s.Request()
			matrix := SyntheticMatrix(req, catalog)
			p := planner.New()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				routes, err := p.PlanVariants(req, catalog.Places, matrix, true)
				if err != nil || len(routes) == 0 {
					b.Fatal(err)
				}
			}
		})
	}
}
func TestAuditIndependentDirectedExample(t *testing.T) {
	req := planner.Request{BudgetMinutes: 35, StartAt: time.Date(2026, 10, 24, 10, 0, 0, 0, time.FixedZone("Minsk", 10800)), Seed: 42}
	a := planner.Place{ID: 1, Categories: []string{"museums"}, Importance: 3, VisitMinutes: 10, PriceKnown: true, TicketCostMinor: 150}
	a.Schedule = planner.Schedule{Known: true, Weekly: map[time.Weekday][]planner.Interval{time.Saturday: {{OpenMinute: 600, CloseMinute: 660}}}}
	z := int64(150)
	req.MaxTicketCostMinor = &z
	b := planner.Place{ID: 2, Categories: []string{"parks"}, Importance: 3, VisitMinutes: 15}
	catalog := Catalog{Places: []planner.Place{a, b}}
	matrix := planner.SliceMatrix{{{Reachable: true}, {Reachable: true, Seconds: 360, Kilometers: 0.5}, {Reachable: true, Seconds: 1200, Kilometers: 2}}, {{Reachable: true}, {Reachable: true}, {Reachable: true, Seconds: 240, Kilometers: 0.25}}, {{Reachable: true}, {Reachable: true, Seconds: 900, Kilometers: 1.5}, {Reachable: true}}}
	// Hand calculation: walk 6+4=10 min, visits 10+15=25, distance .75 km,
	// tickets 150 kopecks; arrivals 10:06/10:20, departure 10:35. No return leg.
	route := planner.Route{Seed: 42, Stops: []planner.Stop{
		{PlaceID: 1, Arrival: req.StartAt.Add(6 * time.Minute), Departure: req.StartAt.Add(16 * time.Minute)},
		{PlaceID: 2, Arrival: req.StartAt.Add(20 * time.Minute), Departure: req.StartAt.Add(35 * time.Minute), HoursUnconfirmed: true, PriceUnconfirmed: true},
	}, Metrics: planner.Metrics{WalkMinutes: 10, VisitMinutes: 25, TotalMinutes: 35, WalkKM: 0.75, CostMinor: 150, UnverifiedCount: 1, CategoryCounts: map[string]int{"museums": 1, "parks": 1}}}
	if err := Audit(req, catalog, matrix, route); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"duplicate", "timestamp", "metrics", "unknown flag", "closed", "budget", "ticket budget", "unreachable", "category"} {
		t.Run(name, func(t *testing.T) {
			bad := route
			bad.Stops = append([]planner.Stop(nil), route.Stops...)
			r := req
			c := catalog
			c.Places = append([]planner.Place(nil), catalog.Places...)
			m := make(planner.SliceMatrix, len(matrix))
			for i := range matrix {
				m[i] = append([]routing.Leg(nil), matrix[i]...)
			}
			switch name {
			case "duplicate":
				bad.Stops[1].PlaceID = 1
			case "timestamp":
				bad.Stops[1].Arrival = bad.Stops[1].Arrival.Add(time.Second)
			case "metrics":
				bad.Metrics.WalkKM = 1
			case "unknown flag":
				bad.Stops[1].PriceUnconfirmed = false
			case "closed":
				c.Places[0].Schedule = planner.Schedule{Known: true, Exceptions: map[string][]planner.Interval{"2026-10-24": nil}}
			case "budget":
				r.BudgetMinutes = 34
			case "ticket budget":
				cost := int64(149)
				r.MaxTicketCostMinor = &cost
			case "unreachable":
				m[1][2].Reachable = false
			case "category":
				r.Categories = []string{"museums"}
			}
			if err := Audit(r, c, m, bad); err == nil {
				t.Fatal("auditor accepted a corrupt route")
			}
		})
	}
}

// Ignore sub-nanosecond floating differences between Go versions; timestamp,
// integer budget and ordering assertions remain exact.
func sameGolden(actual, expected []planner.Variant) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i, a := range actual {
		e := expected[i]
		if !near(a.BeforeWalkMinutes, e.BeforeWalkMinutes) || len(a.Route.Stops) != len(e.Route.Stops) {
			return false
		}
		a.BeforeWalkMinutes = e.BeforeWalkMinutes
		am, em := a.Route.Metrics, e.Route.Metrics
		for _, pair := range [][2]float64{{am.WalkMinutes, em.WalkMinutes}, {am.TotalMinutes, em.TotalMinutes}, {am.WalkKM, em.WalkKM}, {am.Score, em.Score}, {am.InterestValue, em.InterestValue}} {
			if !near(pair[0], pair[1]) {
				return false
			}
		}
		am.WalkMinutes = em.WalkMinutes
		am.TotalMinutes = em.TotalMinutes
		am.WalkKM = em.WalkKM
		am.Score = em.Score
		am.InterestValue = em.InterestValue
		a.Route.Metrics = am
		a.Route.Stops = append([]planner.Stop(nil), a.Route.Stops...)
		for j, stop := range a.Route.Stops {
			want := e.Route.Stops[j]
			if !stop.Arrival.Equal(want.Arrival) || !stop.Departure.Equal(want.Departure) {
				return false
			}
			a.Route.Stops[j].Arrival = want.Arrival
			a.Route.Stops[j].Departure = want.Departure
		}
		if !reflect.DeepEqual(a, e) {
			return false
		}
	}
	return true
}
