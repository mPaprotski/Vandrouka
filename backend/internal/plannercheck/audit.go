package plannercheck

import (
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/yourname/vandrouka/backend/internal/planner"
)

// Audit independently checks a returned route, without CanVisit/EvaluateOrder.
// It intentionally does not establish optimality or pedestrian accessibility.
func Audit(req planner.Request, catalog Catalog, matrix planner.DistanceMatrix, route planner.Route) error {
	index := map[int64]int{}
	for i, p := range catalog.Places {
		if _, ok := index[p.ID]; !ok {
			index[p.ID] = i
		}
	}
	current := req.StartAt
	from := 0
	seen := map[int64]bool{}
	categories := map[string]int{}
	var walk time.Duration
	var visits, unverified int
	var km float64
	var cost int64
	multiplier := 1.0
	switch req.Pace {
	case planner.PaceSlow:
		multiplier = 1.2
	case planner.PaceFast:
		multiplier = 0.85
	}
	if route.Seed != req.Seed {
		return fmt.Errorf("seed differs")
	}
	for _, stop := range route.Stops {
		i, ok := index[stop.PlaceID]
		if !ok || seen[stop.PlaceID] {
			return fmt.Errorf("unknown or repeated stop %d", stop.PlaceID)
		}
		seen[stop.PlaceID] = true
		p := catalog.Places[i]
		matches := len(req.Categories) == 0
		for _, selected := range req.Categories {
			for _, cat := range p.Categories {
				if selected == cat {
					matches = true
				}
			}
		}
		if !matches {
			return fmt.Errorf("category mismatch")
		}
		leg := matrix.Leg(from, i+1)
		if !leg.Reachable || !finite(leg.Seconds) || !finite(leg.Kilometers) || leg.Seconds < 0 || leg.Kilometers < 0 {
			return fmt.Errorf("invalid or unreachable leg")
		}
		duration := time.Duration(math.Ceil(leg.Seconds * multiplier * float64(time.Second)))
		arrival := current.Add(duration)
		departure := arrival.Add(time.Duration(p.VisitMinutes) * time.Minute)
		if !stop.Arrival.Equal(arrival) || !stop.Departure.Equal(departure) {
			return fmt.Errorf("incorrect arrival/departure at %d", p.ID)
		}
		intervals, exception := p.Schedule.Exceptions[arrival.Format("2006-01-02")]
		if p.Schedule.Known || exception {
			if !exception {
				intervals = p.Schedule.Weekly[arrival.Weekday()]
			}
			allowed := false
			y, m, d := arrival.Date()
			midnight := time.Date(y, m, d, 0, 0, 0, 0, arrival.Location())
			for _, interval := range intervals {
				if interval.OpenMinute < 0 || interval.CloseMinute > 1440 || interval.OpenMinute >= interval.CloseMinute {
					continue
				}
				opening := midnight.Add(time.Duration(interval.OpenMinute) * time.Minute)
				closing := midnight.Add(time.Duration(interval.CloseMinute) * time.Minute)
				if !arrival.Before(opening) && !departure.After(closing) {
					allowed = true
				}
			}
			if !allowed {
				return fmt.Errorf("closed during visit at %d", p.ID)
			}
		}
		if stop.HoursUnconfirmed != !p.Schedule.Known || stop.PriceUnconfirmed != !p.PriceKnown {
			return fmt.Errorf("missing or incorrect uncertainty flag")
		}
		if !p.Schedule.Known || !p.PriceKnown {
			unverified++
		}
		if p.PriceKnown {
			cost += p.TicketCostMinor
		}
		for _, cat := range p.Categories {
			categories[cat]++
		}
		walk += duration
		visits += p.VisitMinutes
		km += leg.Kilometers
		current = departure
		from = i + 1
	}
	if current.Sub(req.StartAt) > time.Duration(req.BudgetMinutes)*time.Minute {
		return fmt.Errorf("time budget exceeded")
	}
	if req.MaxTicketCostMinor != nil && cost > *req.MaxTicketCostMinor {
		return fmt.Errorf("ticket budget exceeded")
	}
	m := route.Metrics
	if !near(m.WalkMinutes, walk.Minutes()) || m.VisitMinutes != visits || !near(m.TotalMinutes, current.Sub(req.StartAt).Minutes()) || !near(m.WalkKM, km) || m.CostMinor != cost || m.UnverifiedCount != unverified || !reflect.DeepEqual(m.CategoryCounts, categories) || !finite(m.Score) || !finite(m.InterestValue) {
		return fmt.Errorf("inconsistent metrics: %+v", m)
	}
	return nil
}
func finite(x float64) bool  { return !math.IsNaN(x) && !math.IsInf(x, 0) }
func near(a, b float64) bool { return finite(a) && finite(b) && math.Abs(a-b) < 1e-8 }
