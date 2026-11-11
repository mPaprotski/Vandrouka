package apihttp

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/auth"
	"github.com/yourname/vandrouka/backend/internal/history"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeDeletions struct {
	owner, id int64
	password  string
	err       error
	calls     int
}

func (f *fakeDeletions) DeleteAccount(_ context.Context, owner int64, password string) error {
	f.owner = owner
	f.password = password
	f.calls++
	return f.err
}
func (f *fakeDeletions) Hide(_ context.Context, owner, id int64) error {
	f.owner = owner
	f.id = id
	f.calls++
	return f.err
}
func TestDeletionsHTTP(t *testing.T) {
	for _, tc := range []struct {
		path, body, token string
		err               error
		status            int
	}{
		{"/v1/me", `{"password":"current password"}`, "valid", nil, 204},
		{"/v1/me", `{"password":"wrong password"}`, "valid", auth.ErrCredentials, 403},
		{"/v1/me", `{"password":"secret","user_id":2}`, "valid", nil, 400},
		{"/v1/me", `{"password":"secret"} {}`, "valid", nil, 400},
		{"/v1/me", `{}`, "bad", nil, 401},
		{"/v1/users/me/visits/42", "", "valid", nil, 204},
		{"/v1/users/me/visits/42", "", "valid", history.ErrNotFound, 404},
		{"/v1/users/me/visits/0", "", "valid", nil, 400},
		{"/v1/users/me/visits/42", "", "bad", nil, 401},
		{"/v1/me", `{"password":"secret"}`, "valid", errors.New("private database secret"), 500},
	} {
		f := &fakeDeletions{err: tc.err}
		r := chi.NewRouter()
		r.Use(AuthResponses(time.Second))
		RegisterDeletions(r, fakeAuth{}, f, f)
		req := httptest.NewRequest("DELETE", tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(tc, w.Code, w.Body)
		}
		if f.calls > 0 && f.owner != 1 {
			t.Fatal("wrong owner")
		}
		if tc.token == "bad" && f.calls > 0 {
			t.Fatal("unauthorized deletion")
		}
		if strings.Contains(w.Body.String(), "private database") {
			t.Fatal("error leaked")
		}
	}
}
