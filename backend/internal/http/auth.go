package apihttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/auth"
)

type AuthService interface {
	Register(context.Context, string, string) (auth.LoginResult, error)
	Login(context.Context, string, string) (auth.LoginResult, error)
	Authenticate(context.Context, string) (auth.User, error)
	Refresh(context.Context, string) (auth.LoginResult, error)
	Logout(context.Context, string) error
}
type authWindow struct {
	count   int
	expires time.Time
}
type authHandler struct {
	service AuthService
	mu      sync.Mutex
	windows map[string]authWindow
	slots   chan struct{}
	now     func() time.Time
}

func RegisterAuth(r chi.Router, service AuthService) {
	h := &authHandler{service: service, windows: make(map[string]authWindow), slots: make(chan struct{}, 2), now: time.Now}
	r.Post("/v1/auth/register", h.register)
	r.Post("/v1/auth/login", h.login)
	r.Get("/v1/me", h.me)
	r.Post("/v1/auth/refresh", h.refresh)
	r.Post("/v1/auth/logout", h.logout)
}

// Limits are per API process. Only RemoteAddr is trusted, never forwarded headers.
func (h *authHandler) allow(key string, limit int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for key, window := range h.windows {
		if !now.Before(window.expires) {
			delete(h.windows, key)
		}
	}
	window, exists := h.windows[key]
	if !exists {
		if len(h.windows) >= 4096 {
			return false
		}
		window.expires = now.Add(time.Minute)
	}
	if window.count >= limit {
		return false
	}
	window.count++
	h.windows[key] = window
	return true
}
func authFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		Error(w, 400, "invalid_request", "Некорректный email или пароль: требуется 12–128 символов")
	case errors.Is(err, auth.ErrEmailExists):
		Error(w, 409, "email_exists", "Этот email уже зарегистрирован")
	case errors.Is(err, auth.ErrCredentials):
		Error(w, 401, "invalid_credentials", "Неверный email или пароль")
	case errors.Is(err, auth.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		Error(w, 401, "unauthorized", "Требуется действующая сессия")
	default:
		failure(w, err)
	}
}
func limitedAuth(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	Error(w, 429, "rate_limited", "Слишком много попыток. Повторите через минуту")
}
func (h *authHandler) credentials(w http.ResponseWriter, r *http.Request) (string, string, func(), bool) {
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !h.allow("ip:"+host, 10) {
		limitedAuth(w)
		return "", "", nil, false
	}
	select {
	case h.slots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "1")
		Error(w, 503, "auth_busy", "Сервис входа занят. Повторите позже")
		return "", "", nil, false
	}
	release := func() { <-h.slots }
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		release()
		Error(w, 415, "unsupported_media_type", "Требуется application/json")
		return "", "", nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	err = decoder.Decode(&input)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = auth.ErrInvalidInput
		}
	}
	if err != nil {
		release()
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			Error(w, 413, "request_too_large", "Слишком большой запрос")
		} else {
			authFailure(w, auth.ErrInvalidInput)
		}
		return "", "", nil, false
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(input.Email))))
	if !h.allow("email:"+hex.EncodeToString(sum[:]), 5) {
		release()
		limitedAuth(w)
		return "", "", nil, false
	}
	return input.Email, input.Password, release, true
}
func (h *authHandler) register(w http.ResponseWriter, r *http.Request) {
	email, password, release, ok := h.credentials(w, r)
	if !ok {
		return
	}
	defer release()
	result, err := h.service.Register(r.Context(), email, password)
	if err != nil {
		authFailure(w, err)
		return
	}
	JSON(w, 201, result)
}
func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	email, password, release, ok := h.credentials(w, r)
	if !ok {
		return
	}
	defer release()
	result, err := h.service.Login(r.Context(), email, password)
	if err != nil {
		authFailure(w, err)
		return
	}
	JSON(w, 200, result)
}
func authenticateRequest(w http.ResponseWriter, r *http.Request, service AuthService) (auth.User, bool) {
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		authFailure(w, auth.ErrUnauthorized)
		return auth.User{}, false
	}
	fields := strings.Fields(headers[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		authFailure(w, auth.ErrUnauthorized)
		return auth.User{}, false
	}
	user, err := service.Authenticate(r.Context(), fields[1])
	if err != nil {
		authFailure(w, err)
		return auth.User{}, false
	}
	return user, true
}
func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, ok := authenticateRequest(w, r, h.service)
	if !ok {
		return
	}
	JSON(w, 200, map[string]auth.User{"user": user})
}

func (h *authHandler) refreshCredential(w http.ResponseWriter, r *http.Request) (string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !h.allow("session-ip:"+host, 30) {
		limitedAuth(w)
		return "", false
	}
	content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || content != "application/json" {
		Error(w, 415, "unsupported_media_type", "Требуется application/json")
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	err = decoder.Decode(&input)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = auth.ErrInvalidInput
		}
	}
	if err != nil || input.RefreshToken == "" || len(input.RefreshToken) > 128 {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			Error(w, 413, "request_too_large", "Слишком большой запрос")
		} else {
			Error(w, 400, "invalid_request", "Некорректные параметры сессии")
		}
		return "", false
	}
	return input.RefreshToken, true
}
func (h *authHandler) refresh(w http.ResponseWriter, r *http.Request) {
	token, ok := h.refreshCredential(w, r)
	if !ok {
		return
	}
	result, err := h.service.Refresh(r.Context(), token)
	if err != nil {
		authFailure(w, err)
		return
	}
	JSON(w, 200, result)
}
func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	token, ok := h.refreshCredential(w, r)
	if !ok {
		return
	}
	if err := h.service.Logout(r.Context(), token); err != nil {
		authFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func AuthResponses(timeout time.Duration) func(http.Handler) http.Handler {
	guard := CatalogResponses(timeout)
	return func(next http.Handler) http.Handler {
		wrapped := guard(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			wrapped.ServeHTTP(w, r)
		})
	}
}
