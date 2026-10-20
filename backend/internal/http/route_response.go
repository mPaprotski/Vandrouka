package apihttp

import (
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
)

// BuildGenerateRouteResponse accepts final service results, not Matrix candidates.
// Exact timestamps and confirmed legs drive totals; metrics are checked rather
// than silently emitting inconsistent transport data. No routing calls are made.
func BuildGenerateRouteResponse(result routes.Result) (GenerateRouteResponse, error) {
	return buildRouteResponse(result, false)
}

// allowEmpty applies only to recalculation; generation still excludes empty variants.
func buildRouteResponse(result routes.Result, allowEmpty bool) (GenerateRouteResponse, error) {
	prepared := result.Prepared
	response := GenerateRouteResponse{Seed: prepared.Request.Seed, Timezone: prepared.City.Timezone, Variants: []RouteVariant{}}
	if response.Seed < 0 || response.Seed > MaxRouteSeed || response.Timezone == "" || response.Timezone == "Local" || len(result.Variants) > 3 {
		return response, fmt.Errorf("invalid final route metadata")
	}
	location, err := time.LoadLocation(response.Timezone)
	if err != nil {
		return response, fmt.Errorf("load response timezone: %w", err)
	}
	for i, verified := range result.Variants {
		route := verified.Variant.Route
		if route.Seed != response.Seed || (!allowEmpty && len(route.Stops) == 0) || verified.Matrix == nil {
			return response, fmt.Errorf("invalid final route")
		}
		if len(route.Stops) == 0 && len(verified.Shapes) != 0 {
			return response, fmt.Errorf("empty route has geometry")
		}
		switch verified.Variant.Profile {
		case planner.ProfileBestMatch, planner.ProfileMorePlaces, planner.ProfileLessWalking, planner.ProfileBudget:
		default:
			return response, fmt.Errorf("invalid route profile")
		}
		variant := RouteVariant{VariantID: fmt.Sprintf("variant-%d", i+1), Profile: string(verified.Variant.Profile), Labels: []string{}, Stops: []RouteStop{}, Explanation: RouteExplanation{CategoryCounts: map[string]int{}}}
		if len(result.Variants) == 1 && len(verified.Variant.Labels) != 0 {
			return response, fmt.Errorf("single route has comparative labels")
		}
		seenLabels := map[planner.Label]bool{}
		for _, label := range verified.Variant.Labels {
			if seenLabels[label] {
				return response, fmt.Errorf("duplicate route label")
			}
			seenLabels[label] = true
			switch label {
			case planner.LabelBestMatch, planner.LabelMorePlaces, planner.LabelLessWalking, planner.LabelBudget:
			default:
				return response, fmt.Errorf("invalid route label")
			}
			variant.Labels = append(variant.Labels, string(label))
		}
		seen := map[int64]bool{}
		current := prepared.Request.StartAt
		from := 0
		var walk, visit time.Duration
		var cost int64
		var meters float64
		var unverified int
		for _, stop := range route.Stops {
			index, ok := prepared.MatrixIndices[stop.PlaceID]
			card, hasCard := prepared.Cards[stop.PlaceID]
			if stop.PlaceID <= 0 || !ok || !hasCard || seen[stop.PlaceID] || index < 1 || index > len(prepared.Places) {
				return response, fmt.Errorf("invalid final stop mapping")
			}
			seen[stop.PlaceID] = true
			place := prepared.Places[index-1]
			if place.ID != stop.PlaceID || card.ID != stop.PlaceID {
				return response, fmt.Errorf("stop identity mismatch")
			}
			leg := verified.Matrix.Leg(from, index)
			if !leg.Reachable || !responseNumber(leg.Seconds) || !responseNumber(leg.Kilometers) {
				return response, fmt.Errorf("invalid confirmed transition")
			}
			walkNS := math.Ceil(leg.Seconds * prepared.Request.Pace.WalkMultiplier() * float64(time.Second))
			if walkNS >= float64(math.MaxInt64) {
				return response, fmt.Errorf("transition duration overflow")
			}
			incoming := time.Duration(walkNS)
			if place.VisitMinutes < 1 || place.VisitMinutes > 1440 || !stop.Arrival.Equal(current.Add(incoming)) || !stop.Departure.Equal(stop.Arrival.Add(time.Duration(place.VisitMinutes)*time.Minute)) {
				return response, fmt.Errorf("inconsistent stop timestamps")
			}
			if stop.HoursUnconfirmed != !place.Schedule.Known || stop.PriceUnconfirmed != !place.PriceKnown {
				return response, fmt.Errorf("inconsistent uncertainty flags")
			}
			if place.PriceKnown {
				if place.TicketCostMinor < 0 || place.TicketCostMinor > math.MaxInt64-cost {
					return response, fmt.Errorf("ticket total overflow")
				}
				cost += place.TicketCostMinor
			}
			if stop.HoursUnconfirmed {
				variant.Explanation.HoursUnconfirmedCount++
			}
			if stop.PriceUnconfirmed {
				variant.Explanation.PricesUnconfirmedCount++
			}
			if stop.HoursUnconfirmed || stop.PriceUnconfirmed {
				unverified++
			}
			for _, category := range place.Categories {
				variant.Explanation.CategoryCounts[category]++
			}
			distance := leg.Kilometers * 1000
			if !responseNumber(distance) {
				return response, fmt.Errorf("distance overflow")
			}
			variant.Stops = append(variant.Stops, RouteStop{PlaceID: stop.PlaceID, Name: card.Name, Location: Coordinates{Lat: card.Lat, Lon: card.Lon}, Arrival: stop.Arrival.In(location), Departure: stop.Departure.In(location), VisitSeconds: place.VisitMinutes * 60, IncomingLeg: RouteLeg{WalkSeconds: incoming.Seconds(), DistanceM: distance}, HoursUnconfirmed: stop.HoursUnconfirmed, PriceUnconfirmed: stop.PriceUnconfirmed})
			walk += incoming
			visit += time.Duration(place.VisitMinutes) * time.Minute
			meters += distance
			current = stop.Departure
			from = index
		}
		total := current.Sub(prepared.Request.StartAt)
		if prepared.Request.BudgetMinutes < 1 || prepared.Request.BudgetMinutes > 1440 || total > time.Duration(prepared.Request.BudgetMinutes)*time.Minute || !responseNumber(meters) {
			return response, fmt.Errorf("final route exceeds time/distance constraints")
		}
		if prepared.Request.MaxTicketCostMinor != nil && cost > *prepared.Request.MaxTicketCostMinor {
			return response, fmt.Errorf("final route exceeds ticket budget")
		}
		metrics := route.Metrics
		if !responseNear(metrics.WalkMinutes*60, walk.Seconds()) || metrics.VisitMinutes != int(visit/time.Minute) || !responseNear(metrics.TotalMinutes*60, total.Seconds()) || !responseNear(metrics.WalkKM*1000, meters) || metrics.CostMinor != cost || metrics.UnverifiedCount != unverified || !reflect.DeepEqual(metrics.CategoryCounts, variant.Explanation.CategoryCounts) {
			return response, fmt.Errorf("inconsistent final route totals")
		}
		variant.Totals = RouteTotals{WalkSeconds: walk.Seconds(), VisitSeconds: int(visit / time.Second), TotalSeconds: total.Seconds(), DistanceM: meters, CostMinor: cost, Currency: "BYN", CostComplete: variant.Explanation.PricesUnconfirmedCount == 0, StopsCount: len(route.Stops), UnverifiedStopsCount: unverified}
		variant.Explanation.Text = planner.Explain(route)
		if len(verified.Shapes) > 0 {
			for _, shape := range verified.Shapes {
				if shape == "" {
					return response, fmt.Errorf("empty final geometry")
				}
			}
			variant.Geometry = &RouteGeometry{Format: "polyline6", Segments: append([]string(nil), verified.Shapes...)}
		}
		response.Variants = append(response.Variants, variant)
	}
	return response, nil
}
func responseNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}
func responseNear(a, b float64) bool {
	return responseNumber(a) && responseNumber(b) && math.Abs(a-b) < 1e-7
}
