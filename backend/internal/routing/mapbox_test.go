package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testMapbox(server *httptest.Server) *MapboxClient {
	c := NewMapboxClient(server.URL, "test-private-token", time.Second)
	c.interval = 0
	return c
}
func TestMapboxMatrixBatching(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasPrefix(r.URL.Path, "/directions-matrix/v1/mapbox/walking/") {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		if r.URL.Query().Get("annotations") != "duration,distance" || r.URL.Query().Get("access_token") != "test-private-token" {
			t.Error("wrong query")
		}
		coords := strings.Split(strings.TrimPrefix(r.URL.Path, "/directions-matrix/v1/mapbox/walking/"), ";")
		if len(coords) > 25 || len(coords) < 2 {
			t.Errorf("coordinate limit: %d", len(coords))
		}
		src := strings.Split(r.URL.Query().Get("sources"), ";")
		dst := strings.Split(r.URL.Query().Get("destinations"), ";")
		if len(src)*len(dst) < 2 {
			t.Error("Mapbox minimum elements violated")
		}
		durations, distances := make([][]any, len(src)), make([][]any, len(src))
		for i, s := range src {
			si, _ := strconv.Atoi(s)
			source, _ := strconv.ParseFloat(strings.Split(coords[si], ",")[0], 64)
			durations[i] = make([]any, len(dst))
			distances[i] = make([]any, len(dst))
			for j, d := range dst {
				di, _ := strconv.Atoi(d)
				target, _ := strconv.ParseFloat(strings.Split(coords[di], ",")[0], 64)
				if source == 1 && target == 2 {
					continue
				}
				if source == target {
					durations[i][j] = float64(0)
					distances[i][j] = float64(0)
					continue
				}
				durations[i][j] = source*100 + target
				distances[i][j] = (source*100 + target) * 10
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "durations": durations, "distances": distances})
	}))
	defer server.Close()
	for _, n := range []int{2, 25, 26, 61} {
		points := make([]Point, n)
		for i := range points {
			points[i] = Point{Lon: float64(i), Lat: 53}
		}
		calls = 0
		m, err := testMapbox(server).Matrix(context.Background(), points)
		if err != nil {
			t.Fatal(err)
		}
		if n <= 25 && calls != 1 {
			t.Errorf("small matrix: calls=%d", calls)
		}
		if n > 25 && calls <= 1 {
			t.Error("matrix was not split")
		}
		for i := range points {
			for j := range points {
				leg := m[i][j]
				if i == 1 && j == 2 {
					if leg.Reachable {
						t.Fatal("null pair reachable")
					}
					continue
				}
				want := float64(i*100 + j)
				if i == j {
					want = 0
				}
				if !leg.Reachable || leg.Seconds != want || leg.Kilometers != want/100 {
					t.Fatalf("n=%d %d->%d got=%+v", n, i, j, leg)
				}
			}
		}
	}
	_, err := testMapbox(server).Between(context.Background(), []Point{{Lon: 1}}, []Point{{Lon: 2}})
	if err != nil {
		t.Fatal(err)
	}
}
func TestMapboxErrorsAndTokenRedaction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"auth", 401, `{"message":"test-private-token"}`, ErrAuthentication},
		{"rate", 429, `{}`, ErrRateLimited},
		{"server", 500, `{}`, ErrUnavailable},
		{"bad json", 200, `garbage`, ErrUnavailable},
		{"wrong dimensions", 200, `{"code":"Ok","durations":[],"distances":[]}`, ErrUnavailable},
		{"half null", 200, `{"code":"Ok","durations":[[null,null],[null,null]],"distances":[[1,null],[null,null]]}`, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			_, err := testMapbox(server).Matrix(context.Background(), []Point{{Lon: 1}, {Lon: 2}})
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "test-private-token") {
				t.Fatalf("unsafe or wrong error: %v", err)
			}
		})
	}
	c := NewMapboxClient("http://127.0.0.1:1", "test-private-token", time.Second)
	c.interval = 0
	_, err := c.Matrix(context.Background(), []Point{{Lon: 1}, {Lon: 2}})
	if err == nil || strings.Contains(err.Error(), "test-private-token") || strings.Contains(err.Error(), "access_token") {
		t.Fatalf("unsafe network error %v", err)
	}
	c = NewMapboxClient("", "", time.Second)
	if _, err = c.Matrix(context.Background(), []Point{{Lon: 1}}); !errors.Is(err, ErrAuthentication) {
		t.Fatal("missing token accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c = NewMapboxClient("http://127.0.0.1:1", "token", time.Second)
	if _, err = c.Matrix(ctx, []Point{{Lon: 1}, {Lon: 2}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("context ignored: %v", err)
	}
}
func TestMapboxDirectionsChunks(t *testing.T) {
	calls := 0
	previousLast := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coords := strings.Split(strings.TrimPrefix(r.URL.Path, "/directions/v5/mapbox/walking/"), ";")
		if len(coords) > 25 {
			t.Error("Directions coordinate limit exceeded")
		}
		if calls > 0 && coords[0] != previousLast {
			t.Error("chunks do not share endpoint")
		}
		previousLast = coords[len(coords)-1]
		calls++
		if r.URL.Query().Get("geometries") != "polyline6" {
			t.Error("wrong geometry format")
		}
		legs := make([]map[string]float64, len(coords)-1)
		for i := range legs {
			legs[i] = map[string]float64{"duration": 10, "distance": 100}
		}
		json.NewEncoder(w).Encode(map[string]any{"code": "Ok", "routes": []any{map[string]any{"duration": 10 * (len(coords) - 1), "distance": 100 * (len(coords) - 1), "geometry": "polyline-shape", "legs": legs}}})
	}))
	defer server.Close()
	points := make([]Point, 61)
	for i := range points {
		points[i] = Point{Lon: float64(i)}
	}
	route, err := testMapbox(server).Route(context.Background(), points)
	if err != nil || calls != 3 || route.Seconds != 600 || route.Kilometers != 6 || len(route.Shapes) != 3 {
		t.Fatalf("route=%+v calls=%d err=%v", route, calls, err)
	}
}
