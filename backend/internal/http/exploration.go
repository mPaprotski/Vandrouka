package apihttp

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/exploration"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"net/http"
)

type ExplorationService interface {
	Read(context.Context, int64) (exploration.State, error)
}

func RegisterExploration(r chi.Router, authentication AuthService, service ExplorationService) {
	r.Get("/v1/me/exploration", func(w http.ResponseWriter, r *http.Request) {
		requestlog.SetStage(r.Context(), "exploration")
		owner, ok := authenticateRequest(w, r, authentication)
		if !ok {
			return
		}
		state, err := service.Read(r.Context(), owner.ID)
		if err != nil {
			failure(w, err)
			return
		}
		JSON(w, 200, state)
	})
}
