package routes

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

type Directions interface {
	Route(context.Context, []routing.Point) (routing.Route, error)
}

// VerifiedVariant retains its confirmed matrix for the HTTP stop serializer.
// Shapes is nil when geometry output is disabled; Directions still runs.
type VerifiedVariant struct {
	Variant planner.Variant
	Matrix  planner.DistanceMatrix
	Shapes  []string
}
type Result struct {
	Prepared Prepared
	Variants []VerifiedVariant
}

// Generate returns only routes feasible after Directions. NoRoute and constraint
// failures discard individual candidates; provider outages fail the request.
func (s *Service) Generate(ctx context.Context, citySlug string, request planner.Request, includeGeometry bool) (Result, error) {
	prepared, err := s.Prepare(ctx, citySlug, request)
	if err != nil {
		return Result{}, err
	}
	result := Result{Prepared: prepared, Variants: []VerifiedVariant{}}
	if prepared.Empty() {
		return result, nil
	}
	if s.directions == nil || s.evaluator == nil {
		return Result{}, fmt.Errorf("route verification dependencies are not configured")
	}
	candidates, err := s.generateCandidates(ctx, prepared)
	if err != nil {
		return Result{}, err
	}
	for _, candidate := range candidates.Variants {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if len(candidate.Route.Stops) == 0 {
			continue
		}
		points := []routing.Point{{Lat: candidates.Prepared.Request.Start.Lat, Lon: candidates.Prepared.Request.Start.Lon}}
		order := make([]int64, 0, len(candidate.Route.Stops))
		seen := map[int64]bool{}
		for _, stop := range candidate.Route.Stops {
			index, ok := candidates.Prepared.MatrixIndices[stop.PlaceID]
			if !ok || seen[stop.PlaceID] || index < 1 || index > len(candidates.Prepared.Places) {
				return Result{}, fmt.Errorf("invalid candidate stop")
			}
			seen[stop.PlaceID] = true
			place := candidates.Prepared.Places[index-1]
			points = append(points, routing.Point{Lat: place.Location.Lat, Lon: place.Location.Lon})
			order = append(order, stop.PlaceID)
		}
		path, err := s.directions.Route(ctx, points)
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if errors.Is(err, routing.ErrNoRoute) {
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("%w: Directions: %w", ErrRoutingUnavailable, err)
		}
		if len(path.Legs) != len(order) {
			return Result{}, fmt.Errorf("%w: Directions leg count differs", ErrRoutingUnavailable)
		}
		confirmed := confirmedMatrix{base: candidates.Matrix, legs: map[[2]int]routing.Leg{}}
		from := 0
		for i, id := range order {
			leg := path.Legs[i]
			if math.IsNaN(leg.Seconds) || math.IsInf(leg.Seconds, 0) || leg.Seconds < 0 || math.IsNaN(leg.Kilometers) || math.IsInf(leg.Kilometers, 0) || leg.Kilometers < 0 {
				return Result{}, fmt.Errorf("%w: invalid Directions leg", ErrRoutingUnavailable)
			}
			to := candidates.Prepared.MatrixIndices[id]
			confirmed.legs[[2]int{from, to}] = leg
			from = to
		}
		req := candidates.Prepared.Request
		req.Profile = candidate.Profile
		checked, err := s.evaluator.EvaluateOrder(req, candidates.Prepared.Places, confirmed, order)
		if errors.Is(err, planner.ErrInfeasible) {
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("verify route order: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		candidate.Route = checked
		verified := VerifiedVariant{Variant: candidate, Matrix: confirmed}
		if includeGeometry {
			if len(path.Shapes) == 0 {
				return Result{}, fmt.Errorf("%w: missing Directions geometry", ErrRoutingUnavailable)
			}
			for _, shape := range path.Shapes {
				if shape == "" {
					return Result{}, fmt.Errorf("%w: empty Directions geometry", ErrRoutingUnavailable)
				}
			}
			verified.Shapes = append([]string(nil), path.Shapes...)
		}
		result.Variants = append(result.Variants, verified)
	}
	// Rank and label only the final metrics, keeping each matrix/shape with its route.
	sort.SliceStable(result.Variants, func(i, j int) bool {
		return result.Variants[i].Variant.Route.Metrics.Score > result.Variants[j].Variant.Route.Metrics.Score
	})
	variants := make([]planner.Variant, len(result.Variants))
	for i, v := range result.Variants {
		variants[i] = v.Variant
	}
	variants = planner.AssignLabels(variants)
	for i := range result.Variants {
		result.Variants[i].Variant = variants[i]
	}
	return result, nil
}

type confirmedMatrix struct {
	base planner.DistanceMatrix
	legs map[[2]int]routing.Leg
}

func (m confirmedMatrix) Leg(from, to int) routing.Leg {
	if leg, ok := m.legs[[2]int{from, to}]; ok {
		return leg
	}
	return m.base.Leg(from, to)
}
