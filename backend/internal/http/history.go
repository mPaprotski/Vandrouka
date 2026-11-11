package apihttp

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/history"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"net/http"
	"net/url"
	"strconv"
)

type HistoryService interface {
	List(context.Context, int64, int, string) (history.Page, error)
}

func RegisterHistory(r chi.Router, authentication AuthService, service HistoryService) {
	r.Get("/v1/users/me/visits", func(w http.ResponseWriter, r *http.Request) {
		requestlog.SetStage(r.Context(), "history")
		owner, ok := authenticateRequest(w, r, authentication)
		if !ok {
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		invalid := err != nil
		for key, values := range query {
			if (key != "limit" && key != "cursor") || len(values) != 1 {
				invalid = true
			}
		}
		limit := 20
		if values, exists := query["limit"]; exists {
			limit, err = strconv.Atoi(values[0])
			if err != nil || limit < 1 || limit > 50 {
				invalid = true
			}
		}
		if invalid {
			Error(w, 400, "invalid_request", "Некорректные параметры страницы истории")
			return
		}
		page, err := service.List(r.Context(), owner.ID, limit, query.Get("cursor"))
		if err != nil {
			if errors.Is(err, history.ErrInvalid) {
				Error(w, 400, "invalid_request", "Некорректный курсор страницы истории")
			} else {
				failure(w, err)
			}
			return
		}
		JSON(w, 200, page)
	})
}
