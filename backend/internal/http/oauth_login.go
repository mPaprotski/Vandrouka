package apihttp

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/auth"
	"github.com/yourname/vandrouka/backend/internal/oauthlogin"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
)

var returnPage = template.Must(template.New("return").Parse(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Вход в Вандроўку</title><p>Вернитесь в приложение, чтобы завершить вход.</p><a href="vandrouka://auth-return?attempt={{.ID}}&amp;code={{.Code}}">Вернуться в Вандроўку</a></html>`))

func oauthFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, oauthlogin.ErrInvalid):
		Error(w, 400, "invalid_request", "Некорректные параметры входа")
	case errors.Is(err, oauthlogin.ErrAttempt):
		Error(w, 401, "invalid_attempt", "Начните вход заново")
	case errors.Is(err, oauthlogin.ErrCancelled):
		Error(w, 400, "oauth_cancelled", "Вход отменён")
	case errors.Is(err, oauthlogin.ErrProvider), errors.Is(err, oauthlogin.ErrDisabled):
		Error(w, 503, "provider_unavailable", "Провайдер входа недоступен")
	case errors.Is(err, auth.ErrExternalEmailRequired):
		Error(w, 422, "provider_email_required", "Для регистрации требуется адрес почты")
	case errors.Is(err, auth.ErrExternalLinkRequired):
		Error(w, 409, "account_link_required", "Войдите в существующий профиль для привязки аккаунта")
	case errors.Is(err, auth.ErrUnauthorized):
		Error(w, 401, "unauthorized", "Сессия завершилась")
	case errors.Is(err, auth.ErrCredentials):
		Error(w, 403, "invalid_credentials", "Подтвердите текущий аккаунт")
	case errors.Is(err, auth.ErrExternalLinkConflict):
		Error(w, 409, "identity_link_conflict", "Этот способ входа уже подключён к другому аккаунту")
	default:
		failure(w, err)
	}
}

func oauthJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		Error(w, 415, "unsupported_media_type", "Требуется application/json")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(target) != nil || d.Decode(&extra) != io.EOF {
		oauthFailure(w, oauthlogin.ErrInvalid)
		return false
	}
	return true
}

func browserCookie(state, value string, age int) *http.Cookie {
	return &http.Cookie{Name: "__Host-vandrouka-" + state, Value: value, Secure: true, HttpOnly: true, Path: "/", SameSite: http.SameSiteLaxMode, MaxAge: age}
}

func RegisterOAuthLogin(r chi.Router, s *oauthlogin.Service) {
	limits := &authHandler{windows: make(map[string]authWindow), now: time.Now}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestlog.SetStage(r.Context(), "oauth_login")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
			next.ServeHTTP(w, r)
		})
	})

	r.Get("/v1/me/login-methods", func(w http.ResponseWriter, r *http.Request) {
		m, err := s.Methods(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			oauthFailure(w, err)
			return
		}
		JSON(w, 200, m)
	})
	r.Post("/v1/auth/oauth/account-attempts", func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !limits.allow("protected:"+host, 5) {
			limitedAuth(w)
			return
		}
		var in struct {
			Provider  string `json:"provider"`
			Challenge string `json:"challenge"`
			Purpose   string `json:"purpose"`
			Password  string `json:"password"`
		}
		if !oauthJSON(w, r, &in) {
			return
		}
		a, err := s.StartProtected(r.Context(), in.Provider, in.Challenge, in.Purpose, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), in.Password)
		if err != nil {
			oauthFailure(w, err)
			return
		}
		JSON(w, 201, a)
	})
	r.Post("/v1/auth/oauth/account-complete", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID      string `json:"attempt_id"`
			Code    string `json:"code"`
			Secret  string `json:"device_secret"`
			Purpose string `json:"purpose"`
		}
		if !oauthJSON(w, r, &in) {
			return
		}
		err := s.CompleteProtected(r.Context(), in.ID, in.Code, in.Secret, in.Purpose, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			oauthFailure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	r.Post("/v1/auth/oauth/attempts", func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !limits.allow("ip:"+host, 10) || !limits.allow("all", 120) {
			limitedAuth(w)
			return
		}
		var input struct {
			Provider  string `json:"provider"`
			Challenge string `json:"challenge"`
		}
		if !oauthJSON(w, r, &input) {
			return
		}
		a, err := s.Start(r.Context(), input.Provider, input.Challenge)
		if err != nil {
			oauthFailure(w, err)
			return
		}
		JSON(w, 201, a)
	})
	r.Get("/v1/auth/oauth/begin/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.Begin(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			oauthFailure(w, err)
			return
		}
		http.SetCookie(w, browserCookie(b.State, b.Cookie, 300))
		http.Redirect(w, r, b.URL, http.StatusFound)
	})
	r.Get("/v1/auth/oauth/callback/{provider}", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		state := q.Get("state")
		c, err := r.Cookie("__Host-vandrouka-" + state)
		if err != nil {
			oauthFailure(w, oauthlogin.ErrAttempt)
			return
		}
		out, err := s.Callback(r.Context(), chi.URLParam(r, "provider"), state, c.Value, q.Get("code"), q.Get("error"))
		if err != nil {
			oauthFailure(w, err)
			return
		}
		http.SetCookie(w, browserCookie(state, "", -1))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = returnPage.Execute(w, out)
	})
	r.Post("/v1/auth/oauth/complete", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID     string `json:"attempt_id"`
			Code   string `json:"code"`
			Secret string `json:"device_secret"`
		}
		if !oauthJSON(w, r, &input) {
			return
		}
		session, err := s.Complete(r.Context(), input.ID, input.Code, input.Secret)
		if err != nil {
			oauthFailure(w, err)
			return
		}
		JSON(w, 200, session)
	})
}
