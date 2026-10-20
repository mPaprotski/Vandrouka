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
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
)

func TestRecalculateEndpointWithService(t *testing.T) {
	for _, tc := range []struct {
		name, change, start string
		budget, status      int
		providerError       error
		code                string
	}{
		{name: "add", change: `{"type":"add","place_id":2}`, start: "10:00:00", budget: 27, status: 200},
		{name: "remove and refill", change: `{"type":"remove","place_id":1}`, start: "10:00:00", budget: 27, status: 200},
		{name: "remove to empty", change: `{"type":"remove","place_id":1}`, start: "10:00:00", budget: 11, status: 200},
		{name: "mandatory cannot fit", change: `{"type":"add","place_id":2}`, start: "10:00:00", budget: 11, status: 422, code: "constraints_violation"},
		{name: "closed baseline", change: `{"type":"add","place_id":2}`, start: "11:00:00", budget: 27, status: 422, code: "constraints_violation"},
		{name: "unknown target", change: `{"type":"add","place_id":999}`, start: "10:00:00", budget: 27, status: 404, code: "not_found"},
		{name: "invalid remove", change: `{"type":"remove","place_id":2}`, start: "10:00:00", budget: 27, status: 400, code: "invalid_request"},
		{name: "provider", change: `{"type":"add","place_id":2}`, start: "10:00:00", budget: 27, status: 503, providerError: routing.ErrRateLimited, code: "routing_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &httpRouting{err: tc.providerError}
			service := routes.NewService(httpCatalog{}, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(provider)), routes.WithDirections(provider))
			router := chi.NewRouter()
			RegisterRecalculation(router, service)
			request := map[string]any{"start": map[string]float64{"lat": 53.9, "lon": 27.5}, "start_at": "2026-10-24T" + tc.start + "+03:00", "budget_minutes": tc.budget, "include_geometry": false}
			var change any
			if err := json.Unmarshal([]byte(tc.change), &change); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]any{"request": request, "base_place_ids": []int64{1}, "change": change})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/v1/routes/recalculate", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.status != 200 {
				var e APIError
				if json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != tc.code {
					t.Fatal(w.Body.String())
				}
				return
			}
			var result RecalculateRouteResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Seed != 42 || result.Timezone != "Europe/Minsk" || result.Variant.Geometry != nil {
				t.Fatal("metadata")
			}
			switch tc.name {
			case "add":
				if len(result.Variant.Stops) != 2 || result.Variant.Totals.TotalSeconds != 1620 || len(result.Diff.AddedPlaceIDs) != 1 || result.Diff.AddedPlaceIDs[0] != 2 || result.Diff.TotalSecondsDelta != 960 {
					t.Fatalf("add %+v", result)
				}
			case "remove and refill":
				if len(result.Variant.Stops) != 1 || result.Variant.Stops[0].PlaceID != 2 || result.Diff.RemovedPlaceIDs[0] != 1 || result.Diff.CostMinorDelta != -150 {
					t.Fatalf("remove %+v", result)
				}
			case "remove to empty":
				if len(result.Variant.Stops) != 0 || result.Variant.Totals.TotalSeconds != 0 || result.Diff.TotalSecondsDelta != -660 {
					t.Fatalf("empty %+v", result)
				}
			}
			if dir := os.Getenv("ROUTE_RESPONSE_EXAMPLES_DIR"); dir != "" && tc.name == "add" {
				if err := os.WriteFile(filepath.Join(dir, "recalculate-http-response.json"), w.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type failingRecalculator struct {
	err           error
	modifications int
}

func (s *failingRecalculator) PrepareBaseline(ctx context.Context, city string, req planner.Request, ids []int64, target int64, geometry bool) (routes.Baseline, error) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > RouteTimeout || time.Until(deadline) < 170*time.Second {
		return routes.Baseline{}, fmt.Errorf("incorrect timeout")
	}
	return routes.Baseline{}, s.err
}
func (s *failingRecalculator) Add(context.Context, routes.Baseline, int64, bool) (routes.VerifiedVariant, error) {
	s.modifications++
	return routes.VerifiedVariant{}, nil
}
func (s *failingRecalculator) Remove(context.Context, routes.Baseline, int64, bool) (routes.VerifiedVariant, error) {
	s.modifications++
	return routes.VerifiedVariant{}, nil
}

func TestRecalculateErrorEnvelopeAndEarlyStop(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{routes.ErrCityNotFound, 404, "not_found"}, {routes.ErrPlaceNotFound, 404, "not_found"},
		{routes.ErrConstraintsViolation, 422, "constraints_violation"},
		{context.DeadlineExceeded, 503, "routing_unavailable"}, {context.Canceled, 503, "routing_unavailable"},
		{fmt.Errorf("private-token URL"), 500, "internal"},
	} {
		service := &failingRecalculator{err: tc.err}
		router := chi.NewRouter()
		RegisterRecalculation(router, service)
		req := httptest.NewRequest("POST", "/v1/routes/recalculate", strings.NewReader(validRecalculateBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var envelope APIError
		if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Error.Code != tc.code || strings.Contains(w.Body.String(), "private-token") || service.modifications != 0 {
			t.Fatalf("%d %s modifications %d", w.Code, w.Body.String(), service.modifications)
		}
	}
}
