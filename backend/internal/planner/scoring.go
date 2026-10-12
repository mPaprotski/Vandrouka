package planner

import (
	"fmt"
	"math"
)

// ScoringWeights specify penalties per kilometer, BYN and unverified stop.
type ScoringWeights struct{ WalkKM, CostBYN, Unverified float64 }

func DefaultScoringWeights() ScoringWeights {
	return ScoringWeights{WalkKM: 0.5, CostBYN: 0.1, Unverified: 0.5}
}

func NewWithWeights(weights ScoringWeights) (*Planner, error) {
	for _, w := range []float64{weights.WalkKM, weights.CostBYN, weights.Unverified} {
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 {
			return nil, fmt.Errorf("scoring penalties must be finite and nonnegative")
		}
	}
	return &Planner{weights: weights}, nil
}
func (p *Planner) score(metrics Metrics) float64 {
	return metrics.InterestValue - p.weights.WalkKM*metrics.WalkKM - p.weights.CostBYN*float64(metrics.CostMinor)/100 - p.weights.Unverified*float64(metrics.UnverifiedCount)
}
