package apihttp

import (
	"fmt"

	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
)

// BuildRecalculateRouteResponse validates both confirmed routes against the same
// fresh snapshot and builds signed deltas from exact serialized totals.
func BuildRecalculateRouteResponse(baseline routes.Baseline, after routes.VerifiedVariant) (RecalculateRouteResponse, error) {
	var zero RecalculateRouteResponse
	beforeRoute := baseline.Verified.Variant.Route
	if len(baseline.Order) == 0 || len(baseline.Order) != len(beforeRoute.Stops) {
		return zero, fmt.Errorf("invalid recalculation baseline")
	}
	for i, id := range baseline.Order {
		if beforeRoute.Stops[i].PlaceID != id {
			return zero, fmt.Errorf("baseline order differs")
		}
	}
	before, err := buildRouteResponse(routes.Result{Prepared: baseline.Prepared, Variants: []routes.VerifiedVariant{baseline.Verified}}, false)
	if err != nil {
		return zero, fmt.Errorf("serialize baseline: %w", err)
	}
	result, err := buildRouteResponse(routes.Result{Prepared: baseline.Prepared, Variants: []routes.VerifiedVariant{after}}, true)
	if err != nil {
		return zero, fmt.Errorf("serialize recalculation: %w", err)
	}
	oldTotals, newTotals := before.Variants[0].Totals, result.Variants[0].Totals
	diff := planner.DiffRoutes(beforeRoute, after.Variant.Route)
	seconds := newTotals.TotalSeconds - oldTotals.TotalSeconds
	meters := newTotals.DistanceM - oldTotals.DistanceM
	cost := newTotals.CostMinor - oldTotals.CostMinor
	diff.Minutes = seconds / 60
	diff.DistanceKM = meters / 1000
	diff.CostMinor = cost
	diff.PricesConfirmed = oldTotals.CostComplete && newTotals.CostComplete
	names := make(map[int64]string, len(baseline.Prepared.Cards))
	for id, card := range baseline.Prepared.Cards {
		names[id] = card.Name
	}
	return RecalculateRouteResponse{
		Seed: result.Seed, Timezone: result.Timezone, Variant: result.Variants[0],
		Diff: RouteDiff{TotalSecondsDelta: seconds, DistanceMDelta: meters, CostMinorDelta: cost,
			AddedPlaceIDs: append([]int64{}, diff.AddedPlaceIDs...), RemovedPlaceIDs: append([]int64{}, diff.RemovedPlaceIDs...),
			PricesConfirmed: diff.PricesConfirmed, Text: planner.ExplainDiff(diff, names)},
	}, nil
}
