package apihttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func limits(t *testing.T) *RouteLimiter {
	t.Helper()
	l, err := NewRouteLimiter(RouteLimitOptions{RequestsPerMinute: 6, Burst: 2, MaxConcurrent: 2, MaxClients: 3, IdleTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func TestRouteLimitRefillAndBoundedClients(t *testing.T) {
	l := limits(t)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		if retry := l.admit("a"); retry != 0 {
			t.Fatal(retry)
		}
		<-l.slots
	}
	if retry := l.admit("a"); retry != 10 {
		t.Fatalf("retry %d", retry)
	}
	now = now.Add(9 * time.Second)
	if retry := l.admit("a"); retry != 1 {
		t.Fatal(retry)
	}
	now = now.Add(time.Second)
	if retry := l.admit("a"); retry != 0 {
		t.Fatal(retry)
	}
	<-l.slots
	for _, key := range []string{"b", "c"} {
		if l.admit(key) != 0 {
			t.Fatal("unexpected rejection")
		}
		<-l.slots
	}
	if l.admit("d") != 60 || len(l.clients) != 3 {
		t.Fatal("unbounded client map")
	}
	now = now.Add(time.Minute)
	if l.admit("d") != 0 || len(l.clients) != 1 {
		t.Fatal("idle cleanup")
	}
	<-l.slots
}
func TestRouteLimitUsesPeerIPAndSharesRateAcrossEndpoints(t *testing.T) {
	l := limits(t)
	calls := 0
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; JSON(w, 200, map[string]string{"ok": "yes"}) }))
	for i, path := range []string{"/v1/routes/generate", "/v1/routes/recalculate", "/v1/routes/generate"} {
		req := httptest.NewRequest("POST", path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", path)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if i < 2 && w.Code != 200 {
			t.Fatal(w.Code)
		}
		if i == 2 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatal("rate response")
		}
	}
	if calls != 2 || clientAddress("[::ffff:127.0.0.1]:1234") != "127.0.0.1" || clientAddress("not-IP") != "unknown" {
		t.Fatal("peer identities")
	}
}
func TestRouteConcurrencyAndSlotRelease(t *testing.T) {
	l, err := NewRouteLimiter(RouteLimitOptions{RequestsPerMinute: 600, Burst: 100, MaxConcurrent: 2, MaxClients: 100, IdleTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	const count = 40
	var wg sync.WaitGroup
	wg.Add(count)
	var mu sync.Mutex
	var active, maxActive, accepted int
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		accepted++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		Error(w, 500, "internal", "test")
	}))
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/route", nil)
			req.RemoteAddr = "127.0.0.1:1000"
			h.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()
	if accepted == 0 || maxActive > 2 || len(l.slots) != 0 {
		t.Fatal("concurrency leaked")
	}
}
func TestSlotRemainsUntilTimedOutWorkerFinishes(t *testing.T) {
	l := limits(t)
	l.slots = make(chan struct{}, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	h := RouteResponses(10 * time.Millisecond)(l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			defer close(finished)
			<-release
		}
		JSON(w, 200, map[string]string{"ok": "yes"})
	})))
	req := httptest.NewRequest("POST", "/blocked", nil)
	req.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 503 || len(l.slots) != 1 {
		t.Fatal("timeout released active work")
	}
	next := httptest.NewRequest("POST", "/other", nil)
	next.RemoteAddr = "127.0.0.2:1"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, next)
	if w.Code != 429 {
		t.Fatal("overlap admitted")
	}
	close(release)
	<-finished
	// finished closes before the limiter's defer, so wait on the slot under lock-free channel inspection.
	deadline := time.Now().Add(time.Second)
	for len(l.slots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, next)
	if w.Code != 200 || len(l.slots) != 0 {
		t.Fatal("slot not released")
	}
}
func TestRouteLimitPanicAndCancellation(t *testing.T) {
	l := limits(t)
	h := RouteResponses(time.Second)(l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			panic("test")
		}
		JSON(w, 200, map[string]bool{"ok": true})
	})))
	req := httptest.NewRequest("POST", "/panic", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 500 || len(l.slots) != 0 {
		t.Fatal("panic leak")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/ok", nil).WithContext(ctx))
	if w.Code != 503 || len(l.slots) != 0 {
		t.Fatal("cancel leak")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/ok", nil))
	if w.Code != 200 {
		t.Fatal("next request")
	}
}
func TestRouteLimitRejectsInvalidConfiguration(t *testing.T) {
	for _, o := range []RouteLimitOptions{{}, {RequestsPerMinute: 1, Burst: 2, MaxConcurrent: 1, MaxClients: 1, IdleTTL: time.Minute}, {RequestsPerMinute: 6, Burst: 2, MaxConcurrent: 1025, MaxClients: 1, IdleTTL: time.Minute}} {
		if _, err := NewRouteLimiter(o); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
