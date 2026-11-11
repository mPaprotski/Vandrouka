package apihttp

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/progress"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
)

type ProgressService interface {
	Read(context.Context, int64) (progress.State, error)
}

func RegisterProgress(r chi.Router, authentication AuthService, service ProgressService) {
	r.Get("/v1/me/progress", func(w http.ResponseWriter, r *http.Request) {
		requestlog.SetStage(r.Context(), "progress")
		owner, ok := authenticateRequest(w, r, authentication)
		if !ok {
			return
		}
		result, err := service.Read(r.Context(), owner.ID)
		if err != nil {
			failure(w, err)
			return
		}
		JSON(w, 200, result)
	})
}
