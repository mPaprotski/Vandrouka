package routes

import (
	"context"
	"errors"
	"fmt"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"sort"

	"github.com/yourname/vandrouka/backend/internal/planner"
)

// Remove uses the verified baseline snapshot without reloading or rebuilding
// Matrix. Surviving stops retain relative order. Refill greedily inserts eligible
// places, checking the entire route through Directions after each insertion.
func (s *Service) Remove(ctx context.Context, baseline Baseline, placeID int64, includeGeometry bool) (VerifiedVariant, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedVariant{}, err
	}
	if s == nil || s.evaluator == nil || s.directions == nil || baseline.Matrix == nil {
		return VerifiedVariant{}, fmt.Errorf("remove dependencies are not configured")
	}
	found := false
	order := make([]int64, 0, len(baseline.Order))
	for _, id := range baseline.Order {
		if id == placeID {
			found = true
		} else {
			order = append(order, id)
		}
	}
	if !found {
		return VerifiedVariant{}, fmt.Errorf("removed place is absent from baseline")
	}
	current, err := s.verifyBaselineOrder(ctx, baseline.Prepared, baseline.Matrix, order, includeGeometry)
	if err != nil {
		return VerifiedVariant{}, err
	}
	req := baseline.Prepared.Request
	req.Profile = planner.ProfileBestMatch
	// At most one insertion per remaining catalog place. Every trial uses the
	// same snapshot, exact evaluator, budgets and full incoming transitions.
	for {
		if err := ctx.Err(); err != nil {
			return VerifiedVariant{}, err
		}
		visited := map[int64]bool{placeID: true}
		for _, id := range order {
			visited[id] = true
		}
		type trial struct {
			order []int64
			route planner.Route
		}
		trials := []trial{}
		for _, place := range baseline.Prepared.Places {
			if visited[place.ID] || !matchesCategories(place.Categories, req.Categories) {
				continue
			}
			var best *trial
			for position := 0; position <= len(order); position++ {
				if err := ctx.Err(); err != nil {
					return VerifiedVariant{}, err
				}
				candidate := make([]int64, 0, len(order)+1)
				candidate = append(candidate, order[:position]...)
				candidate = append(candidate, place.ID)
				candidate = append(candidate, order[position:]...)
				requestlog.SetStage(ctx, "planner")
				checked, err := s.evaluator.EvaluateOrder(req, baseline.Prepared.Places, current.Matrix, candidate)
				if errors.Is(err, planner.ErrInfeasible) {
					continue
				}
				if err != nil {
					return VerifiedVariant{}, fmt.Errorf("evaluate refill: %w", err)
				}
				if best == nil || checked.Metrics.Score > best.route.Metrics.Score || checked.Metrics.Score == best.route.Metrics.Score && checked.Metrics.TotalMinutes < best.route.Metrics.TotalMinutes {
					best = &trial{order: candidate, route: checked}
				}
			}
			if best != nil {
				trials = append(trials, *best)
			}
		}
		sort.SliceStable(trials, func(i, j int) bool {
			if trials[i].route.Metrics.Score != trials[j].route.Metrics.Score {
				return trials[i].route.Metrics.Score > trials[j].route.Metrics.Score
			}
			return trials[i].route.Metrics.TotalMinutes < trials[j].route.Metrics.TotalMinutes
		})
		accepted := false
		for _, candidate := range trials {
			verified, err := s.verifyBaselineOrder(ctx, baseline.Prepared, current.Matrix, candidate.order, includeGeometry)
			if errors.Is(err, ErrConstraintsViolation) {
				continue
			}
			if err != nil {
				return VerifiedVariant{}, err
			}
			current = verified
			order = candidate.order
			accepted = true
			break
		}
		if !accepted {
			return current, nil
		}
	}
}

func matchesCategories(categories, selected []string) bool {
	if len(selected) == 0 {
		return true
	}
	for _, category := range categories {
		for _, want := range selected {
			if category == want {
				return true
			}
		}
	}
	return false
}
