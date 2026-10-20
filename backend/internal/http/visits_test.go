package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

type fakeVisits struct {
	result   visits.Result
	err      error
	calls    int
	received time.Time
}

func (f *fakeVisits) Confirm(_ context.Context, owner, place int64, key string, in visits.Input, received time.Time) (visits.Result, error) {
	f.calls++
	f.received = received
	if owner != 1 || place != 7 || key != samplePhotoID || in.PhotoID != samplePhotoID {
		return visits.Result{}, visits.ErrInvalid
	}
	return f.result, f.err
}
func TestVisitHTTP(t *testing.T) {
	now := time.Now().UTC()
	code := visits.TooFar
	base := visits.Visit{ID: 1, PlaceID: 7, PhotoID: samplePhotoID, Status: visits.Confirmed, CreatedAt: now, ConfirmedAt: &now, UserID: 1, IdempotencyKey: "private", RequestHash: []byte("private"), DistanceM: new(float64)}
	valid := `{"photo_id":"` + samplePhotoID + `","location":{"lat":0,"lon":0,"accuracy_m":0,"observed_at":"2026-10-25T12:00:00Z"}}`
	cases := []struct {
		name, body, token, key, mime, path string
		result                             visits.Result
		err                                error
		want                               int
		calls                              int
	}{
		{name: "new", body: valid, result: visits.Result{Visit: base, XPAwarded: 100}, want: 201, calls: 1},
		{name: "replay", body: valid, result: visits.Result{Visit: base, XPAwarded: 100, Replay: true}, want: 200, calls: 1},
		{name: "no session", body: valid, token: "bad", want: 401},
		{name: "absent accuracy", body: strings.Replace(valid, `"accuracy_m":0,`, "", 1), want: 400},
		{name: "null latitude", body: strings.Replace(valid, `"lat":0`, `"lat":null`, 1), want: 400},
		{name: "missing location", body: `{"photo_id":"` + samplePhotoID + `"}`, want: 400},
		{name: "missing key", body: valid, key: "absent", want: 400},
		{name: "wrong path", body: valid, path: "bad", want: 400},
		{name: "wrong content", body: valid, mime: "text/plain", want: 415},
		{name: "extra field", body: strings.Replace(valid, `"lat":0`, `"user_id":1,"lat":0`, 1), want: 400},
		{name: "client XP", body: strings.Replace(valid, `"photo_id"`, `"xp_awarded":100,"photo_id"`, 1), want: 400},
		{name: "extra document", body: valid + `{}`, want: 400},
		{name: "large", body: `{"photo_id":"` + strings.Repeat("a", 5000) + `"}`, want: 413},
		{name: "foreign", body: valid, err: visits.ErrNotFound, want: 404, calls: 1},
		{name: "conflict", body: valid, err: visits.ErrConflict, want: 409, calls: 1},
		{name: "consumed", body: valid, err: visits.ErrPhotoUsed, want: 409, calls: 1},
		{name: "invalid", body: valid, err: visits.ErrInvalid, want: 400, calls: 1},
		{name: "database error", body: valid, err: errors.New("private sql credentials"), want: 500, calls: 1},
	}
	rejected := base
	rejected.Status = visits.Rejected
	rejected.ConfirmedAt = nil
	rejected.RejectionCode = &code
	cases = append(cases, struct {
		name, body, token, key, mime, path string
		result                             visits.Result
		err                                error
		want                               int
		calls                              int
	}{name: "rejected replay", body: valid, result: visits.Result{Visit: rejected, Replay: true}, want: 422, calls: 1})
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeVisits{result: tt.result, err: tt.err}
			r := chi.NewRouter()
			r.Use(RequestLogging, AuthResponses(time.Second))
			RegisterVisits(r, fakeAuth{}, f)
			path := tt.path
			if path == "" {
				path = "7"
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/places/"+path+"/visits", strings.NewReader(tt.body))
			token := tt.token
			if token == "" {
				token = "valid"
			}
			req.Header.Set("Authorization", "Bearer "+token)
			if tt.key != "absent" {
				req.Header.Set("Idempotency-Key", samplePhotoID)
			}
			mime := tt.mime
			if mime == "" {
				mime = "application/json"
			}
			req.Header.Set("Content-Type", mime)
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			if recorder.Code != tt.want || f.calls != tt.calls {
				t.Fatalf("status %d calls %d body %s", recorder.Code, f.calls, recorder.Body)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing private headers")
			}
			for _, s := range []string{"private", "user_id", "distance_m", "request_hash", "idempotency_key"} {
				if strings.Contains(recorder.Body.String(), s) {
					t.Fatal("private value", s)
				}
			}
			if tt.want == 201 || tt.want == 200 {
				var body visits.Result
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.XPAwarded != 100 {
					t.Fatal(err, body)
				}
			}
			if f.calls > 0 && f.received.IsZero() {
				t.Fatal("receive time not captured")
			}
		})
	}
	if endpointTemplate("/v1/places/secret/visits") != "/v1/places/{id}/visits" {
		t.Fatal("unsafe log template")
	}
}
