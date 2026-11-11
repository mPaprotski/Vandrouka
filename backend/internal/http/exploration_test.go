package apihttp

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/exploration"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeExploration struct {
	owner int64
	err   error
}

func (f *fakeExploration) Read(_ context.Context, owner int64) (exploration.State, error) {
	f.owner = owner
	return exploration.State{Achievements: exploration.Definitions(0)}, f.err
}
func TestExplorationPrivateHTTP(t *testing.T) {
	for _, tc := range []struct {
		token  string
		err    error
		status int
	}{{"valid", nil, 200}, {"bad", nil, 401}, {"valid", errors.New("private sql credentials"), 500}} {
		f := &fakeExploration{err: tc.err}
		r := chi.NewRouter()
		r.Use(AuthResponses(time.Second))
		RegisterExploration(r, fakeAuth{}, f)
		req := httptest.NewRequest("GET", "/v1/me/exploration?user_id=999", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Header())
		}
		if tc.status == 401 && f.owner != 0 || tc.status != 401 && f.owner != 1 {
			t.Fatal("wrong owner")
		}
		if strings.Contains(w.Body.String(), "private sql") {
			t.Fatal("leak")
		}
	}
}
