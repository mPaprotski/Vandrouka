package planner

import "sort"

// Variant records the generating profile, not a claim that it is objectively
// best. Labels and comparative explanations belong to the next stage.
type Variant struct {
	Profile           Profile
	Route             Route
	BeforeWalkMinutes float64
	Labels            []Label
}

// PlanVariants generates six candidates from one matrix: four preference
// profiles and two seeded perturbations of best_match. It optimizes each if
// requested, removes empty/highly overlapping sets and returns at most three.
// No network or random global state is used. All returned routes retain req.Seed.
func (p *Planner) PlanVariants(req Request, places []Place, matrix DistanceMatrix, optimize bool) ([]Variant, error) {
	if _, err := validateInputs(req, places, matrix); err != nil {
		return nil, err
	}
	profiles := []Profile{ProfileBestMatch, ProfileMorePlaces, ProfileLessWalking, ProfileBudget, ProfileBestMatch, ProfileBestMatch}
	var pool []Variant
	for i, profile := range profiles {
		candidateReq := req
		candidateReq.Profile = profile
		var variation uint64
		if i >= 4 {
			variation = uint64(i)
		}
		route, err := p.plan(candidateReq, places, matrix, variation)
		if err != nil {
			return nil, err
		}
		if len(route.Stops) == 0 {
			continue
		}
		before := route.Metrics.WalkMinutes
		if optimize {
			route, err = p.Optimize(candidateReq, places, matrix, route)
			if err != nil {
				return nil, err
			}
		}
		pool = append(pool, Variant{Profile: profile, Route: route, BeforeWalkMinutes: before})
	}
	if len(pool) == 0 {
		return nil, nil
	}
	// Rank all candidates by the common configurable score. Stable ordering
	// preserves reproducibility for ties.
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].Route.Metrics.Score > pool[j].Route.Metrics.Score })
	var result []Variant
	for _, candidate := range pool {
		duplicate := false
		for _, selected := range result {
			if SimilarRoutes(candidate.Route, selected.Route) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, candidate)
			if len(result) == 3 {
				break
			}
		}
	}
	return AssignLabels(result), nil
}

// SimilarRoutes uses Jaccard set overlap >80%. Reordering the same stops does
// not create a new variant; exact 80% overlap is retained per the specification.
func SimilarRoutes(a, b Route) bool {
	setA := make(map[int64]bool, len(a.Stops))
	setB := make(map[int64]bool, len(b.Stops))
	for _, stop := range a.Stops {
		setA[stop.PlaceID] = true
	}
	for _, stop := range b.Stops {
		setB[stop.PlaceID] = true
	}
	intersection := 0
	for id := range setA {
		if setB[id] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return true
	}
	return intersection*5 > union*4
}
