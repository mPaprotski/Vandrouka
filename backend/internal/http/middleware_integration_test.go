package apihttp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// This uses the same middleware order as cmd/api, across real HTTP connections.
func TestMiddlewareOverloadKeepsCatalogAvailable(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	limiter, err := NewRouteLimiter(RouteLimitOptions{RequestsPerMinute: 600, Burst: 100, MaxConcurrent: 1, MaxClients: 10, IdleTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	router := chi.NewRouter()
	router.Use(RequestLogging)
	router.Group(func(r chi.Router) {
		r.Use(CatalogResponses(time.Second))
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"status": "ok"}) })
		r.Get("/v1/cities", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]any{"cities": []any{}}) })
	})
	router.Group(func(r chi.Router) {
		r.Use(RouteResponses(2*time.Second), limiter.Middleware)
		r.Post("/v1/routes/generate", func(w http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			JSON(w, 200, map[string]any{"variants": []any{}})
		})
		r.Post("/v1/routes/recalculate", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, map[string]string{"result": "ok"}) })
	})
	server := httptest.NewServer(router)
	defer server.Close()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	client := &http.Client{Timeout: 3 * time.Second}
	first := make(chan *http.Response, 1)
	firstErr := make(chan error, 1)
	go func() {
		response, err := client.Post(server.URL+"/v1/routes/generate", "application/json", strings.NewReader(`{}`))
		if err != nil {
			firstErr <- err
			return
		}
		first <- response
	}()
	select {
	case <-started:
	case err := <-firstErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	const count = 12
	var wg sync.WaitGroup
	wg.Add(count)
	failures := make(chan string, count)
	ids := make(chan string, count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			response, err := client.Post(server.URL+"/v1/routes/recalculate", "application/json", strings.NewReader(`{}`))
			if err != nil {
				failures <- err.Error()
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			var envelope APIError
			if err != nil || response.StatusCode != 429 || json.Unmarshal(body, &envelope) != nil || envelope.Error.Code != "rate_limited" || response.Header.Get("Retry-After") == "" || len(response.Header.Get("X-Request-ID")) != 32 {
				failures <- string(body)
			}
			ids <- response.Header.Get("X-Request-ID")
		}()
	}
	wg.Wait()
	close(failures)
	close(ids)
	for _, path := range []string{"/health", "/v1/cities"} {
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || response.Header.Get("X-Request-ID") == "" {
			t.Fatal("catalog unavailable")
		}
	}
	// Release on every subsequent exit, even if an assertion fails.
	releaseOnce.Do(func() { close(release) })
	for failure := range failures {
		t.Error(failure)
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Error("duplicate request ID")
		}
		seen[id] = true
	}
	select {
	case response := <-first:
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(response.StatusCode)
		}
	case err := <-firstErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("first request stuck")
	}

	response, err := client.Post(server.URL+"/v1/routes/recalculate", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || len(limiter.slots) != 0 {
		t.Fatal("slot leaked")
	}
	// Server close waits for logging defers before inspecting the captured log.
	server.Close()
	summaries := 0
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			t.Fatal("non-JSON log")
		}
		if entry["msg"] == "HTTP request" {
			summaries++
			if entry["request_id"] == "" || entry["duration_ms"] == nil {
				t.Fatal("missing correlation")
			}
		}
	}
	if summaries != count+4 {
		t.Fatalf("request summaries %d", summaries)
	}
}

func TestComposedMiddlewareFailureReleasesCapacity(t *testing.T) {
	for _, action := range []string{"error", "panic", "timeout"} {
		t.Run(action, func(t *testing.T) {
			limiter, err := NewRouteLimiter(RouteLimitOptions{RequestsPerMinute: 600, Burst: 100, MaxConcurrent: 1, MaxClients: 10, IdleTTL: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			handler := RequestLogging(RouteResponses(20 * time.Millisecond)(limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/error":
					Error(w, 500, "internal", "test")
				case "/panic":
					_, _ = w.Write([]byte("partial-private-token"))
					panic("private-token")
				case "/timeout":
					<-r.Context().Done()
				default:
					JSON(w, 200, map[string]bool{"ok": true})
				}
			}))))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("POST", "/"+action, nil))
			want := 500
			if action == "timeout" {
				want = 503
			}
			var envelope APIError
			if w.Code != want || json.Unmarshal(w.Body.Bytes(), &envelope) != nil || strings.Contains(w.Body.String(), "private-token") || len(w.Header().Get("X-Request-ID")) != 32 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			deadline := time.Now().Add(time.Second)
			for len(limiter.slots) != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			next := httptest.NewRecorder()
			handler.ServeHTTP(next, httptest.NewRequest("POST", "/ok", nil))
			if next.Code != 200 || len(limiter.slots) != 0 {
				t.Fatal("capacity not released")
			}
		})
	}
}
