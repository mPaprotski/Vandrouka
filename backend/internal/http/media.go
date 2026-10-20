package apihttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/media"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

type MediaService interface {
	Upload(context.Context, int64, int64, io.Reader, string) (visits.Photo, error)
	Read(context.Context, int64, string) (visits.Photo, []byte, error)
	Delete(context.Context, int64, string) error
	Discard(context.Context, int64, string) error
}
type mediaHandler struct {
	service        MediaService
	authentication AuthService
	mu             sync.Mutex
	windows        map[int64]authWindow
	slots          chan struct{}
	now            func() time.Time
}

func RegisterMedia(r chi.Router, authentication AuthService, service MediaService) {
	h := &mediaHandler{service: service, authentication: authentication, windows: make(map[int64]authWindow), slots: make(chan struct{}, 2), now: time.Now}
	r.Post("/v1/visits/photo-upload", h.upload)
	r.Get("/v1/visits/photos/{photo_id}", h.read)
	r.Delete("/v1/visits/photos/{photo_id}", h.remove)
}
func MediaResponses(timeout time.Duration) func(http.Handler) http.Handler {
	guard := responseGuard(timeout, mediaErrors)
	return func(next http.Handler) http.Handler {
		wrapped := guard(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			wrapped.ServeHTTP(w, r)
		})
	}
}
func mediaFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrInvalid):
		Error(w, 400, "invalid_request", "Некорректная фотография или параметры запроса")
	case errors.Is(err, media.ErrTooLarge):
		Error(w, 413, "request_too_large", "Фотография превышает лимит 10 МБ, 20 мегапикселей или 8192 пикселя по стороне")
	case errors.Is(err, media.ErrUnsupported):
		Error(w, 415, "unsupported_media_type", "Требуется фотография JPEG или PNG с соответствующим MIME")
	case errors.Is(err, media.ErrConfirmed):
		Error(w, 409, "photo_confirmed", "Фотография уже связана с подтверждённым посещением")
	case errors.Is(err, media.ErrNotFound):
		Error(w, 404, "not_found", "Объект не найден")
	case errors.Is(err, media.ErrUnavailable):
		Error(w, 503, "media_unavailable", "Фотохранилище временно недоступно")
	default:
		writeFailure(w, err, mediaErrors)
	}
}
func (h *mediaHandler) admit(w http.ResponseWriter, owner int64, upload bool) bool {
	if upload {
		h.mu.Lock()
		now := h.now()
		for id, v := range h.windows {
			if !now.Before(v.expires) {
				delete(h.windows, id)
			}
		}
		window, exists := h.windows[owner]
		if (!exists && len(h.windows) >= 4096) || window.count >= 6 {
			h.mu.Unlock()
			w.Header().Set("Retry-After", "60")
			Error(w, 429, "rate_limited", "Слишком много загрузок. Повторите через минуту")
			return false
		}
		if !exists {
			window.expires = now.Add(time.Minute)
		}
		window.count++
		h.windows[owner] = window
		h.mu.Unlock()
	}
	select {
	case h.slots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		Error(w, 429, "rate_limited", "Слишком много операций с фотографиями. Повторите позже")
		return false
	}
}
func readPhotoForm(w http.ResponseWriter, r *http.Request) (int64, []byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxFileBytes+(64<<10))
	mr, err := r.MultipartReader()
	if err != nil {
		return 0, nil, "", media.ErrUnsupported
	}
	var file []byte
	var declared string
	var place int64
	hasFile, hasPlace := false, false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, nil, "", formError(err)
		}
		switch part.FormName() {
		case "place_id":
			if hasPlace || part.FileName() != "" {
				return 0, nil, "", media.ErrInvalid
			}
			hasPlace = true
			value, err := io.ReadAll(io.LimitReader(part, 32))
			if err != nil {
				return 0, nil, "", formError(err)
			}
			place, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil || place < 1 {
				return 0, nil, "", media.ErrInvalid
			}
		case "file":
			if hasFile || part.FileName() == "" {
				return 0, nil, "", media.ErrInvalid
			}
			hasFile = true
			declared, _, err = mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil || (declared != "image/jpeg" && declared != "image/png") {
				return 0, nil, "", media.ErrUnsupported
			}
			file, err = io.ReadAll(io.LimitReader(part, media.MaxFileBytes+1))
			if err != nil {
				return 0, nil, "", formError(err)
			}
			if len(file) > media.MaxFileBytes {
				return 0, nil, "", media.ErrTooLarge
			}
		default:
			return 0, nil, "", media.ErrInvalid
		}
		if err := part.Close(); err != nil {
			return 0, nil, "", formError(err)
		}
	}
	if !hasFile || !hasPlace {
		return 0, nil, "", media.ErrInvalid
	}
	return place, file, declared, nil
}
func formError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return media.ErrTooLarge
	}
	return media.ErrInvalid
}
func (h *mediaHandler) upload(w http.ResponseWriter, r *http.Request) {
	requestlog.SetStage(r.Context(), "media")
	user, ok := authenticateRequest(w, r, h.authentication)
	if !ok {
		return
	}
	if !h.admit(w, user.ID, true) {
		return
	}
	defer func() { <-h.slots }()
	place, file, mime, err := readPhotoForm(w, r)
	if err != nil {
		mediaFailure(w, err)
		return
	}
	photo, err := h.service.Upload(r.Context(), user.ID, place, bytes.NewReader(file), mime)
	if err != nil {
		mediaFailure(w, err)
		return
	}
	JSON(w, 201, photo)
}
func (h *mediaHandler) read(w http.ResponseWriter, r *http.Request) {
	requestlog.SetStage(r.Context(), "media")
	user, ok := authenticateRequest(w, r, h.authentication)
	if !ok {
		return
	}
	if !h.admit(w, user.ID, false) {
		return
	}
	defer func() { <-h.slots }()
	photo, body, err := h.service.Read(r.Context(), user.ID, chi.URLParam(r, "photo_id"))
	if err != nil {
		mediaFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", *photo.MIMEType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
func (h *mediaHandler) remove(w http.ResponseWriter, r *http.Request) {
	requestlog.SetStage(r.Context(), "media")
	user, ok := authenticateRequest(w, r, h.authentication)
	if !ok {
		return
	}
	if !h.admit(w, user.ID, false) {
		return
	}
	defer func() { <-h.slots }()
	values, exists := r.URL.Query()["unconfirmed_only"]
	if exists && (len(values) != 1 || (values[0] != "true" && values[0] != "false")) {
		Error(w, 400, "invalid_request", "Некорректный режим удаления")
		return
	}
	var err error
	if exists && values[0] == "true" {
		err = h.service.Discard(r.Context(), user.ID, chi.URLParam(r, "photo_id"))
	} else {
		err = h.service.Delete(r.Context(), user.ID, chi.URLParam(r, "photo_id"))
	}
	if err != nil {
		mediaFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
