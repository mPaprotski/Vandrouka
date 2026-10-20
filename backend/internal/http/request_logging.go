package apihttp

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
)

// RequestLogging assigns server-owned IDs and logs only safe endpoint templates.
// Query strings, bodies, arbitrary URL segments and client IDs are not logged.
func RequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			Error(w, 500, "internal", "Не удалось выполнить запрос")
			return
		}
		id := hex.EncodeToString(random[:])
		w.Header().Set("X-Request-ID", id)
		ctx := requestlog.WithTrace(r.Context())
		writer := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		started := time.Now()
		defer func() {
			status := writer.Status()
			if status == 0 {
				status = 200
			}
			slog.Info("HTTP request", "request_id", id, "method", r.Method, "path", endpointTemplate(r.URL.Path), "status", status, "duration_ms", float64(time.Since(started))/float64(time.Millisecond), "stage", requestlog.Stage(ctx))
		}()
		next.ServeHTTP(writer, r.WithContext(ctx))
	})
}

func endpointTemplate(path string) string {
	switch path {
	case "/health", "/v1/cities", "/v1/routes/generate", "/v1/routes/recalculate":
		return path
	}
	parts := strings.Split(path, "/")
	if len(parts) == 4 && parts[1] == "v1" && parts[2] == "places" {
		return "/v1/places/{id}"
	}
	if len(parts) == 5 && parts[1] == "v1" && parts[2] == "cities" && (parts[4] == "categories" || parts[4] == "places") {
		return "/v1/cities/{slug}/" + parts[4]
	}
	return "/unmatched"
}
