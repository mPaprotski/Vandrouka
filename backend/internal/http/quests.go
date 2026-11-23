package apihttp

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/yourname/vandrouka/backend/internal/quests"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
)

type questHandler struct {
	db          quests.Postgres
	joinMu      sync.Mutex
	joinWindows map[int64]authWindow
}

type questEndpoint func(http.ResponseWriter, *http.Request, int64, string)

func RegisterQuests(r chi.Router, authentication AuthService, db quests.Postgres) {
	h := &questHandler{db: db, joinWindows: make(map[int64]authWindow)}
	register := func(method, path, param string, handle questEndpoint) {
		registerQuestEndpoint(r, authentication, method, path, param, handle)
	}
	register("POST", "/v1/quests", "", h.create)
	register("GET", "/v1/quests/mine", "", h.list)
	register("GET", "/v1/quests/{quest_id}/editor", "quest_id", h.editor)
	register("PUT", "/v1/quests/{quest_id}", "quest_id", h.save)
	register("POST", "/v1/quests/{quest_id}/publish", "quest_id", h.publish)
	register("POST", "/v1/quests/{quest_id}/archive", "quest_id", h.archive)
	register("DELETE", "/v1/quests/{quest_id}", "quest_id", h.remove)
	register("POST", "/v1/quests/{quest_id}/runs", "quest_id", h.createRun)
	register("GET", "/v1/quest-runs/mine", "", h.runs)
	register("GET", "/v1/quest-runs/{run_id}/manage", "run_id", h.run)
	register("POST", "/v1/quest-runs/{run_id}/start", "run_id", h.start)
	register("POST", "/v1/quest-runs/{run_id}/finish", "run_id", h.finish)
	register("POST", "/v1/quest-runs/{run_id}/join-code", "run_id", h.rotate)
	register("PATCH", "/v1/quest-runs/{run_id}/joining", "run_id", h.joining)
	register("GET", "/v1/quest-runs/{run_id}/participants", "run_id", h.participants)
	register("POST", "/v1/quest-participations/join", "", h.join)
	register("GET", "/v1/quest-participations/mine", "", h.participations)
	register("GET", "/v1/quest-participations/{id}", "id", h.progress)
	register("POST", "/v1/quest-participations/{id}/attempts", "id", h.createAttempt)
	register("GET", "/v1/quest-attempts/{id}", "id", h.attempt)
	register("POST", "/v1/quest-attempts/{id}/submit", "id", h.submitAttempt)
	register("POST", "/v1/quest-attempts/{id}/withdraw", "id", h.withdrawAttempt)
	register("GET", "/v1/quest-runs/{run_id}/reviews", "run_id", h.reviewQueue)
	register("POST", "/v1/quest-attempts/{id}/review", "id", h.reviewAttempt)

}

func registerQuestEndpoint(r chi.Router, authentication AuthService, method, path, param string, handle questEndpoint) {
	r.MethodFunc(method, path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		requestlog.SetStage(r.Context(), "quests")
		user, ok := authenticateRequest(w, r, authentication)
		if !ok {
			return
		}
		var id string
		if param != "" {
			id, ok = questID(chi.URLParam(r, param))
			if !ok {
				Error(w, 400, "invalid_request", "Некорректный идентификатор")
				return
			}
		}
		handle(w, r, user.ID, id)
	})
}

func questID(raw string) (string, bool) {
	id, err := uuid.Parse(raw)
	return id.String(), err == nil && id != uuid.Nil && len(raw) == 36
}

func questCursor(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return uuid.Nil.String(), true
	}
	cursor, ok := questID(raw)
	if !ok {
		Error(w, 400, "invalid_request", "Некорректный курсор списка")
	}
	return cursor, ok
}

func readQuestJSON(w http.ResponseWriter, r *http.Request, input any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		Error(w, 415, "unsupported_media_type", "Требуется application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 768<<10)
	reader := json.NewDecoder(r.Body)
	reader.DisallowUnknownFields()
	if err = reader.Decode(input); err == nil {
		var extra any
		err = reader.Decode(&extra)
		if err == io.EOF {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		Error(w, 413, "request_too_large", "Слишком большой запрос")
	} else {
		Error(w, 400, "invalid_request", "Некорректные параметры квеста")
	}
	return false
}

func questResult(w http.ResponseWriter, status int, result any, err error) {
	if err == nil {
		if status == http.StatusNoContent {
			w.WriteHeader(status)
		} else {
			JSON(w, status, result)
		}
		return
	}
	var invalid *quests.Invalid
	var rejection *quests.LocationRejection
	switch {
	case errors.As(err, &invalid):
		JSON(w, 422, map[string]any{"error": map[string]string{
			"code": "invalid_quest", "message": invalid.Message, "field": invalid.Field,
		}})
	case errors.As(err, &rejection):
		Error(w, 422, string(rejection.Code), visitRejectionMessage(rejection.Code))
	case errors.Is(err, quests.ErrPhotoExpired):
		Error(w, 409, "quest_photo_expired", "Срок проверки истёк. Вы можете сделать новый снимок, пока прогулка открыта")
	case errors.Is(err, quests.ErrPhotoNotReady):
		Error(w, 409, "photo_not_ready", "Загрузите обработанный снимок")
	case errors.Is(err, quests.ErrUploadInProgress):
		w.Header().Set("Retry-After", "2")
		Error(w, 409, "photo_upload_in_progress", "Снимок ещё обрабатывается")
	case errors.Is(err, quests.ErrUploadConflict):
		Error(w, 409, "photo_upload_conflict", "Для этой попытки уже сохранён другой снимок")
	case errors.Is(err, quests.ErrSubmissionConflict):
		Error(w, 409, "idempotency_conflict", "Ключ отправки уже использован с другими данными")

	case errors.Is(err, quests.ErrInvite):
		Error(w, 404, "invite_unavailable", "Код неверен или подключение закрыто")
	case errors.Is(err, quests.ErrNotFound):
		Error(w, 404, "not_found", "Квест или прохождение не найдено")
	case errors.Is(err, quests.ErrState):
		Error(w, 409, "quest_state_conflict", "Действие недоступно в текущем состоянии квеста")
	case errors.Is(err, quests.ErrRevision):
		Error(w, 409, "quest_revision_conflict", "Квест изменился. Загрузите актуальную редакцию")
	default:
		writeFailure(w, err, "quests")
	}
}

func validRevision(w http.ResponseWriter, revision int64) bool {
	if revision < 1 {
		Error(w, 400, "invalid_request", "Укажите expected_revision")
		return false
	}
	return true
}

func readRevision(w http.ResponseWriter, r *http.Request) (int64, bool) {
	var input struct {
		Revision int64 `json:"expected_revision"`
	}
	ok := readQuestJSON(w, r, &input) && validRevision(w, input.Revision)
	return input.Revision, ok
}

func (h *questHandler) create(w http.ResponseWriter, r *http.Request, actor int64, _ string) {
	var input struct {
		CityID int64 `json:"city_id"`
	}
	if !readQuestJSON(w, r, &input) {
		return
	}
	value, err := h.db.Create(r.Context(), actor, input.CityID)
	questResult(w, 201, value, err)
}

func (h *questHandler) list(w http.ResponseWriter, r *http.Request, actor int64, _ string) {
	if cursor, ok := questCursor(w, r); ok {
		value, err := h.db.List(r.Context(), actor, cursor)
		questResult(w, 200, value, err)
	}
}

func (h *questHandler) editor(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.Get(r.Context(), actor, id)
	questResult(w, 200, value, err)
}

func (h *questHandler) save(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	var input quests.SaveInput
	if !readQuestJSON(w, r, &input) || !validRevision(w, input.ExpectedRevision) {
		return
	}
	value, err := h.db.Save(r.Context(), actor, id, input)
	questResult(w, 200, value, err)
}

func (h *questHandler) publish(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	if revision, ok := readRevision(w, r); ok {
		value, err := h.db.Publish(r.Context(), actor, id, revision)
		questResult(w, 200, value, err)
	}
}

func (h *questHandler) archive(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	if revision, ok := readRevision(w, r); ok {
		value, err := h.db.Archive(r.Context(), actor, id, revision)
		questResult(w, 200, value, err)
	}
}

func (h *questHandler) remove(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	revision, _ := strconv.ParseInt(r.URL.Query().Get("expected_revision"), 10, 64)
	if validRevision(w, revision) {
		questResult(w, 204, nil, h.db.Delete(r.Context(), actor, id, revision))
	}
}

func (h *questHandler) createRun(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	var input struct {
		Title    string `json:"title"`
		Revision int64  `json:"expected_revision"`
	}
	if !readQuestJSON(w, r, &input) || !validRevision(w, input.Revision) {
		return
	}
	value, err := h.db.CreateRun(r.Context(), actor, id, input.Revision, input.Title)
	questResult(w, 201, value, err)
}

func (h *questHandler) runs(w http.ResponseWriter, r *http.Request, actor int64, _ string) {
	if cursor, ok := questCursor(w, r); ok {
		value, err := h.db.ListRuns(r.Context(), actor, cursor)
		questResult(w, 200, value, err)
	}
}

func (h *questHandler) run(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.GetRun(r.Context(), actor, id)
	questResult(w, 200, value, err)
}

func (h *questHandler) start(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.Start(r.Context(), actor, id)
	questResult(w, 200, value, err)
}

func (h *questHandler) finish(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.Finish(r.Context(), actor, id)
	questResult(w, 200, value, err)
}

func (h *questHandler) rotate(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.RotateCode(r.Context(), actor, id)
	questResult(w, 200, value, err)
}

func (h *questHandler) joining(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	var input struct {
		Enabled *bool `json:"join_enabled"`
	}
	if !readQuestJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		Error(w, 400, "invalid_request", "Укажите join_enabled")
		return
	}
	value, err := h.db.SetJoining(r.Context(), actor, id, *input.Enabled)
	questResult(w, 200, value, err)
}

func (h *questHandler) participants(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	if cursor, ok := questCursor(w, r); ok {
		value, err := h.db.ListParticipants(r.Context(), actor, id, cursor)
		questResult(w, 200, value, err)
	}
}

func (h *questHandler) allowJoin(actor int64) bool {
	h.joinMu.Lock()
	defer h.joinMu.Unlock()
	now := time.Now()
	for id, window := range h.joinWindows {
		if !now.Before(window.expires) {
			delete(h.joinWindows, id)
		}
	}
	window, exists := h.joinWindows[actor]
	if !exists {
		if len(h.joinWindows) >= 4096 {
			return false
		}
		window.expires = now.Add(time.Minute)
	}
	if window.count >= 10 {
		return false
	}
	window.count++
	h.joinWindows[actor] = window
	return true
}

func (h *questHandler) join(w http.ResponseWriter, r *http.Request, actor int64, _ string) {
	if !h.allowJoin(actor) {
		limitedAuth(w)
		return
	}
	var input struct {
		Code        string `json:"code"`
		DisplayName string `json:"display_name"`
	}
	if !readQuestJSON(w, r, &input) {
		return
	}
	value, err := h.db.Join(r.Context(), actor, input.Code, input.DisplayName)
	questResult(w, 200, value, err)
}

func (h *questHandler) participations(w http.ResponseWriter, r *http.Request, actor int64, _ string) {
	if cursor, ok := questCursor(w, r); ok {
		value, err := h.db.ListParticipations(r.Context(), actor, cursor)
		questResult(w, 200, value, err)
	}
}

func (h *questHandler) progress(w http.ResponseWriter, r *http.Request, actor int64, id string) {
	value, err := h.db.GetParticipation(r.Context(), actor, id)
	questResult(w, 200, value, err)
}
