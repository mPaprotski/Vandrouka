package apihttp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type generatorFunc func(context.Context, string, planner.Request, bool) (routes.Result, error)

func (f generatorFunc) Generate(c context.Context, s string, r planner.Request, g bool) (routes.Result, error) {
	return f(c, s, r, g)
}
func TestGenerateEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"empty", nil, 200, ""}, {"city", routes.ErrCityNotFound, 404, "not_found"},
		{"provider", fmt.Errorf("%w: secret-token", routes.ErrRoutingUnavailable), 503, "routing_unavailable"},
		{"timeout", context.DeadlineExceeded, 503, "routing_unavailable"},
		{"internal", fmt.Errorf("secret-token"), 500, "internal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			RegisterRoutes(r, generatorFunc(func(ctx context.Context, city string, req planner.Request, g bool) (routes.Result, error) {
				d, ok := ctx.Deadline()
				if !ok || time.Until(d) < 170*time.Second || time.Until(d) > RouteTimeout {
					t.Fatal("timeout")
				}
				if city != "minsk" || req.Seed != 42 || req.Pace != planner.PaceNormal || req.BudgetMinutes != 120 || !g || req.MaxTicketCostMinor != nil {
					t.Fatalf("request %+v", req)
				}
				return routes.Result{Prepared: routes.Prepared{City: places.City{Timezone: "Europe/Minsk"}, Request: req}}, tc.err
			}))
			req := httptest.NewRequest("POST", "/v1/routes/generate", strings.NewReader(`{"start":{"lat":53.9,"lon":27.5},"start_at":"2026-10-24T10:00:00+03:00","budget_minutes":120}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status || strings.Contains(w.Body.String(), "secret-token") {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.status == 200 {
				var v GenerateRouteResponse
				if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.Variants == nil || len(v.Variants) != 0 || v.Seed != 42 {
					t.Fatal(w.Body.String())
				}
			} else {
				var e APIError
				if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e.Error.Code != tc.code {
					t.Fatal(w.Body.String())
				}
			}
		})
	}
}
func TestGenerateInvalidRequestDoesNotCallService(t *testing.T) {
	r := chi.NewRouter()
	RegisterRoutes(r, generatorFunc(func(context.Context, string, planner.Request, bool) (routes.Result, error) {
		t.Fatal("service called")
		return routes.Result{}, nil
	}))
	for _, body := range []string{`{}`, `{"unknown":true}`, strings.Repeat("x", MaxRouteRequestBytes+1)} {
		req := httptest.NewRequest("POST", "/v1/routes/generate", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
