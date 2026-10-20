package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

type VisitService interface {
	Confirm(context.Context, int64, int64, string, visits.Input, time.Time) (visits.Result, error)
}

func RegisterVisits(r chi.Router, authentication AuthService, service VisitService) {
	r.Post("/v1/places/{id}/visits", func(w http.ResponseWriter, r *http.Request) {
		received := time.Now().UTC()
		requestlog.SetStage(r.Context(), "visits")
		user, ok := authenticateRequest(w, r, authentication)
		if !ok {
			return
		}
		place, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || place < 1 {
			Error(w, 400, "invalid_request", "Некорректное место")
			return
		}
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) != 1 {
			Error(w, 400, "invalid_request", "Требуется один Idempotency-Key UUID")
			return
		}
		content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || content != "application/json" {
			Error(w, 415, "unsupported_media_type", "Требуется application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		// Pointer fields distinguish absent/null coordinates and accuracy from valid zero.
		var body struct {
			PhotoID  string `json:"photo_id"`
			Location *struct {
				Lat        *float64  `json:"lat"`
				Lon        *float64  `json:"lon"`
				Accuracy   *float64  `json:"accuracy_m"`
				ObservedAt time.Time `json:"observed_at"`
			} `json:"location"`
		}
		err = decoder.Decode(&body)
		if err == nil {
			var extra any
			err = decoder.Decode(&extra)
			if err == io.EOF {
				err = nil
			} else if err == nil {
				err = visits.ErrInvalid
			}
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				Error(w, 413, "request_too_large", "Слишком большой запрос")
			} else {
				Error(w, 400, "invalid_request", "Некорректные параметры посещения")
			}
			return
		}
		l := body.Location
		if l == nil || l.Lat == nil || l.Lon == nil || l.Accuracy == nil {
			Error(w, 400, "invalid_request", "Требуются координаты и точность GPS")
			return
		}
		result, err := service.Confirm(r.Context(), user.ID, place, keys[0], visits.Input{PhotoID: body.PhotoID, Location: visits.Location{Lat: *l.Lat, Lon: *l.Lon, AccuracyM: *l.Accuracy, ObservedAt: l.ObservedAt}}, received)
		if err != nil {
			switch {
			case errors.Is(err, visits.ErrInvalid):
				Error(w, 400, "invalid_request", "Некорректные параметры посещения")
			case errors.Is(err, visits.ErrNotFound):
				Error(w, 404, "not_found", "Объект не найден")
			case errors.Is(err, visits.ErrConflict):
				Error(w, 409, "idempotency_conflict", "Ключ уже использован с другими параметрами")
			case errors.Is(err, visits.ErrPhotoUsed):
				Error(w, 409, "photo_already_used", "Фотография уже подтверждает другое посещение")
			default:
				failure(w, err)
			}
			return
		}
		if result.Visit.Status == visits.Rejected {
			code := string(*result.Visit.RejectionCode)
			messages := map[string]string{"too_far": "Подойдите ближе к месту", "poor_accuracy": "Дождитесь более точного GPS", "stale_location": "Получите свежую геопозицию", "future_location": "Проверьте время на устройстве", "inactive_place": "Место недоступно для посещения", "photo_not_ready": "Дождитесь готовности фотографии"}
			JSON(w, 422, map[string]any{"visit": result.Visit, "xp_awarded": 0, "error": map[string]string{"code": code, "message": messages[code]}})
			return
		}
		status := 201
		if result.Replay {
			status = 200
		}
		JSON(w, status, result)
	})
}
