package apihttp

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/media"
	"github.com/yourname/vandrouka/backend/internal/quests"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

func (h *questHandler) createAttempt(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	var input struct {
		TaskID string `json:"task_id"`
	}
	if !readQuestJSON(w, r, &input) {
		return
	}
	task, ok := questID(input.TaskID)
	if !ok {
		Error(w, 400, "invalid_request", "Укажите действительный task_id")
		return
	}
	value, err := h.db.CreateAttempt(r.Context(), actor, id, task)
	questResult(w, 200, value, err)
}

func (h *questHandler) attempt(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.GetAttempt(r.Context(), actor, id)
	questResult(w, 200, value, err)
}

func (h *questHandler) submitAttempt(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	received := time.Now().UTC()
	keys := r.Header.Values("Idempotency-Key")
	var key string
	var valid bool
	if len(keys) == 1 {
		key, valid = questID(keys[0])
	}
	if !valid {
		Error(w, 400, "invalid_request", "Требуется один Idempotency-Key UUID")
		return
	}
	var input struct {
		Location *struct {
			Lat        *float64  `json:"lat"`
			Lon        *float64  `json:"lon"`
			AccuracyM  *float64  `json:"accuracy_m"`
			ObservedAt time.Time `json:"observed_at"`
		} `json:"location"`
	}
	if !readQuestJSON(w, r, &input) {
		return
	}
	l := input.Location
	if l == nil || l.Lat == nil || l.Lon == nil || l.AccuracyM == nil {
		Error(w, 400, "invalid_request", "Требуются координаты и точность GPS")
		return
	}
	value, err := h.db.Submit(r.Context(), actor, id, key, visits.Location{
		Lat: *l.Lat, Lon: *l.Lon, AccuracyM: *l.AccuracyM, ObservedAt: l.ObservedAt,
	}, received)
	questResult(w, 200, value, err)
}

func (h *questHandler) withdrawAttempt(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.Withdraw(r.Context(), actor, id)
	questResult(w, 200, value, err)
}

func (h *questHandler) reviewQueue(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	if cursor, ok := questCursor(w, r); ok {
		value, err := h.db.ReviewQueue(r.Context(), actor, id, cursor)
		questResult(w, 200, value, err)
	}
}

func (h *questHandler) reviewAttempt(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	var input quests.ReviewInput
	if !readQuestJSON(w, r, &input) || !validRevision(w, input.ExpectedRevision) {
		return
	}
	value, err := h.db.Review(r.Context(), actor, id, input)
	questResult(w, 200, value, err)
}

func (h *mediaHandler) RegisterQuestPhotos(r chi.Router, db quests.Postgres, service *media.Service) {
	const path = "/v1/quest-attempts/{id}/photo"
	registerQuestEndpoint(r, h.authentication, "PUT", path, "id", func(w http.ResponseWriter, r *http.Request, actor int64, id string) {
		if !h.admit(w, actor, true) {
			return
		}
		defer func() { <-h.slots }()
		_, file, mime, err := readPhotoFormFields(w, r, false)
		if err != nil {
			mediaFailure(w, err)
			return
		}
		value, err := db.UploadPhoto(r.Context(), actor, id, file, mime, service)
		if errors.Is(err, media.ErrUnavailable) || errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrInvalid) ||
			errors.Is(err, media.ErrUnsupported) || errors.Is(err, media.ErrTooLarge) || errors.Is(err, media.ErrAnonymizedImageTooLarge) ||
			errors.Is(err, media.ErrAnonymizerUnavailable) || errors.Is(err, media.ErrAnonymizerBusy) {
			mediaFailure(w, err)
			return
		}
		questResult(w, 200, value, err)
	})

	registerQuestEndpoint(r, h.authentication, "GET", path, "id", func(w http.ResponseWriter, r *http.Request, actor int64, id string) {
		if !h.admit(w, actor, false) {
			return
		}
		defer func() { <-h.slots }()
		allowed, err := db.PhotoForActor(r.Context(), actor, id)
		if err != nil {
			questResult(w, 200, nil, err)
			return
		}
		photo, data, err := service.ReadAuthorized(r.Context(), allowed)
		if err != nil {
			mediaFailure(w, err)
			return
		}
		writePrivatePhoto(w, photo, data)
	})
}
