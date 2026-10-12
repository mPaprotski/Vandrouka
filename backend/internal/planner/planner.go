package planner

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"time"
)

// Planner implements the greedy route planning algorithm.
type Planner struct{ weights ScoringWeights }

func New() *Planner {
	return &Planner{weights: DefaultScoringWeights()}
}

// candidate holds information about a potential next place to visit.
type candidate struct {
	placeIndex    int // 0-based index in the input places slice (matrix index = 1 + placeIndex)
	place         Place
	costMinor     int64         // effective known ticket cost
	scaledWalkMin float64       // scaled walk time in minutes
	duration      time.Duration // walk plus visit
	value         float64
	efficiency    float64
	legKM         float64
	arrival       time.Time
	departure     time.Time
	hoursUnconf   bool
	priceUnconf   bool
}

// Plan builds a single greedy route meeting all constraints.
// Matrix indexing convention:
// - Index 0 is the starting location.
// - Index 1 + i corresponds to places[i].
func (p *Planner) Plan(req Request, places []Place, matrix DistanceMatrix) (Route, error) {
	return p.plan(req, places, matrix, 0)
}

func (p *Planner) plan(req Request, places []Place, matrix DistanceMatrix, variation uint64) (Route, error) {
	firstIndex, err := validateInputs(req, places, matrix)
	if err != nil {
		return Route{}, err
	}

	paceMult := req.Pace.WalkMultiplier()

	// Build map of selected categories for O(1) lookup.
	selectedCats := make(map[string]bool, len(req.Categories))
	for _, c := range req.Categories {
		if c != "" {
			selectedCats[c] = true
		}
	}
	allCategories := len(selectedCats) == 0

	// Pseudo-random generator seeded for deterministic tie-breaking.
	var rng *rand.Rand
	if req.Seed != 0 {
		rng = rand.New(rand.NewPCG(uint64(req.Seed), uint64(req.Seed)^0x5851f42d4c957f2d))
	} else {
		rng = rand.New(rand.NewPCG(1, 2))
	}

	if variation != 0 {
		rng = rand.New(rand.NewPCG(uint64(req.Seed)^variation, variation+0x5851f42d4c957f2d))
	}

	// Track visited places by both slice index and unique PlaceID.
	// In case the input places slice contains duplicate PlaceIDs (e.g. repeated records),
	// visiting any instance marks that PlaceID as visited so it will never be included twice.
	visitedIdx := make([]bool, len(places))
	visitedPlaceIDs := make(map[int64]bool, len(places))
	currentLocIndex := 0 // starts at 0 (start point)
	currentTime := req.StartAt
	remainingBudget := time.Duration(req.BudgetMinutes) * time.Minute
	var accumulatedCostMinor int64

	var stops []Stop
	var totalWalkMin float64
	var totalVisitMin int
	var totalWalkKM float64
	var totalValue float64
	var unverifiedCount int
	categoryCounts := make(map[string]int)

	for {
		var candidates []candidate

		for i, place := range places {
			if firstIndex[place.ID] != i || visitedIdx[i] || visitedPlaceIDs[place.ID] {
				continue
			}

			// 1. Category interest weight.
			// value = importance * interest_weight * (1 + must_see_bonus)
			// For selected categories interest_weight = 1, otherwise = 0;
			// with empty categories list all places are considered (weight = 1).
			var interestWeight float64
			if allCategories {
				interestWeight = 1.0
			} else {
				hasCat := false
				for _, c := range place.Categories {
					if selectedCats[c] {
						hasCat = true
						break
					}
				}
				if hasCat {
					interestWeight = 1.0
				} else {
					interestWeight = 0.0
				}
			}

			if interestWeight <= 0 {
				continue
			}

			var mustSeeBonus float64
			if place.MustSee {
				mustSeeBonus = 0.5
			}
			value := float64(place.Importance) * interestWeight * (1.0 + mustSeeBonus)

			// 2. Reachability from current location.
			targetLocIndex := 1 + i
			leg := matrix.Leg(currentLocIndex, targetLocIndex)
			if !leg.Reachable {
				continue
			}

			if math.IsNaN(leg.Seconds) || math.IsInf(leg.Seconds, 0) || leg.Seconds < 0 || math.IsNaN(leg.Kilometers) || math.IsInf(leg.Kilometers, 0) || leg.Kilometers < 0 {
				return Route{}, fmt.Errorf("invalid matrix leg %d -> %d", currentLocIndex, targetLocIndex)
			}
			// Shared duration for budget, timestamps, and metrics; round upward.
			walkNS := math.Ceil(leg.Seconds * paceMult * float64(time.Second))
			if walkNS >= float64(math.MaxInt64) {
				continue
			}
			walkDuration := time.Duration(walkNS)
			visitDuration := time.Duration(place.VisitMinutes) * time.Minute
			if visitDuration > remainingBudget || walkDuration > remainingBudget-visitDuration {
				continue
			}
			duration := walkDuration + visitDuration
			scaledWalkMin := walkDuration.Minutes()
			deltaTime := duration.Minutes()

			// 4. Ticket budget constraint.
			var placeCostMinor int64
			var priceUnconf bool
			if place.PriceKnown {
				placeCostMinor = place.TicketCostMinor
			} else {
				// Unknown price is treated as 0 cost for budget limit check,
				// but flagged as unconfirmed.
				placeCostMinor = 0
				priceUnconf = true
			}
			if placeCostMinor > math.MaxInt64-accumulatedCostMinor {
				return Route{}, fmt.Errorf("ticket cost overflow")
			}
			if req.MaxTicketCostMinor != nil && placeCostMinor > *req.MaxTicketCostMinor-accumulatedCostMinor {
				continue
			}

			// 5. Schedule check via CanVisit.
			// Compute walk duration with nanosecond accuracy from scaledWalkMin to avoid rounding drift.
			arrival := currentTime.Add(walkDuration)
			if !CanVisit(place, arrival) {
				continue
			}

			departure := arrival.Add(time.Duration(place.VisitMinutes) * time.Minute)
			hoursUnconf := !place.Schedule.Known

			efficiency := value / deltaTime
			switch req.Profile {
			case ProfileMorePlaces:
				efficiency = 1 / deltaTime
			case ProfileLessWalking:
				efficiency = value / (float64(place.VisitMinutes) + 4*scaledWalkMin)
			case ProfileBudget:
				penalty := 1 + float64(placeCostMinor)/100
				if priceUnconf {
					penalty += 2
				}
				efficiency = value / (deltaTime * penalty)
			}
			if variation != 0 {
				efficiency *= 0.9 + rng.Float64()*0.2
			}

			candidates = append(candidates, candidate{
				placeIndex:    i,
				place:         place,
				costMinor:     placeCostMinor,
				scaledWalkMin: scaledWalkMin,
				duration:      duration,
				value:         value,
				efficiency:    efficiency,
				legKM:         leg.Kilometers,
				arrival:       arrival,
				departure:     departure,
				hoursUnconf:   hoursUnconf,
				priceUnconf:   priceUnconf,
			})
		}

		if len(candidates) == 0 {
			break
		}

		// Sort candidates by efficiency descending.
		// If efficiencies are equal (within epsilon 1e-9), deterministically tie-break:
		// collect all top candidates, sort deterministically by place.ID,
		// and pick uniformly using seeded rng.
		const eps = 1e-9
		maxEff := -1.0
		for _, c := range candidates {
			if c.efficiency > maxEff {
				maxEff = c.efficiency
			}
		}

		var bestCandidates []candidate
		for _, c := range candidates {
			if c.efficiency >= maxEff-eps {
				bestCandidates = append(bestCandidates, c)
			}
		}

		// Deterministic tie-break among bestCandidates
		sort.Slice(bestCandidates, func(i, j int) bool {
			return bestCandidates[i].place.ID < bestCandidates[j].place.ID
		})

		chosenIdx := 0
		if len(bestCandidates) > 1 {
			chosenIdx = rng.IntN(len(bestCandidates))
		}
		chosen := bestCandidates[chosenIdx]

		// Add chosen candidate to route
		stops = append(stops, Stop{
			PlaceID:          chosen.place.ID,
			Arrival:          chosen.arrival,
			Departure:        chosen.departure,
			HoursUnconfirmed: chosen.hoursUnconf,
			PriceUnconfirmed: chosen.priceUnconf,
		})

		visitedIdx[chosen.placeIndex] = true
		visitedPlaceIDs[chosen.place.ID] = true
		currentLocIndex = 1 + chosen.placeIndex
		currentTime = chosen.departure
		remainingBudget -= chosen.duration
		accumulatedCostMinor += chosen.costMinor

		totalWalkMin += chosen.scaledWalkMin
		totalVisitMin += chosen.place.VisitMinutes
		totalWalkKM += chosen.legKM
		totalValue += chosen.value

		if chosen.hoursUnconf || chosen.priceUnconf {
			unverifiedCount++
		}
		for _, cat := range chosen.place.Categories {
			categoryCounts[cat]++
		}
	}

	metrics := Metrics{
		WalkMinutes:     totalWalkMin,
		VisitMinutes:    totalVisitMin,
		TotalMinutes:    totalWalkMin + float64(totalVisitMin),
		WalkKM:          totalWalkKM,
		CostMinor:       accumulatedCostMinor,
		UnverifiedCount: unverifiedCount,
		CategoryCounts:  categoryCounts,
		InterestValue:   totalValue,
	}

	metrics.Score = p.score(metrics)
	return Route{
		Stops:   stops,
		Metrics: metrics,
		Seed:    req.Seed,
	}, nil
}

func validPoint(p Point) bool {
	return !math.IsNaN(p.Lat) && !math.IsNaN(p.Lon) && !math.IsInf(p.Lat, 0) && !math.IsInf(p.Lon, 0) && math.Abs(p.Lat) <= 90 && math.Abs(p.Lon) <= 180
}

func validateInputs(req Request, places []Place, matrix DistanceMatrix) (map[int64]int, error) {
	if req.BudgetMinutes <= 0 || int64(req.BudgetMinutes) > math.MaxInt64/int64(time.Minute) {
		return nil, fmt.Errorf("budget must be greater than zero")
	}
	if req.StartAt.IsZero() {
		return nil, fmt.Errorf("start time must not be zero")
	}
	if req.MaxTicketCostMinor != nil && *req.MaxTicketCostMinor < 0 {
		return nil, fmt.Errorf("ticket budget cannot be negative")
	}
	if matrix == nil {
		return nil, fmt.Errorf("distance matrix is required")
	}

	switch req.Profile {
	case "", ProfileBestMatch, ProfileMorePlaces, ProfileLessWalking, ProfileBudget:
	default:
		return nil, fmt.Errorf("unknown planning profile %q", req.Profile)
	}
	if req.Pace != "" && req.Pace != PaceNormal && req.Pace != PaceSlow && req.Pace != PaceFast {
		return nil, fmt.Errorf("invalid walking pace %q", req.Pace)
	}
	if !validPoint(req.Start) {
		return nil, fmt.Errorf("invalid start coordinates")
	}
	// First record for each ID is authoritative, including its matrix index.
	firstIndex := make(map[int64]int, len(places))
	for i, place := range places {
		if _, exists := firstIndex[place.ID]; exists {
			continue
		}
		firstIndex[place.ID] = i
		if place.ID <= 0 || place.Importance < 1 || place.Importance > 5 || place.VisitMinutes <= 0 || place.VisitMinutes > 1440 || place.TicketCostMinor < 0 || !validPoint(place.Location) {
			return nil, fmt.Errorf("invalid place at index %d", i)
		}
	}

	return firstIndex, nil
}
