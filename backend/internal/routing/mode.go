package routing

import "context"

type modeKey struct{}

// The mode belongs to a single request. The shared client and pacing stay shared.
func WithTravelMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, modeKey{}, mode)
}

func mapboxProfile(ctx context.Context) string {
	if ctx.Value(modeKey{}) == "cycling" {
		return "cycling"
	}
	return "walking"
}
