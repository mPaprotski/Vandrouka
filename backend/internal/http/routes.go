package apihttp

import (
	"context"
	"encoding/json"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
)

const RouteTimeout = 180 * time.Second

type RouteGenerator interface {
	Generate(context.Context, string, planner.Request, bool) (routes.Result, error)
}

// RegisterRoutes bounds the complete generation, including provider pacing.
func RegisterRoutes(r chi.Router, service RouteGenerator) {
	r.Post("/v1/routes/generate", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), RouteTimeout)
		defer cancel()
		input, ok := ReadGenerateRouteRequest(w, r)
		if !ok {
			return
		}
		result, err := service.Generate(ctx, input.City, planner.Request{
			Start:   planner.Point{Lat: input.Start.Lat, Lon: input.Start.Lon},
			StartAt: input.StartAt, BudgetMinutes: input.BudgetMinutes,
			Pace: planner.Pace(input.Pace), Categories: input.Categories,
			MaxTicketCostMinor: input.TicketBudgetMinor, Seed: input.Seed,
		}, input.IncludeGeometry)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			writeFailure(w, err, generateErrors)
			return
		}
		requestlog.SetStage(ctx, "serialization")
		response, err := BuildGenerateRouteResponse(result)
		if err != nil {
			writeFailure(w, err, generateErrors)
			return
		}
		// Encode before committing the status, so malformed data cannot produce 200.
		body, err := json.Marshal(response)
		if err != nil {
			writeFailure(w, err, generateErrors)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(body, '\n'))
	})
}
