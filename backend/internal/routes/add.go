package routes

import (
	"context"
	"errors"
	"fmt"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"sort"

	"github.com/yourname/vandrouka/backend/internal/planner"
)

// Add preserves as many baseline stops as the deterministic greedy search can.
// The target is mandatory in every trial, including the final singleton trial.
// Surviving stops retain their relative order; no budgets are expanded.
func (s *Service) Add(ctx context.Context, baseline Baseline, placeID int64, includeGeometry bool) (VerifiedVariant, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedVariant{}, err
	}
	if s == nil || s.evaluator == nil || s.directions == nil || baseline.Matrix == nil {
		return VerifiedVariant{}, fmt.Errorf("add dependencies are not configured")
	}
	target, ok := baseline.Prepared.Cards[placeID]
	if !ok {
		return VerifiedVariant{}, ErrPlaceNotFound
	}
	for _, id := range baseline.Order {
		if id == placeID {
			return VerifiedVariant{}, fmt.Errorf("added place already belongs to baseline")
		}
	}
	if !matchesCategories(target.Categories, baseline.Prepared.Request.Categories) {
		return VerifiedVariant{}, fmt.Errorf("%w: added place category mismatch", ErrConstraintsViolation)
	}
	req := baseline.Prepared.Request
	req.Profile = planner.ProfileBestMatch
	survivors := append([]int64(nil), baseline.Order...)
	// Matrix ranks positions but is never authoritative for rejecting a trial:
	// actual Directions can be shorter, and opening-hour arrivals can differ.
	for {
		if err := ctx.Err(); err != nil {
			return VerifiedVariant{}, err
		}
		type insertion struct {
			order             []int64
			score, time       float64
			estimatedFeasible bool
		}
		candidates := make([]insertion, 0, len(survivors)+1)
		fitsMinimum := additionMinimumFits(baseline.Prepared, survivors, placeID)
		for position := 0; fitsMinimum && position <= len(survivors); position++ {
			if err := ctx.Err(); err != nil {
				return VerifiedVariant{}, err
			}
			order := make([]int64, 0, len(survivors)+1)
			order = append(order, survivors[:position]...)
			order = append(order, placeID)
			order = append(order, survivors[position:]...)
			requestlog.SetStage(ctx, "planner")
			checked, err := s.evaluator.EvaluateOrder(req, baseline.Prepared.Places, baseline.Verified.Matrix, order)
			if err != nil && !errors.Is(err, planner.ErrInfeasible) {
				return VerifiedVariant{}, fmt.Errorf("evaluate addition: %w", err)
			}
			candidates = append(candidates, insertion{order: order, score: checked.Metrics.Score, time: checked.Metrics.TotalMinutes, estimatedFeasible: err == nil})
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			if a.estimatedFeasible != b.estimatedFeasible {
				return a.estimatedFeasible
			}
			if a.score != b.score {
				return a.score > b.score
			}
			return a.time < b.time
		})
		for _, candidate := range candidates {
			verified, err := s.verifyBaselineOrder(ctx, baseline.Prepared, baseline.Matrix, candidate.order, includeGeometry)
			if errors.Is(err, ErrConstraintsViolation) {
				continue
			}
			if err != nil {
				return VerifiedVariant{}, err
			}
			return verified, nil
		}
		if len(survivors) == 0 {
			return VerifiedVariant{}, fmt.Errorf("%w: mandatory addition cannot fit", ErrConstraintsViolation)
		}
		// Release the least important optional stop first, protecting must-see stops
		// until ordinary stops are exhausted. Stable ties follow baseline order.
		remove := 0
		for i := 1; i < len(survivors); i++ {
			a := baseline.Prepared.Cards[survivors[i]]
			b := baseline.Prepared.Cards[survivors[remove]]
			if a.MustSee != b.MustSee {
				if !a.MustSee {
					remove = i
				}
				continue
			}
			if a.Importance < b.Importance {
				remove = i
			}
		}
		survivors = append(survivors[:remove], survivors[remove+1:]...)
	}
}

// Walking cannot be negative: visit durations and known prices are hard lower
// bounds, so skipping these trials does not rely on Matrix approximations.
func additionMinimumFits(prepared Prepared, survivors []int64, target int64) bool {
	remainingMinutes := prepared.Request.BudgetMinutes
	var remainingCost int64
	if prepared.Request.MaxTicketCostMinor != nil {
		remainingCost = *prepared.Request.MaxTicketCostMinor
	}
	ids := append(append([]int64(nil), survivors...), target)
	for _, id := range ids {
		place := prepared.Places[prepared.MatrixIndices[id]-1]
		if place.VisitMinutes > remainingMinutes {
			return false
		}
		remainingMinutes -= place.VisitMinutes
		if prepared.Request.MaxTicketCostMinor != nil && place.PriceKnown {
			if place.TicketCostMinor > remainingCost {
				return false
			}
			remainingCost -= place.TicketCostMinor
		}
	}
	return true
}
