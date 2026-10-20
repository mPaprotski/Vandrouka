package apihttp

import (
	"context"
	"encoding/json"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
)

type RouteRecalculator interface {
	PrepareBaseline(context.Context, string, planner.Request, []int64, int64, bool) (routes.Baseline, error)
	Remove(context.Context, routes.Baseline, int64, bool) (routes.VerifiedVariant, error)
	Add(context.Context, routes.Baseline, int64, bool) (routes.VerifiedVariant, error)
}

func RegisterRecalculation(r chi.Router, service RouteRecalculator) {
	r.Post("/v1/routes/recalculate", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), RouteTimeout)
		defer cancel()
		input, ok := ReadRecalculateRouteRequest(w, r)
		if !ok {
			return
		}
		req := input.Request
		baseline, err := service.PrepareBaseline(ctx, req.City, planner.Request{
			Start: planner.Point{Lat: req.Start.Lat, Lon: req.Start.Lon}, StartAt: req.StartAt,
			BudgetMinutes: req.BudgetMinutes, Pace: planner.Pace(req.Pace), Categories: req.Categories,
			MaxTicketCostMinor: req.TicketBudgetMinor, Seed: req.Seed,
		}, input.BasePlaceIDs, input.PlaceID, req.IncludeGeometry)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			writeFailure(w, err, recalculateErrors)
			return
		}
		var after routes.VerifiedVariant
		switch input.ChangeType {
		case RouteChangeRemove:
			after, err = service.Remove(ctx, baseline, input.PlaceID, req.IncludeGeometry)
		case RouteChangeAdd:
			after, err = service.Add(ctx, baseline, input.PlaceID, req.IncludeGeometry)
		}
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			writeFailure(w, err, recalculateErrors)
			return
		}
		requestlog.SetStage(ctx, "serialization")
		response, err := BuildRecalculateRouteResponse(baseline, after)
		if err != nil {
			writeFailure(w, err, recalculateErrors)
			return
		}
		body, err := json.Marshal(response)
		if err != nil {
			writeFailure(w, err, recalculateErrors)
			return
		}
		if err := ctx.Err(); err != nil {
			writeFailure(w, err, recalculateErrors)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(body, '\n'))
	})
}
