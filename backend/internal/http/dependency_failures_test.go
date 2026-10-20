package apihttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/places"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
	"github.com/yourname/vandrouka/backend/internal/store"
)

type oneMuseumCatalog struct{ httpCatalog }

func (c oneMuseumCatalog) ListActivePlaces(ctx context.Context, city string) ([]places.Place, error) {
	cards, err := c.httpCatalog.ListActivePlaces(ctx, city)
	return cards[:1], err
}

const dependencyRequest = `{"start":{"lat":53.9,"lon":27.5},"start_at":"2026-10-24T10:00:00+03:00","budget_minutes":30}`

func assertDependencyError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var result APIError
	if w.Code != status || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Error.Code != code || strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "access_token") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "variants") {
		t.Fatal("outage became successful empty route")
	}
}

func TestMapboxFailuresThroughHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, stage                   string
		status                        int
		malformed, hung, missingToken bool
	}{
		{name: "unauthorized", stage: "matrix", status: 401}, {name: "forbidden", stage: "matrix", status: 403},
		{name: "quota", stage: "matrix", status: 429}, {name: "server failure", stage: "matrix", status: 500},
		{name: "missing token", missingToken: true}, {name: "invalid Matrix JSON", stage: "matrix", malformed: true},
		{name: "hung Matrix", stage: "matrix", hung: true},
		{name: "Directions outage after Matrix", stage: "directions", status: 503},
		{name: "corrupt Directions", stage: "directions", malformed: true},
		{name: "hung Directions", stage: "directions", hung: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var matrixCalls, directionCalls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				stage := "directions"
				if strings.Contains(r.URL.Path, "directions-matrix") {
					stage = "matrix"
					matrixCalls.Add(1)
				} else {
					directionCalls.Add(1)
				}
				if stage == tc.stage {
					if tc.hung {
						<-r.Context().Done()
						return
					}
					if tc.malformed {
						_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"geometry":"test","legs":[]}]}`))
						return
					}
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write([]byte(`{"code":"Ok","durations":[[0,60],[60,0]],"distances":[[0,100],[100,0]]}`))
			}))
			defer provider.Close()
			token := "private-token"
			if tc.missingToken {
				token = ""
			}
			client := routing.NewMapboxClient(provider.URL, token, 150*time.Millisecond)
			service := routes.NewService(oneMuseumCatalog{}, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(client)), routes.WithDirections(client))
			router := chi.NewRouter()
			router.Use(RequestLogging, RouteResponses(3*time.Second))
			RegisterRoutes(router, service)
			req := httptest.NewRequest("POST", "/v1/routes/generate", strings.NewReader(dependencyRequest))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assertDependencyError(t, w, 503, "routing_unavailable")
			expected := int32(1)
			if tc.missingToken {
				expected = 0
			}
			if matrixCalls.Load() != expected {
				t.Fatal("unexpected Matrix retries")
			}
			if tc.stage == "directions" {
				if directionCalls.Load() != 1 {
					t.Fatal("unexpected Directions retries")
				}
			} else if directionCalls.Load() != 0 {
				t.Fatal("continued after Matrix failure")
			}
		})
	}
}

func TestClientCancellationStopsMapboxWork(t *testing.T) {
	started := make(chan struct{})
	disconnected := make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-r.Context().Done()
		close(disconnected)
	}))
	defer provider.Close()
	client := routing.NewMapboxClient(provider.URL, "private-token", time.Second)
	service := routes.NewService(oneMuseumCatalog{}, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(client)), routes.WithDirections(client))
	router := chi.NewRouter()
	router.Use(RouteResponses(time.Second))
	RegisterRoutes(router, service)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/v1/routes/generate", strings.NewReader(dependencyRequest)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	finished := make(chan struct{})
	w := httptest.NewRecorder()
	go func() { router.ServeHTTP(w, req); close(finished) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider never called")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request did not stop")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("provider context not canceled")
	}
	assertDependencyError(t, w, 503, "routing_unavailable")
	if calls.Load() != 1 {
		t.Fatal("continued provider work")
	}
}

func TestClosedPostgresPoolReturnsInternalWithoutRouting(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	catalog := store.NewCatalog(pool)
	provider := &httpRouting{}
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(provider)), routes.WithDirections(provider))
	router := chi.NewRouter()
	router.Use(RequestLogging)
	RegisterCatalog(router, catalog)
	RegisterRoutes(router, service)
	for _, path := range []string{"/v1/cities", "/v1/routes/generate"} {
		method := "GET"
		body := ""
		if strings.Contains(path, "routes") {
			method = "POST"
			body = dependencyRequest
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assertDependencyError(t, w, 500, "internal")
	}
	if provider.directions != 0 {
		t.Fatal("routing called after DB failure")
	}
}

func TestRecalculationOutageAfterVerifiedBaseline(t *testing.T) {
	var matrices, directions atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "directions-matrix") {
			matrices.Add(1)
			_, _ = w.Write([]byte(`{"code":"Ok","durations":[[0,60,60],[60,0,60],[60,60,0]],"distances":[[0,100,100],[100,0,100],[100,100,0]]}`))
			return
		}
		if directions.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"geometry":"test","duration":60,"distance":100,"legs":[{"duration":60,"distance":100}]}]}`))
			return
		}
		w.WriteHeader(503)
	}))
	defer provider.Close()
	client := routing.NewMapboxClient(provider.URL, "private-token", time.Second)
	service := routes.NewService(httpCatalog{}, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(client)), routes.WithDirections(client))
	router := chi.NewRouter()
	router.Use(RouteResponses(5 * time.Second))
	RegisterRecalculation(router, service)
	req := httptest.NewRequest("POST", "/v1/routes/recalculate", strings.NewReader(`{"request":{"start":{"lat":53.9,"lon":27.5},"start_at":"2026-10-24T10:00:00+03:00","budget_minutes":27,"include_geometry":false},"base_place_ids":[1],"change":{"type":"add","place_id":2}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assertDependencyError(t, w, 503, "routing_unavailable")
	if matrices.Load() != 1 || directions.Load() != 2 || strings.Contains(w.Body.String(), `"diff"`) {
		t.Fatalf("partial success/retry: %s", w.Body.String())
	}
}
