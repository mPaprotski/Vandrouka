package apihttp

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/history"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeHistory struct {
	owner int64
	limit int
	err   error
}

func (f *fakeHistory) List(_ context.Context, owner int64, limit int, cursor string) (history.Page, error) {
	f.owner = owner
	f.limit = limit
	return history.Page{Visits: []history.Item{}}, f.err
}
func TestHistoryHTTP(t *testing.T) {
	for _, tc := range []struct {
		query, token  string
		err           error
		status, limit int
	}{
		{"", "valid", nil, 200, 20}, {"?limit=50&cursor=abc", "valid", nil, 200, 50}, {"", "bad", nil, 401, 0},
		{"?limit=0", "valid", nil, 400, 0}, {"?limit=51", "valid", nil, 400, 0}, {"?limit=1&limit=2", "valid", nil, 400, 0}, {"?user_id=2", "valid", nil, 400, 0}, {"?cursor=a&cursor=b", "valid", nil, 400, 0},
		{"?cursor=bad", "valid", history.ErrInvalid, 400, 20}, {"", "valid", errors.New("secret SQL"), 500, 20},
	} {
		f := &fakeHistory{err: tc.err}
		r := chi.NewRouter()
		r.Use(AuthResponses(time.Second))
		RegisterHistory(r, fakeAuth{}, f)
		req := httptest.NewRequest("GET", "/v1/users/me/visits"+tc.query, nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status || f.limit != tc.limit || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(tc, w.Code, f.limit, w.Header())
		}
		if f.limit > 0 && f.owner != 1 {
			t.Fatal("wrong owner")
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("leak")
		}
	}
}
