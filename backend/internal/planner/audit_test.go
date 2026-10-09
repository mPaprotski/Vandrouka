package planner

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

func auditRequest() Request {
	return Request{BudgetMinutes: 60, StartAt: time.Date(2026, 10, 19, 10, 0, 0, 0, time.FixedZone("Minsk", 10800)), Seed: 42}
}
func auditPlace(id int64) Place {
	return Place{ID: id, Importance: 5, VisitMinutes: 10, PriceKnown: true}
}

func TestBudgetFractionalWalk(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seconds float64
		stops   int
	}{
		{"exact", 600, 1}, {"one microsecond over", 600.000001, 0}, {"fractional fits", 599.25, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := auditRequest()
			req.BudgetMinutes = 20
			m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Seconds: tc.seconds, Reachable: true}}}
			r, err := New().Plan(req, []Place{auditPlace(1)}, m)
			if err != nil || len(r.Stops) != tc.stops {
				t.Fatalf("route=%+v err=%v", r, err)
			}
			if len(r.Stops) > 0 && (r.Stops[0].Departure.Sub(req.StartAt) > 20*time.Minute || math.Abs(r.Metrics.TotalMinutes-r.Stops[0].Departure.Sub(req.StartAt).Minutes()) > 1e-12) {
				t.Fatal("budget or timestamp mismatch")
			}
		})
	}
}

func TestClosedPlaceAfterPreviousStop(t *testing.T) {
	req := auditRequest()
	a, b := auditPlace(1), auditPlace(2)
	b.Schedule = Schedule{Known: true, Weekly: map[time.Weekday][]Interval{time.Monday: {{600, 630}}}}
	// A: arrive 10:10, leave 10:20. B: arrive 10:25, leave 10:35 -> closed.
	// Direct start -> B would arrive 10:00 and fit, so checking from start is wrong.
	b.Importance = 1
	m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Seconds: 600, Reachable: true}, {0, 2}: {Seconds: 0, Reachable: true}, {1, 2}: {Seconds: 300, Reachable: true}}}
	r, err := New().Plan(req, []Place{a, b}, m)
	if err != nil || len(r.Stops) != 1 || r.Stops[0].PlaceID != 1 {
		t.Fatalf("route=%+v err=%v", r, err)
	}
	if !r.Stops[0].Arrival.Equal(req.StartAt.Add(10*time.Minute)) || !r.Stops[0].Departure.Equal(req.StartAt.Add(20*time.Minute)) {
		t.Fatal("incorrect times")
	}
}

func TestArrivalNextLocalDate(t *testing.T) {
	req := auditRequest()
	req.StartAt = time.Date(2026, 10, 19, 23, 55, 0, 0, req.StartAt.Location())
	p := auditPlace(1)
	p.Schedule = Schedule{Known: true, Exceptions: map[string][]Interval{"2026-10-20": {{0, 30}}}}
	m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Seconds: 600, Reachable: true}}}
	r, err := New().Plan(req, []Place{p}, m)
	if err != nil || len(r.Stops) != 1 || r.Stops[0].Arrival.Format("2006-01-02 15:04") != "2026-10-20 00:05" {
		t.Fatalf("route=%+v err=%v", r, err)
	}
	p.Schedule.Exceptions["2026-10-20"] = nil
	r, err = New().Plan(req, []Place{p}, m)
	if err != nil || len(r.Stops) != 0 {
		t.Fatalf("holiday closure ignored: %+v %v", r, err)
	}
}

func TestUnknownCostDoesNotLeakIntoTotals(t *testing.T) {
	req := auditRequest()
	zero := int64(0)
	req.MaxTicketCostMinor = &zero
	a, b := auditPlace(1), auditPlace(2)
	a.PriceKnown = false
	a.TicketCostMinor = 5000
	m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Reachable: true}, {1, 2}: {Reachable: true}}}
	r, err := New().Plan(req, []Place{a, b}, m)
	if err != nil || len(r.Stops) != 2 || r.Metrics.CostMinor != 0 || !r.Stops[0].PriceUnconfirmed {
		t.Fatalf("route=%+v err=%v", r, err)
	}
}

func TestTicketBudgetCumulativeOverrun(t *testing.T) {
	req := auditRequest()
	limit := int64(1499)
	req.MaxTicketCostMinor = &limit
	a, b := auditPlace(1), auditPlace(2)
	a.TicketCostMinor = 1000
	b.TicketCostMinor = 500
	m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Reachable: true}, {1, 2}: {Reachable: true}}}
	r, err := New().Plan(req, []Place{a, b}, m)
	if err != nil || len(r.Stops) != 1 || r.Metrics.CostMinor != 1000 {
		t.Fatalf("route=%+v err=%v", r, err)
	}
}

func TestConflictingDuplicatesFirstRecordWins(t *testing.T) {
	a, b := auditPlace(1), auditPlace(1)
	a.Schedule = Schedule{Known: true}
	b.VisitMinutes = 1
	m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Reachable: true}, {0, 2}: {Reachable: true}}}
	r, err := New().Plan(auditRequest(), []Place{a, b}, m)
	if err != nil || len(r.Stops) != 0 {
		t.Fatalf("duplicate bypassed first record's closed hours: %+v %v", r, err)
	}
}

func TestInvalidPlannerData(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Request, *Place, *routing.Leg)
	}{
		{"pace", func(r *Request, p *Place, l *routing.Leg) { r.Pace = "invalid" }},
		{"coordinate", func(r *Request, p *Place, l *routing.Leg) { r.Start.Lat = math.NaN() }},
		{"visit duration", func(r *Request, p *Place, l *routing.Leg) { p.VisitMinutes = -1 }},
		{"importance", func(r *Request, p *Place, l *routing.Leg) { p.Importance = 0 }},
		{"negative ticket", func(r *Request, p *Place, l *routing.Leg) { p.TicketCostMinor = -1 }},
		{"NaN walk", func(r *Request, p *Place, l *routing.Leg) { l.Seconds = math.NaN() }},
		{"infinite walk", func(r *Request, p *Place, l *routing.Leg) { l.Seconds = math.Inf(1) }},
		{"negative walk", func(r *Request, p *Place, l *routing.Leg) { l.Seconds = -1 }},
		{"negative km", func(r *Request, p *Place, l *routing.Leg) { l.Kilometers = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, p, leg := auditRequest(), auditPlace(1), routing.Leg{Reachable: true}
			tc.change(&req, &p, &leg)
			_, err := New().Plan(req, []Place{p}, mockMatrix{map[[2]int]routing.Leg{{0, 1}: leg}})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestTicketCostOverflow(t *testing.T) {
	a, b := auditPlace(1), auditPlace(2)
	a.TicketCostMinor = math.MaxInt64
	b.TicketCostMinor = 1
	m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Reachable: true}, {1, 2}: {Reachable: true}}}
	if _, err := New().Plan(auditRequest(), []Place{a, b}, m); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestDeterminismIncludesMetrics(t *testing.T) {
	p := []Place{auditPlace(1), auditPlace(2)}
	m := mockMatrix{map[[2]int]routing.Leg{{0, 1}: {Reachable: true}, {0, 2}: {Reachable: true}, {1, 2}: {Reachable: true}, {2, 1}: {Reachable: true}}}
	a, err := New().Plan(auditRequest(), p, m)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		b, err := New().Plan(auditRequest(), p, m)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("non-deterministic: %+v %v", b, err)
		}
	}
}

func TestCatalogMissingAdultBYNPrice(t *testing.T) {
	for _, pricesList := range [][]places.Price{nil, {{Kind: "child", Amount: 5, Currency: "BYN"}}, {{Kind: "adult", Amount: 5, Currency: "USD"}}, {{Kind: "adult", Amount: math.NaN(), Currency: "BYN"}}} {
		p := FromCatalogPlace(places.Place{PriceKnown: true, Prices: pricesList})
		if p.PriceKnown || p.TicketCostMinor != 0 {
			t.Fatalf("invalid adult price marked known: %+v", p)
		}
	}
}
