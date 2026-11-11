package apihttp

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestVisitLimits(t *testing.T) {
	l := newVisitLimits()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for i := 0; i < 30; i++ {
		if !l.admit(httptest.NewRecorder(), 1) {
			t.Fatal("early limit", i)
		}
		<-l.slots
	}
	blocked := httptest.NewRecorder()
	if l.admit(blocked, 1) || blocked.Code != 429 || blocked.Header().Get("Retry-After") != "60" {
		t.Fatal("missing owner limit")
	}
	if !l.admit(httptest.NewRecorder(), 2) {
		t.Fatal("other owner blocked")
	}
	<-l.slots
	now = now.Add(time.Minute)
	if !l.admit(httptest.NewRecorder(), 1) {
		t.Fatal("window did not expire")
	}
	<-l.slots
	for i := 0; i < 4; i++ {
		if !l.admit(httptest.NewRecorder(), int64(i+10)) {
			t.Fatal("early concurrent limit")
		}
	}
	blocked = httptest.NewRecorder()
	if l.admit(blocked, 99) || blocked.Code != 429 || blocked.Header().Get("Retry-After") != "1" {
		t.Fatal("missing concurrency limit")
	}
	<-l.slots
	if !l.admit(httptest.NewRecorder(), 99) {
		t.Fatal("slot not released")
	}
}
func TestVisitLimitMapBound(t *testing.T) {
	l := newVisitLimits()
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := int64(1); i <= 4096; i++ {
		l.windows[i] = authWindow{expires: now.Add(time.Minute)}
	}
	w := httptest.NewRecorder()
	if l.admit(w, 5000) || w.Code != 429 || len(l.windows) != 4096 {
		t.Fatal("map not bounded")
	}
	if !l.admit(httptest.NewRecorder(), 1) {
		t.Fatal("existing owner denied")
	}
	<-l.slots
	now = now.Add(time.Minute)
	if !l.admit(httptest.NewRecorder(), 5000) || len(l.windows) != 1 {
		t.Fatal("expired entries retained")
	}
	<-l.slots
}
