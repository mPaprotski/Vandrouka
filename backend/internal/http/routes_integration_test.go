package apihttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

type httpCatalog struct{}

func (httpCatalog) ListCities(context.Context) ([]places.City, error) {
	return []places.City{{Slug: "minsk", Timezone: "Europe/Minsk"}}, nil
}
func (httpCatalog) ListActivePlaces(context.Context, string) ([]places.Place, error) {
	return []places.Place{
		{ID: 1, City: "minsk", Name: "Museum", Lat: 53.901, Lon: 27.501, Categories: []string{"museums"}, Importance: 5, VisitMinutes: 10, PriceKnown: true, Prices: []places.Price{{Kind: "adult", Amount: 1.5, Currency: "BYN"}}, HoursKnown: true, OpeningHours: []places.Hours{{Weekday: 6, OpenTime: "10:00", CloseTime: "11:00"}}},
		{ID: 2, City: "minsk", Name: "Park", Lat: 53.902, Lon: 27.502, Categories: []string{"parks"}, Importance: 3, VisitMinutes: 15},
	}, nil
}

type httpRouting struct {
	err        error
	directions int
}

func (p *httpRouting) Matrix(ctx context.Context, points []routing.Point) (routing.Matrix, error) {
	if p.err != nil {
		return nil, p.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := make(routing.Matrix, len(points))
	for i := range m {
		m[i] = make([]routing.Leg, len(points))
		for j := range m[i] {
			m[i][j] = routing.Leg{Reachable: true, Seconds: 60, Kilometers: .1}
		}
	}
	return m, nil
}
func (p *httpRouting) Route(ctx context.Context, points []routing.Point) (routing.Route, error) {
	p.directions++
	if err := ctx.Err(); err != nil {
		return routing.Route{}, err
	}
	result := routing.Route{Shapes: []string{"test-polyline"}}
	for i := 1; i < len(points); i++ {
		result.Legs = append(result.Legs, routing.Leg{Reachable: true, Seconds: 60, Kilometers: .1})
	}
	return result, nil
}
func TestGenerateHTTPServiceIntegration(t *testing.T) {
	for _, tc := range []struct {
		name, categories string
		budget, cost     int
		geometry         bool
		start            string
		want             int
	}{
		{"both", `["museums","parks"]`, 27, 150, true, "10:00:00", 2},
		{"geometry off", `["museums","parks"]`, 27, 150, false, "10:00:00", 2},
		{"zero ticket", `["museums","parks"]`, 27, 0, true, "10:00:00", 1},
		{"museum", `["museums"]`, 11, 150, true, "10:00:00", 1},
		{"closed during visit", `["museums"]`, 20, 150, true, "10:55:00", 0},
		{"too little time", `["museums"]`, 10, 150, true, "10:00:00", 0},
		{"empty category", `["streets"]`, 27, 150, true, "10:00:00", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &httpRouting{}
			service := routes.NewService(httpCatalog{}, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(provider)), routes.WithDirections(provider))
			router := chi.NewRouter()
			RegisterRoutes(router, service)
			body := fmt.Sprintf(`{"start":{"lat":53.9,"lon":27.5},"start_at":"2026-10-24T%s+03:00","budget_minutes":%d,"ticket_budget_minor":%d,"categories":%s,"seed":7,"include_geometry":%t}`, tc.start, tc.budget, tc.cost, tc.categories, tc.geometry)
			call := func() []byte {
				req := httptest.NewRequest("POST", "/v1/routes/generate", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != 200 {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				return w.Body.Bytes()
			}
			first, second := call(), call()
			if string(first) != string(second) {
				t.Fatal("seed not reproducible")
			}
			var response GenerateRouteResponse
			if err := json.Unmarshal(first, &response); err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 {
				if len(response.Variants) != 0 {
					t.Fatal("expected empty")
				}
				return
			}
			if len(response.Variants) == 0 {
				t.Fatal("missing variants")
			}
			v := response.Variants[0]
			if len(v.Stops) != tc.want || v.Totals.TotalSeconds > float64(tc.budget*60) || v.Totals.CostMinor > int64(tc.cost) {
				t.Fatalf("invalid totals %+v", v.Totals)
			}
			if (v.Geometry != nil) != tc.geometry || provider.directions == 0 {
				t.Fatal("geometry or verification")
			}
			if tc.want == 2 && (v.Totals.WalkSeconds != 120 || v.Totals.VisitSeconds != 1500 || v.Totals.TotalSeconds != 1620 || v.Totals.DistanceM != 200 || v.Totals.CostMinor != 150) {
				t.Fatalf("independent totals differ %+v", v.Totals)
			}
			if dir := os.Getenv("ROUTE_RESPONSE_EXAMPLES_DIR"); dir != "" && tc.name == "both" {
				if err := os.WriteFile(filepath.Join(dir, "route-http-response.json"), first, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestGenerateHTTPProviderFailureAndCancellation(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		provider := &httpRouting{err: routing.ErrRateLimited}
		service := routes.NewService(httpCatalog{}, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(provider)), routes.WithDirections(provider))
		router := chi.NewRouter()
		RegisterRoutes(router, service)
		req := httptest.NewRequest("POST", "/v1/routes/generate", strings.NewReader(`{"start":{"lat":53.9,"lon":27.5},"start_at":"2026-10-24T10:00:00+03:00","budget_minutes":30}`))
		req.Header.Set("Content-Type", "application/json")
		if mode == 1 {
			ctx, stop := context.WithCancel(req.Context())
			stop()
			req = req.WithContext(ctx)
		}
		if mode == 2 {
			ctx, stop := context.WithTimeout(req.Context(), -time.Second)
			defer stop()
			req = req.WithContext(ctx)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 503 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if provider.directions != 0 {
			t.Fatal("continued after failure")
		}
	}
}
