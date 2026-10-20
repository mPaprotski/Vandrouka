package apihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponseGuardTimeoutAndLateWrites(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		route      bool
		status     int
		code       string
	}{
		{"catalog", "/v1/cities", false, 500, "internal"},
		{"routes", "/v1/routes/generate", true, 503, "routing_unavailable"},
		{"health", "/health", false, 503, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			finished := make(chan error, 1)
			contextCanceled := make(chan struct{})
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Partial", "must-not-escape")
				w.WriteHeader(200)
				_, _ = w.Write([]byte("partial-success"))
				<-r.Context().Done()
				close(contextCanceled)
				<-release
				w.Header().Set("X-Late", "discard")
				_, err := w.Write([]byte("late-success"))
				finished <- err
			})
			guard := CatalogResponses(20 * time.Millisecond)
			if tc.route {
				guard = RouteResponses(20 * time.Millisecond)
			}
			w := httptest.NewRecorder()
			guard(handler).ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			close(release)
			select {
			case err := <-finished:
				if !errors.Is(err, http.ErrHandlerTimeout) {
					t.Fatalf("late write %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("handler did not finish")
			}
			select {
			case <-contextCanceled:
			default:
				t.Fatal("context not canceled")
			}
			if w.Code != tc.status || w.Header().Get("X-Partial") != "" || w.Header().Get("X-Late") != "" || strings.Contains(w.Body.String(), "success") {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.code != "" {
				var e APIError
				if json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != tc.code {
					t.Fatal(w.Body.String())
				}
			} else {
				if strings.TrimSpace(w.Body.String()) != `{"status":"unavailable"}` {
					t.Fatal(w.Body.String())
				}
			}
		})
	}
}

func TestResponseGuardPanicDoesNotLeakPartialResponseOrCause(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	handler := RouteResponses(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			w.Header().Set("X-Partial", "discard")
			_, _ = w.Write([]byte("partial"))
			panic("private-token SQL")
		}
		JSON(w, 200, map[string]string{"status": "ok"})
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	var e APIError
	if w.Code != 500 || json.Unmarshal(w.Body.Bytes(), &e) != nil || e.Error.Code != "internal" || w.Header().Get("X-Partial") != "" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "partial") || strings.Contains(w.Body.String(), "private-token") || strings.Contains(logs.String(), "private-token") {
		t.Fatal("panic cause leaked")
	}
	next := httptest.NewRecorder()
	handler.ServeHTTP(next, httptest.NewRequest("GET", "/ok", nil))
	if next.Code != 200 || !strings.Contains(next.Body.String(), `"status":"ok"`) {
		t.Fatal("subsequent request failed")
	}
}

func TestResponseGuardPreservesSuccessAndHonorsParentDeadline(t *testing.T) {
	handler := RouteResponses(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Result", "yes")
		JSON(w, 201, map[string]string{"result": "ok"})
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/v1/routes/generate", nil))
	if w.Code != 201 || w.Header().Get("X-Result") != "yes" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("success changed")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/v1/routes/generate", nil).WithContext(ctx))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "routing_unavailable") {
		t.Fatalf("expired parent %d %s", w.Code, w.Body.String())
	}
}

func TestResponseGuardDoesNotStartCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler := RouteResponses(time.Second)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("canceled request reached handler") }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/route", nil).WithContext(ctx))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
