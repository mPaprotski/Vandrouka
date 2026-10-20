package apihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/store"
)

func TestSharedErrorMapping(t *testing.T) {
	for _, scope := range []errorScope{catalogErrors, generateErrors, recalculateErrors} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
			code   string
		}{
			{"invalid", fmt.Errorf("%w: sensitive detail", ErrInvalidRouteRequest), 400, "invalid_request"},
			{"repository missing", fmt.Errorf("load: %w", store.ErrNotFound), 404, "not_found"},
			{"city missing", routes.ErrCityNotFound, 404, "not_found"},
			{"place missing", routes.ErrPlaceNotFound, 404, "not_found"},
			{"unexpected", errors.New("postgres SQL private-token"), 500, "internal"},
		} {
			t.Run(string(scope)+"/"+tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				writeFailure(w, tc.err, scope)
				var body APIError
				if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.Code != tc.code || body.Error.Message == "" || body.Error.Details != nil {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
					t.Fatal("content type")
				}
				if strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "sensitive detail") {
					t.Fatal("leaked cause")
				}
			})
		}
		for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("%w: secret URL", routes.ErrRoutingUnavailable)} {
			w := httptest.NewRecorder()
			writeFailure(w, err, scope)
			var body APIError
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			expectedStatus, expectedCode := 503, "routing_unavailable"
			if scope == catalogErrors {
				expectedStatus, expectedCode = 500, "internal"
			}
			if w.Code != expectedStatus || body.Error.Code != expectedCode {
				t.Fatalf("scope %s: %d %s", scope, w.Code, w.Body.String())
			}
		}
		w := httptest.NewRecorder()
		writeFailure(w, routes.ErrConstraintsViolation, scope)
		expected := 500
		if scope == recalculateErrors {
			expected = 422
		}
		if w.Code != expected {
			t.Fatal("constraint scope mismatch")
		}
	}
}

func TestErrorLogsDoNotExposeCauses(t *testing.T) {
	var log bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	defer slog.SetDefault(original)
	err := fmt.Errorf("%w: https://provider/path?access_token=private-token SELECT password", routes.ErrRoutingUnavailable)
	writeFailure(httptest.NewRecorder(), err, generateErrors)
	writeFailure(httptest.NewRecorder(), errors.New("private-token SELECT password"), catalogErrors)
	if strings.Contains(log.String(), "private-token") || strings.Contains(log.String(), "SELECT") || strings.Contains(log.String(), "access_token") {
		t.Fatal("error cause leaked in logs")
	}
	if !strings.Contains(log.String(), `"scope":"generate"`) || !strings.Contains(log.String(), `"code":"internal"`) {
		t.Fatal("missing safe diagnostic metadata")
	}
}
