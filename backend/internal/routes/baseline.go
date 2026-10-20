package routes

import (
	"context"
	"errors"
	"fmt"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"math"

	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

var ErrPlaceNotFound = errors.New("route place not found")
var ErrConstraintsViolation = errors.New("route constraints violated")

// Baseline shares one fresh catalog and Matrix with the subsequent modification.
// Order retains the client's order; Verified contains Directions-confirmed metrics.
type Baseline struct {
	Prepared Prepared
	Matrix   planner.DistanceMatrix
	Order    []int64
	Verified VerifiedVariant
}

// PrepareBaseline validates IDs before any billed routing call. changePlaceID is
// checked here too, so unknown/inactive add targets cannot trigger provider work.
// No totals from the client or previously generated routes are used.
func (s *Service) PrepareBaseline(ctx context.Context, city string, req planner.Request, order []int64, changePlaceID int64, includeGeometry bool) (Baseline, error) {
	if len(order) == 0 || changePlaceID <= 0 {
		return Baseline{}, fmt.Errorf("invalid baseline input")
	}
	seen := map[int64]bool{}
	for _, id := range order {
		if id <= 0 || seen[id] {
			return Baseline{}, fmt.Errorf("invalid baseline order")
		}
		seen[id] = true
	}
	// Load all active cards once to distinguish absent IDs from category mismatch.
	catalogRequest := req
	catalogRequest.Categories = nil
	prepared, err := s.Prepare(ctx, city, catalogRequest)
	if err != nil {
		return Baseline{}, err
	}
	prepared.Request.Categories = append([]string(nil), req.Categories...)
	for _, id := range append(append([]int64(nil), order...), changePlaceID) {
		if _, ok := prepared.Cards[id]; !ok {
			return Baseline{}, ErrPlaceNotFound
		}
	}
	for _, id := range order {
		if len(req.Categories) == 0 {
			continue
		}
		matches := false
		for _, category := range prepared.Cards[id].Categories {
			for _, selected := range req.Categories {
				if category == selected {
					matches = true
				}
			}
		}
		if !matches {
			return Baseline{}, fmt.Errorf("%w: baseline category mismatch", ErrConstraintsViolation)
		}
	}
	if s.matrix == nil || s.directions == nil || s.evaluator == nil {
		return Baseline{}, fmt.Errorf("baseline dependencies are not configured")
	}
	requestlog.SetStage(ctx, "matrix")
	matrix, err := s.matrix.BuildMatrix(ctx, prepared.Request.Start, prepared.Places)
	if ctx.Err() != nil {
		return Baseline{}, ctx.Err()
	}
	if err != nil {
		return Baseline{}, fmt.Errorf("%w: Matrix: %w", ErrRoutingUnavailable, err)
	}
	if matrix == nil {
		return Baseline{}, fmt.Errorf("%w: missing matrix", ErrRoutingUnavailable)
	}
	verified, err := s.verifyBaselineOrder(ctx, prepared, matrix, order, includeGeometry)
	if err != nil {
		return Baseline{}, err
	}
	return Baseline{Prepared: prepared, Matrix: matrix, Order: append([]int64(nil), order...), Verified: verified}, nil
}

func (s *Service) verifyBaselineOrder(ctx context.Context, p Prepared, matrix planner.DistanceMatrix, order []int64, includeGeometry bool) (VerifiedVariant, error) {
	if len(order) == 0 {
		request := p.Request
		request.Profile = planner.ProfileBestMatch
		requestlog.SetStage(ctx, "planner")
		checked, err := s.evaluator.EvaluateOrder(request, p.Places, matrix, nil)
		if err != nil {
			return VerifiedVariant{}, err
		}
		if err := ctx.Err(); err != nil {
			return VerifiedVariant{}, err
		}
		return VerifiedVariant{Variant: planner.Variant{Profile: planner.ProfileBestMatch, Route: checked}, Matrix: matrix}, nil
	}
	points := []routing.Point{{Lat: p.Request.Start.Lat, Lon: p.Request.Start.Lon}}
	for _, id := range order {
		place := p.Places[p.MatrixIndices[id]-1]
		points = append(points, routing.Point{Lat: place.Location.Lat, Lon: place.Location.Lon})
	}
	requestlog.SetStage(ctx, "directions")
	path, err := s.directions.Route(ctx, points)
	if ctx.Err() != nil {
		return VerifiedVariant{}, ctx.Err()
	}
	if errors.Is(err, routing.ErrNoRoute) {
		return VerifiedVariant{}, fmt.Errorf("%w: no baseline walking route", ErrConstraintsViolation)
	}
	if err != nil {
		return VerifiedVariant{}, fmt.Errorf("%w: Directions: %w", ErrRoutingUnavailable, err)
	}
	if len(path.Legs) != len(order) {
		return VerifiedVariant{}, fmt.Errorf("%w: Directions leg count differs", ErrRoutingUnavailable)
	}
	confirmed := confirmedMatrix{base: matrix, legs: map[[2]int]routing.Leg{}}
	from := 0
	for i, id := range order {
		leg := path.Legs[i]
		if math.IsNaN(leg.Seconds) || math.IsInf(leg.Seconds, 0) || leg.Seconds < 0 || math.IsNaN(leg.Kilometers) || math.IsInf(leg.Kilometers, 0) || leg.Kilometers < 0 {
			return VerifiedVariant{}, fmt.Errorf("%w: invalid Directions leg", ErrRoutingUnavailable)
		}
		to := p.MatrixIndices[id]
		confirmed.legs[[2]int{from, to}] = leg
		from = to
	}
	request := p.Request
	request.Profile = planner.ProfileBestMatch
	requestlog.SetStage(ctx, "planner")
	checked, err := s.evaluator.EvaluateOrder(request, p.Places, confirmed, order)
	if errors.Is(err, planner.ErrInfeasible) {
		return VerifiedVariant{}, fmt.Errorf("%w: %w", ErrConstraintsViolation, err)
	}
	if err != nil {
		return VerifiedVariant{}, fmt.Errorf("evaluate baseline: %w", err)
	}
	if ctx.Err() != nil {
		return VerifiedVariant{}, ctx.Err()
	}
	result := VerifiedVariant{Variant: planner.Variant{Profile: planner.ProfileBestMatch, Route: checked}, Matrix: confirmed}
	if includeGeometry {
		if len(path.Shapes) == 0 {
			return VerifiedVariant{}, fmt.Errorf("%w: missing Directions geometry", ErrRoutingUnavailable)
		}
		for _, shape := range path.Shapes {
			if shape == "" {
				return VerifiedVariant{}, fmt.Errorf("%w: empty Directions geometry", ErrRoutingUnavailable)
			}
		}
		result.Shapes = append([]string(nil), path.Shapes...)
	}
	return result, nil
}
