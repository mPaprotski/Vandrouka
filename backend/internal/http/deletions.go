package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/auth"
	"github.com/yourname/vandrouka/backend/internal/history"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

type AccountDeletion interface {
	DeleteAccount(context.Context, int64, string) error
}
type HistoryDeletion interface {
	Hide(context.Context, int64, int64) error
}

func RegisterDeletions(r chi.Router, authentication AuthService, accounts AccountDeletion, visits HistoryDeletion) {
	limits := newVisitLimits()
	passwordSlots := make(chan struct{}, 2)
	r.Delete("/v1/users/me/visits/{id}", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := authenticateRequest(w, r, authentication)
		if !ok {
			return
		}
		if !limits.admit(w, owner.ID) {
			return
		}
		defer func() { <-limits.slots }()
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || id < 1 {
			Error(w, 400, "invalid_request", "Некорректное посещение")
			return
		}
		if err = visits.Hide(r.Context(), owner.ID, id); err != nil {
			if errors.Is(err, history.ErrNotFound) {
				Error(w, 404, "not_found", "Объект не найден")
			} else {
				failure(w, err)
			}
			return
		}
		w.WriteHeader(204)
	})
	accountLimits := &authHandler{windows: make(map[string]authWindow), now: time.Now}
	r.Delete("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := authenticateRequest(w, r, authentication)
		if !ok {
			return
		}
		if !accountLimits.allow(strconv.FormatInt(owner.ID, 10), 5) {
			limitedAuth(w)
			return
		}
		select {
		case passwordSlots <- struct{}{}:
			defer func() { <-passwordSlots }()
		default:
			limitedAuth(w)
			return
		}
		content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || content != "application/json" {
			Error(w, 415, "unsupported_media_type", "Требуется application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		var body struct {
			Password string `json:"password"`
		}
		if d.Decode(&body) != nil {
			Error(w, 400, "invalid_request", "Требуется текущий пароль")
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			Error(w, 400, "invalid_request", "Некорректный запрос")
			return
		}
		if err = accounts.DeleteAccount(r.Context(), owner.ID, body.Password); err != nil {
			if errors.Is(err, auth.ErrCredentials) {
				Error(w, 403, "invalid_credentials", "Неверный пароль")
			} else if errors.Is(err, auth.ErrUnauthorized) {
				Error(w, 401, "unauthorized", "Сессия завершена")
			} else if errors.Is(err, auth.ErrInvalidInput) {
				Error(w, 400, "invalid_request", "Некорректный пароль")
			} else {
				failure(w, err)
			}
			return
		}
		w.WriteHeader(204)
	})
}
