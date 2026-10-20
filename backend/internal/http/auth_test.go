package apihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/auth"
)

type fakeAuth struct{ err error }

func (f fakeAuth) Register(context.Context, string, string) (auth.LoginResult, error) {
	return auth.LoginResult{User: auth.User{ID: 1, Email: "user@example.com"}, TokenType: "Bearer", AccessToken: "test-only-token"}, f.err
}
func (f fakeAuth) Login(ctx context.Context, email, password string) (auth.LoginResult, error) {
	return f.Register(ctx, email, password)
}
func (f fakeAuth) Authenticate(_ context.Context, token string) (auth.User, error) {
	if token != "valid" {
		return auth.User{}, auth.ErrUnauthorized
	}
	return auth.User{ID: 1, Email: "user@example.com"}, f.err
}
func authRouter(service AuthService) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestLogging, CatalogResponses(time.Second))
	RegisterAuth(r, service)
	return r
}
func authRequest(router http.Handler, method, path, body, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func TestAuthHTTP(t *testing.T) {
	body := `{"email":"user@example.com","password":"strong password 123"}`
	for _, path := range []string{"/v1/auth/register", "/v1/auth/login"} {
		rec := authRequest(authRouter(fakeAuth{}), "POST", path, body, "")
		expected := 200
		if strings.HasSuffix(path, "register") {
			expected = 201
		}
		if rec.Code != expected || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Request-ID") == "" {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	for _, entry := range []struct {
		err  error
		code int
		kind string
	}{{auth.ErrInvalidInput, 400, "invalid_request"}, {auth.ErrEmailExists, 409, "email_exists"}, {auth.ErrCredentials, 401, "invalid_credentials"}, {errors.New("postgres://secret@host"), 500, "internal"}} {
		rec := authRequest(authRouter(fakeAuth{entry.err}), "POST", "/v1/auth/login", body, "")
		if rec.Code != entry.code || !strings.Contains(rec.Body.String(), entry.kind) || strings.Contains(rec.Body.String(), "secret") {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	for _, header := range []string{"", "Basic valid", "Bearer", "Bearer valid extra", "Bearer unknown"} {
		rec := authRequest(authRouter(fakeAuth{}), "GET", "/v1/me", "", header)
		if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatal(header, rec.Code)
		}
	}
	rec := authRequest(authRouter(fakeAuth{}), "GET", "/v1/me", "", "bearer valid")
	var result struct {
		User auth.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != 200 || result.User.ID != 1 {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
func TestAuthMalformedBodies(t *testing.T) {
	for _, body := range []string{"null", "[]", "{}", "{", `{"email":"a","password":"b","extra":true}`, `{"email":"a","password":"b"} {}`, `{"email":12}`, `{"password":true}`} {
		// Missing fields and null reach service validation; use the same public error.
		rec := authRequest(authRouter(fakeAuth{auth.ErrInvalidInput}), "POST", "/v1/auth/register", body, "")
		if rec.Code != 400 {
			t.Fatal(body, rec.Code)
		}
	}
	rec := authRequest(authRouter(fakeAuth{}), "POST", "/v1/auth/login", `{"password":"`+strings.Repeat("x", 5000)+`"}`, "")
	if rec.Code != 413 {
		t.Fatal(rec.Code)
	}
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewBufferString(`{}`))
	rec = httptest.NewRecorder()
	authRouter(fakeAuth{}).ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Fatal(rec.Code)
	}
}
func TestAuthLimits(t *testing.T) {
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	h := &authHandler{windows: make(map[string]authWindow), now: func() time.Time { return now }}
	for i := 0; i < 5; i++ {
		if !h.allow("email:hash", 5) {
			t.Fatal("early limit")
		}
	}
	if h.allow("email:hash", 5) {
		t.Fatal("limit missing")
	}
	now = now.Add(time.Minute)
	if !h.allow("email:hash", 5) {
		t.Fatal("window not expired")
	}
	for i := 0; i < 4096; i++ {
		h.windows[string(rune(i))+"key"] = authWindow{expires: now.Add(time.Minute)}
	}
	if h.allow("new-client", 10) {
		t.Fatal("unbounded map")
	}
	router := authRouter(fakeAuth{})
	for i := 0; i < 5; i++ {
		rec := authRequest(router, "POST", "/v1/auth/login", `{"email":"same@example.com","password":"strong password 123"}`, "")
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
	}
	rec := authRequest(router, "POST", "/v1/auth/login", `{"email":"SAME@example.com","password":"strong password 123"}`, "")
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Fatal(rec.Code)
	}
	// A forged forwarding header cannot bypass the peer-IP limit.
	router = authRouter(fakeAuth{})
	for i := 0; i < 11; i++ {
		req := httptest.NewRequest("POST", "/v1/auth/login", strings.NewReader(`{}`))
		req.Header.Set("X-Forwarded-For", string(rune('a'+i)))
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
	if rec.Code != 429 {
		t.Fatal("IP limit bypassed", rec.Code)
	}
}

type blockedAuth struct {
	fakeAuth
	started chan struct{}
	release chan struct{}
}

func (b blockedAuth) Login(ctx context.Context, email, password string) (auth.LoginResult, error) {
	b.started <- struct{}{}
	<-b.release
	return b.fakeAuth.Login(ctx, email, password)
}
func TestAuthConcurrencyLimit(t *testing.T) {
	service := blockedAuth{started: make(chan struct{}, 2), release: make(chan struct{})}
	router := authRouter(service)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			authRequest(router, "POST", "/v1/auth/login", `{"email":"user@example.com","password":"long password 123"}`, "")
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-service.started:
		case <-time.After(time.Second):
			close(service.release)
			t.Fatal("request did not start")
		}
	}
	rec := authRequest(router, "POST", "/v1/auth/login", `{"email":"user@example.com","password":"long password 123"}`, "")
	close(service.release)
	wg.Wait()
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "auth_busy") {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
