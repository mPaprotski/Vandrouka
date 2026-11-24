package apihttp

import (
	"strings"
	"testing"
)

func TestWalkRequestLimits(t *testing.T) {
	r, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(map[string]any{"max_distance_m": 3500.5, "max_stops": 4, "travel_mode": "cycling"})))
	if err != nil || r.TravelMode != "cycling" || r.MaxStops == nil || *r.MaxStops != 4 || r.MaxDistanceM == nil || *r.MaxDistanceM != 3500.5 {
		t.Fatalf("new parameters lost: %+v %v", r, err)
	}
	for _, fields := range []map[string]any{{"max_stops": 0}, {"max_stops": 1.5}, {"max_distance_m": 0}, {"travel_mode": "car"}, {"travel_mode": nil}} {
		if _, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(fields))); err == nil {
			t.Fatalf("invalid parameters accepted: %v", fields)
		}
	}
	old, err := ParseGenerateRouteRequest(strings.NewReader(requestBody(nil)))
	if err != nil || old.TravelMode != "walking" || old.MaxDistanceM != nil || old.MaxStops != nil {
		t.Fatalf("legacy defaults changed: %+v %v", old, err)
	}
}
