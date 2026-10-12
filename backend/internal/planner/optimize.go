package planner

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInfeasible means an order cannot meet reachability, hours or budget rules.
var ErrInfeasible = errors.New("route order is infeasible")

// MaxOptimizationPasses bounds best-improvement searches. Each pass checks all
// segment reversals (2-opt for an open path) and pair swaps, then accepts at most
// one strictly shorter feasible order. No places are added or removed.
const MaxOptimizationPasses = 10

// EvaluateOrder rebuilds times and metrics for an explicit sequence of place IDs.
// Matrix indices retain their original catalog positions. Arrival times must be
// in the city's timezone, as in Plan. Duplicate IDs in the order are rejected.
func (p *Planner) EvaluateOrder(req Request, places []Place, matrix DistanceMatrix, order []int64) (Route, error) {
	index, err := validateInputs(req, places, matrix)
	if err != nil {
		return Route{}, err
	}
	return p.evaluateOrder(req, places, matrix, index, order)
}

func (p *Planner) evaluateOrder(req Request, places []Place, matrix DistanceMatrix, index map[int64]int, order []int64) (Route, error) {
	route := Route{Seed: req.Seed, Metrics: Metrics{CategoryCounts: make(map[string]int)}}
	current := req.StartAt
	remaining := time.Duration(req.BudgetMinutes) * time.Minute
	from := 0
	visited := make(map[int64]bool, len(order))
	var walkTotal time.Duration
	var valueTotal float64
	for _, id := range order {
		i, ok := index[id]
		if !ok || visited[id] {
			return Route{}, fmt.Errorf("unknown or repeated place ID %d", id)
		}
		visited[id] = true
		place := places[i]
		value := placeValue(req, place)
		if value == 0 {
			return Route{}, fmt.Errorf("place %d does not match categories", id)
		}
		leg := matrix.Leg(from, i+1)
		if !leg.Reachable {
			return Route{}, fmt.Errorf("%w: unreachable place %d", ErrInfeasible, id)
		}
		if math.IsNaN(leg.Seconds) || math.IsInf(leg.Seconds, 0) || leg.Seconds < 0 || math.IsNaN(leg.Kilometers) || math.IsInf(leg.Kilometers, 0) || leg.Kilometers < 0 {
			return Route{}, fmt.Errorf("invalid matrix leg %d -> %d", from, i+1)
		}
		walkNS := math.Ceil(leg.Seconds * req.Pace.WalkMultiplier() * float64(time.Second))
		if walkNS >= float64(math.MaxInt64) {
			return Route{}, fmt.Errorf("%w: walking time overflow", ErrInfeasible)
		}
		walk := time.Duration(walkNS)
		visit := time.Duration(place.VisitMinutes) * time.Minute
		if visit > remaining || walk > remaining-visit {
			return Route{}, fmt.Errorf("%w: time budget at place %d", ErrInfeasible, id)
		}
		var cost int64
		if place.PriceKnown {
			cost = place.TicketCostMinor
		}
		if req.MaxTicketCostMinor != nil && cost > *req.MaxTicketCostMinor-route.Metrics.CostMinor {
			return Route{}, fmt.Errorf("%w: ticket budget", ErrInfeasible)
		}
		if cost > math.MaxInt64-route.Metrics.CostMinor {
			return Route{}, fmt.Errorf("ticket cost overflow")
		}
		arrival := current.Add(walk)
		if !CanVisit(place, arrival) {
			return Route{}, fmt.Errorf("%w: opening hours at place %d", ErrInfeasible, id)
		}
		departure := arrival.Add(visit)
		stop := Stop{PlaceID: id, Arrival: arrival, Departure: departure, HoursUnconfirmed: !place.Schedule.Known, PriceUnconfirmed: !place.PriceKnown}
		route.Stops = append(route.Stops, stop)
		route.Metrics.VisitMinutes += place.VisitMinutes
		route.Metrics.CostMinor += cost
		route.Metrics.WalkKM += leg.Kilometers
		if stop.HoursUnconfirmed || stop.PriceUnconfirmed {
			route.Metrics.UnverifiedCount++
		}
		for _, category := range place.Categories {
			route.Metrics.CategoryCounts[category]++
		}
		valueTotal += value
		walkTotal += walk
		remaining -= walk + visit
		current = departure
		from = i + 1
	}
	route.Metrics.WalkMinutes = walkTotal.Minutes()
	route.Metrics.TotalMinutes = route.Metrics.WalkMinutes + float64(route.Metrics.VisitMinutes)
	route.Metrics.InterestValue = valueTotal
	route.Metrics.Score = p.score(route.Metrics)
	return route, nil
}

func placeValue(req Request, place Place) float64 {
	interested := len(req.Categories) == 0
	nonempty := false
	for _, selected := range req.Categories {
		if selected == "" {
			continue
		}
		nonempty = true
		for _, category := range place.Categories {
			if selected == category {
				interested = true
			}
		}
	}
	if !nonempty {
		interested = true
	}
	if !interested {
		return 0
	}
	bonus := 1.0
	if place.MustSee {
		bonus = 1.5
	}
	return float64(place.Importance) * bonus
}

// Optimize preserves the input set and seed, recalculates the baseline rather
// than trusting its metrics, and performs bounded deterministic local search.
// Bad matrix data returns an error; infeasible alternative orders are skipped.
// The supplied route and catalog are never mutated.
func (p *Planner) Optimize(req Request, places []Place, matrix DistanceMatrix, route Route) (Route, error) {
	index, err := validateInputs(req, places, matrix)
	if err != nil {
		return Route{}, err
	}
	order := make([]int64, len(route.Stops))
	for i, stop := range route.Stops {
		order[i] = stop.PlaceID
	}
	best, err := p.evaluateOrder(req, places, matrix, index, order)
	if err != nil {
		return Route{}, fmt.Errorf("invalid initial route: %w", err)
	}
	for pass := 0; pass < MaxOptimizationPasses; pass++ {
		bestOrder := order
		improved := false
		for move := 0; move < 2; move++ {
			for i := 0; i < len(order)-1; i++ {
				for j := i + 1; j < len(order); j++ {
					trial := append([]int64(nil), order...)
					if move == 0 {
						for a, b := i, j; a < b; a, b = a+1, b-1 {
							trial[a], trial[b] = trial[b], trial[a]
						}
					} else {
						trial[i], trial[j] = trial[j], trial[i]
					}
					candidate, err := p.evaluateOrder(req, places, matrix, index, trial)
					if errors.Is(err, ErrInfeasible) {
						continue
					}
					if err != nil {
						return Route{}, err
					}
					// With the same set of visits, earlier departure means less walking.
					if candidate.Stops[len(candidate.Stops)-1].Departure.Before(best.Stops[len(best.Stops)-1].Departure) {
						best = candidate
						bestOrder = trial
						improved = true
					}
				}
			}
		}
		if !improved {
			break
		}
		order = bestOrder
	}
	return best, nil
}
