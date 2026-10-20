package apihttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"github.com/yourname/vandrouka/backend/internal/routes"
)

func TestRequestLoggingCorrelationAndPrivacy(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	handler := RequestLogging(RouteResponses(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestlog.SetStage(r.Context(), "directions")
		writeFailure(w, fmt.Errorf("%w: private-token", routes.ErrRoutingUnavailable), generateErrors)
	})))
	req := httptest.NewRequest("POST", "/v1/routes/generate?access_token=private-token&lat=53.903333", strings.NewReader("private-body"))
	req.Header.Set("X-Request-ID", "untrusted-private-id")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	id := w.Header().Get("X-Request-ID")
	if len(id) != 32 || id == "untrusted-private-id" || w.Code != 503 {
		t.Fatal("request ID/status")
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logs %s", logs.String())
	}
	for _, line := range lines {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil || record["request_id"] != id {
			t.Fatalf("correlation %s", line)
		}
	}
	var summary map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &summary)
	if summary["method"] != "POST" || summary["path"] != "/v1/routes/generate" || summary["status"] != float64(503) || summary["stage"] != "directions" || summary["duration_ms"].(float64) < 0 {
		t.Fatal("request summary")
	}
	for _, secret := range []string{"private-token", "private-body", "53.903333", "untrusted-private-id", "access_token"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("logged %s", secret)
		}
	}
	next := httptest.NewRecorder()
	handler.ServeHTTP(next, req)
	if next.Header().Get("X-Request-ID") == id {
		t.Fatal("reused ID")
	}
}

func TestRequestLoggingPanicAndTimeout(t *testing.T) {
	for _, panicHandler := range []bool{false, true} {
		var logs bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
		handler := RequestLogging(RouteResponses(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestlog.SetStage(r.Context(), "matrix")
			if panicHandler {
				panic("private-panic")
			}
			<-r.Context().Done()
		})))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/v1/routes/recalculate", nil))
		slog.SetDefault(old)
		want := 503
		if panicHandler {
			want = 500
		}
		if w.Code != want || w.Header().Get("X-Request-ID") == "" || strings.Contains(logs.String(), "private-panic") || !strings.Contains(logs.String(), `"stage":"matrix"`) {
			t.Fatalf("%d %s", w.Code, logs.String())
		}
	}
}
func TestRequestLogPathTemplates(t *testing.T) {
	for path, want := range map[string]string{"/v1/places/123": "/v1/places/{id}", "/v1/cities/minsk/places": "/v1/cities/{slug}/places", "/private-token/53.9": "/unmatched", "/health": "/health"} {
		if endpointTemplate(path) != want {
			t.Fatal(path)
		}
	}
}
