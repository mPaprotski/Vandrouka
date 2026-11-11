package apihttp

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/progress"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeProgress struct {
	owner int64
	err   error
}

func (f *fakeProgress) Read(_ context.Context, id int64) (progress.State, error) {
	f.owner = id
	s, _ := progress.Calculate(300)
	return s, f.err
}
func TestProgressHTTP(t *testing.T) {
	for _, tc := range []struct {
		token  string
		err    error
		status int
	}{{"valid", nil, 200}, {"bad", nil, 401}, {"valid", errors.New("secret database details"), 500}} {
		service := &fakeProgress{err: tc.err}
		r := chi.NewRouter()
		r.Use(AuthResponses(time.Second))
		RegisterProgress(r, fakeAuth{}, service)
		req := httptest.NewRequest("GET", "/v1/me/progress?user_id=999", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Header())
		}
		if tc.status != 401 && service.owner != 1 {
			t.Fatal("client selected owner")
		}
		if tc.status == 401 && service.owner != 0 {
			t.Fatal("unauthorized read")
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("database details leaked")
		}
		if tc.status == 200 && !strings.Contains(w.Body.String(), `"total_xp":300`) {
			t.Fatal(w.Body)
		}
	}
	if endpointTemplate("/v1/me/progress") != "/v1/me/progress" {
		t.Fatal("log template")
	}
}
