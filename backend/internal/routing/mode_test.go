package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentTravelModes(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		if strings.Contains(r.URL.Path, "directions-matrix") {
			json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "durations": [][]float64{{0, 60}, {60, 0}}, "distances": [][]float64{{0, 100}, {100, 0}}})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "routes": []any{map[string]any{"duration": 60, "distance": 100, "geometry": "shape", "legs": []any{map[string]any{"duration": 60, "distance": 100}}}}})
		}
	}))
	defer server.Close()
	client := testMapbox(server)
	points := []Point{{Lat: 53.9, Lon: 27.5}, {Lat: 53.91, Lon: 27.51}}
	var wg sync.WaitGroup
	for _, mode := range []string{"walking", "cycling"} {
		wg.Add(1)
		go func(mode string) {
			defer wg.Done()
			ctx := WithTravelMode(context.Background(), mode)
			if _, err := client.Matrix(ctx, points); err != nil {
				t.Error(err)
			}
			if _, err := client.Route(ctx, points); err != nil {
				t.Error(err)
			}
		}(mode)
	}
	wg.Wait()
	for _, profile := range []string{"walking", "cycling"} {
		for _, prefix := range []string{"/directions-matrix/v1/mapbox/", "/directions/v5/mapbox/"} {
			found := false
			for path, n := range paths {
				if strings.HasPrefix(path, prefix+profile+"/") && n == 1 {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s%s: %v", prefix, profile, paths)
			}
		}
	}
}
