package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckLinks(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.Method+" "+r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusMovedPermanently)
		case "/head-not-supported":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		case "/head-not-implemented":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotImplemented)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	input := []Place{
		{Slug: "first", Links: []Link{{URL: server.URL + "/ok"}, {URL: server.URL + "/missing"}, {URL: server.URL + "/head-not-supported"}, {URL: server.URL + "/redirect"}, {URL: server.URL + "/head-not-implemented"}}},
		{Slug: "second", Links: []Link{{URL: server.URL + "/missing"}, {URL: server.URL + "/forbidden"}}},
	}
	warnings := CheckLinks(context.Background(), input, server.Client(), time.Second)
	if len(warnings) != 3 {
		t.Fatalf("warnings=%+v", warnings)
	}
	if warnings[0].PlaceSlug != "first" || warnings[1].PlaceSlug != "second" || !strings.Contains(warnings[2].Reason, "403") {
		t.Fatalf("wrong warning attribution or order: %+v", warnings)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["HEAD /missing"] != 1 {
		t.Fatal("shared URL was requested more than once")
	}
	if requests["GET /head-not-supported"] != 1 || requests["GET /head-not-implemented"] != 1 {
		t.Fatal("HEAD fallback did not use GET")
	}
	if requests["GET /missing"] != 0 {
		t.Fatal("404 should not be retried as GET")
	}
}

func TestCheckLinksTimeoutAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer server.Close()
	input := []Place{{Slug: "slow", Links: []Link{{URL: server.URL}}}}
	warnings := CheckLinks(context.Background(), input, server.Client(), 20*time.Millisecond)
	if len(warnings) != 1 {
		t.Fatal("timeout must produce a warning")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	warnings = CheckLinks(ctx, input, server.Client(), time.Second)
	if len(warnings) != 1 || !strings.Contains(warnings[0].Reason, "context canceled") {
		t.Fatalf("cancellation was ignored: %+v", warnings)
	}
}

func TestCheckLinksBoundsConcurrency(t *testing.T) {
	var mu sync.Mutex
	active, maximum := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	input := []Place{{Slug: "many"}}
	for i := 0; i < 12; i++ {
		input[0].Links = append(input[0].Links, Link{URL: server.URL + "/" + string(rune('a'+i))})
	}
	if warnings := CheckLinks(context.Background(), input, server.Client(), time.Second); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if maximum > 4 {
		t.Fatalf("too many concurrent requests: %d", maximum)
	}
}
