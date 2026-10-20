package apihttp

import (
	"bytes"
	"context"
	"errors"
	"github.com/yourname/vandrouka/backend/internal/requestlog"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// CatalogResponses/RouteResponses buffer JSON endpoints until completion. This
// prevents partial success responses from escaping before a panic or deadline.
func CatalogResponses(timeout time.Duration) func(http.Handler) http.Handler {
	return responseGuard(timeout, catalogErrors)
}
func RouteResponses(timeout time.Duration) func(http.Handler) http.Handler {
	return responseGuard(timeout, generateErrors)
}

func responseGuard(timeout time.Duration, scope errorScope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			if scope == catalogErrors {
				requestlog.SetStage(ctx, "catalog")
			}
			buffered := &guardWriter{header: make(http.Header)}
			buffered.header.Set("X-Request-ID", w.Header().Get("X-Request-ID"))
			done := make(chan bool, 1)
			fail := func(err error, panicFailure bool) {
				buffered.stop()
				if panicFailure {
					slog.Error("API handler panic", "scope", string(scope), "request_id", w.Header().Get("X-Request-ID"))
				}
				// Health has its own documented envelope, rather than APIError.
				if scope == catalogErrors && r.URL.Path == "/health" {
					JSON(w, 503, map[string]string{"status": "unavailable"})
					return
				}
				writeFailure(w, err, scope)
			}
			if err := ctx.Err(); err != nil {
				fail(err, false)
				return
			}
			go func() {
				panicked := true
				defer func() {
					if panicked {
						_ = recover()
					}
					done <- panicked
				}()
				next.ServeHTTP(buffered, r.WithContext(ctx))
				panicked = false
			}()
			select {
			case panicked := <-done:
				if ctx.Err() != nil {
					fail(ctx.Err(), false)
					return
				}
				if panicked {
					fail(errHandlerPanic, true)
					return
				}
				buffered.commit(w)
			case <-ctx.Done():
				fail(ctx.Err(), false)
			}
		})
	}
}

// A static panic error prevents recovered values from disclosing secrets.
var errHandlerPanic = errors.New("handler panic")

type guardWriter struct {
	mu      sync.Mutex
	header  http.Header // accessed by handler; read by commit only after completion
	body    bytes.Buffer
	status  int
	stopped bool
}

func (w *guardWriter) Header() http.Header { return w.header }
func (w *guardWriter) WriteHeader(status int) {
	if status < 100 || status > 999 {
		panic("invalid HTTP status")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.status != 0 {
		return
	}
	if status < 200 {
		return
	} // These JSON endpoints do not emit informational responses.
	w.status = status
}
func (w *guardWriter) Write(body []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return 0, http.ErrHandlerTimeout
	}
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(body)
}
func (w *guardWriter) stop() { w.mu.Lock(); defer w.mu.Unlock(); w.stopped = true; w.body.Reset() }
func (w *guardWriter) commit(target http.ResponseWriter) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, values := range w.header {
		target.Header()[key] = append([]string(nil), values...)
	}
	status := w.status
	if status == 0 {
		status = 200
	}
	target.WriteHeader(status)
	_, _ = target.Write(w.body.Bytes())
	w.stopped = true
}
