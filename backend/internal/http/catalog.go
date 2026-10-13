package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/store"
)

type Catalog interface {
	ListCities(context.Context) ([]places.City, error)
	ListCategories(context.Context, string) ([]places.Category, error)
	ListPlaces(context.Context, string, string, string) ([]places.Place, error)
	GetPlace(context.Context, int64) (places.Place, error)
}

type Handler struct{ catalog Catalog }

func RegisterCatalog(r chi.Router, catalog Catalog) {
	h := &Handler{catalog: catalog}
	r.Get("/v1/cities", h.cities)
	r.Get("/v1/cities/{slug}/categories", h.categories)
	r.Get("/v1/cities/{slug}/places", h.places)
	r.Get("/v1/places/{id}", h.place)
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode response", "error", err)
	}
}
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, APIError{Error: ErrorDetail{Code: code, Message: message}})
}

func failure(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		Error(w, 404, "not_found", "Объект не найден")
		return
	}
	slog.Error("catalog request failed", "error", err)
	Error(w, 500, "internal", "Не удалось выполнить запрос")
}

var validSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func city(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := chi.URLParam(r, "slug")
	if len(slug) > 100 || !validSlug.MatchString(slug) {
		Error(w, 400, "invalid_request", "Некорректный идентификатор города")
		return "", false
	}
	return slug, true
}
func (h *Handler) cities(w http.ResponseWriter, r *http.Request) {
	result, err := h.catalog.ListCities(r.Context())
	if err != nil {
		failure(w, err)
		return
	}
	JSON(w, 200, map[string]any{"cities": result})
}
func (h *Handler) categories(w http.ResponseWriter, r *http.Request) {
	slug, ok := city(w, r)
	if !ok {
		return
	}
	result, err := h.catalog.ListCategories(r.Context(), slug)
	if err != nil {
		failure(w, err)
		return
	}
	JSON(w, 200, map[string]any{"categories": result})
}
func (h *Handler) places(w http.ResponseWriter, r *http.Request) {
	slug, ok := city(w, r)
	if !ok {
		return
	}
	category := r.URL.Query().Get("category")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 200 || len(category) > 100 || category != "" && !validSlug.MatchString(category) {
		Error(w, 400, "invalid_request", "Некорректные параметры поиска")
		return
	}
	if category != "" {
		categories, err := h.catalog.ListCategories(r.Context(), slug)
		if err != nil {
			failure(w, err)
			return
		}
		found := false
		for _, c := range categories {
			if c.Slug == category {
				found = true
				break
			}
		}
		if !found {
			Error(w, 400, "invalid_request", "Неизвестная категория")
			return
		}
	}
	result, err := h.catalog.ListPlaces(r.Context(), slug, category, query)
	if err != nil {
		failure(w, err)
		return
	}
	JSON(w, 200, map[string]any{"places": result})
}
func (h *Handler) place(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		Error(w, 400, "invalid_request", "Некорректный идентификатор места")
		return
	}
	result, err := h.catalog.GetPlace(r.Context(), id)
	if err != nil {
		failure(w, err)
		return
	}
	JSON(w, 200, result)
}
