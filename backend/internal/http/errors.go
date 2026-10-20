package apihttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/store"
)

type errorScope string

const (
	mediaErrors       errorScope = "media"
	catalogErrors     errorScope = "catalog"
	generateErrors    errorScope = "generate"
	recalculateErrors errorScope = "recalculate"
)

// writeFailure maps service errors to the existing public contract. Raw errors
// can contain SQL or provider credentials, so neither responses nor logs expose them.
func writeFailure(w http.ResponseWriter, err error, scope errorScope) {
	status, code, message := http.StatusInternalServerError, "internal", "Не удалось выполнить запрос"
	switch {
	case scope == mediaErrors && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)):
		status, code, message = 503, "media_unavailable", "Фотохранилище временно недоступно"
	case (scope == generateErrors || scope == recalculateErrors) && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, routes.ErrRoutingUnavailable)):
		status, code, message = 503, "routing_unavailable", "Сервис маршрутизации временно недоступен"
	case errors.Is(err, ErrInvalidRouteRequest):
		status, code, message = 400, "invalid_request", "Некорректные параметры запроса"
	case errors.Is(err, store.ErrNotFound) || errors.Is(err, routes.ErrCityNotFound) || errors.Is(err, routes.ErrPlaceNotFound):
		status, code, message = 404, "not_found", "Объект не найден"
	case scope == recalculateErrors && errors.Is(err, routes.ErrConstraintsViolation):
		status, code, message = 422, "constraints_violation", "Не удалось выполнить изменение в пределах ограничений маршрута"
	}
	if status >= 500 {
		slog.Error("API request failed", "scope", string(scope), "code", code, "status", status, "request_id", w.Header().Get("X-Request-ID"))
	}
	Error(w, status, code, message)
}

func failure(w http.ResponseWriter, err error) { writeFailure(w, err, catalogErrors) }
