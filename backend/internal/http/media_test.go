package apihttp

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/auth"
	"github.com/yourname/vandrouka/backend/internal/media"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

const samplePhotoID = "00000000-0000-4000-8000-000000000001"

type mediaAuth struct{ fakeAuth }

func (a mediaAuth) Authenticate(ctx context.Context, token string) (auth.User, error) {
	if token == "other" {
		return auth.User{ID: 2}, nil
	}
	return a.fakeAuth.Authenticate(ctx, token)
}

type fakeMedia struct {
	body    []byte
	deleted bool
	err     error
	uploads int
}

func (s *fakeMedia) Upload(_ context.Context, owner, place int64, reader io.Reader, mime string) (visits.Photo, error) {
	s.uploads++
	if s.err != nil {
		return visits.Photo{}, s.err
	}
	if owner != 1 || place != 7 {
		return visits.Photo{}, media.ErrNotFound
	}
	cleaned, err := media.Sanitize(reader, mime)
	if err != nil {
		return visits.Photo{}, err
	}
	s.body = cleaned.Body
	size := int64(len(s.body))
	width, height := cleaned.Width, cleaned.Height
	now := time.Now().UTC()
	return visits.Photo{ID: samplePhotoID, UserID: owner, PlaceID: place, ObjectKey: "secret/storage/key", Status: visits.Ready, MIMEType: &cleaned.MIME, SizeBytes: &size, Width: &width, Height: &height, CreatedAt: now, ReadyAt: &now}, nil
}
func (s *fakeMedia) Read(_ context.Context, owner int64, id string) (visits.Photo, []byte, error) {
	if s.err != nil {
		return visits.Photo{}, nil, s.err
	}
	if owner != 1 || id != samplePhotoID || s.deleted {
		return visits.Photo{}, nil, media.ErrNotFound
	}
	mime := "image/png"
	return visits.Photo{MIMEType: &mime}, s.body, nil
}
func (s *fakeMedia) Delete(_ context.Context, owner int64, id string) error {
	if s.err != nil {
		return s.err
	}
	if owner != 1 || id != samplePhotoID {
		return media.ErrNotFound
	}
	s.deleted = true
	return nil
}
func mediaRouter(service MediaService) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestLogging, MediaResponses(time.Second))
	RegisterMedia(r, mediaAuth{}, service)
	return r
}
func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func uploadRequest(t *testing.T, router http.Handler, fields [][2]string, body []byte, mime, token string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	for _, field := range fields {
		if err := form.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="untrusted-private-name.png"`)
	header.Set("Content-Type", mime)
	part, err := form.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(body); err != nil {
		t.Fatal(err)
	}
	form.Close()
	req := httptest.NewRequest("POST", "/v1/visits/photo-upload", &b)
	req.Header.Set("Content-Type", form.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func privateRequest(router http.Handler, method, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/v1/visits/photos/"+samplePhotoID, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func TestMediaHTTP(t *testing.T) {
	service := &fakeMedia{}
	router := mediaRouter(service)
	if rec := uploadRequest(t, router, [][2]string{{"place_id", "7"}}, testPNG(t), "image/png", ""); rec.Code != 401 || service.uploads != 0 {
		t.Fatal("anonymous upload", rec.Code)
	}
	rec := uploadRequest(t, router, [][2]string{{"place_id", "7"}}, testPNG(t), "image/png", "valid")
	if rec.Code != 201 || strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "user_id") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing privacy/request headers")
	}
	for _, method := range []string{"GET", "DELETE"} {
		if rec := privateRequest(router, method, ""); rec.Code != 401 {
			t.Fatal("anonymous", method, rec.Code)
		}
		if rec := privateRequest(router, method, "other"); rec.Code != 404 {
			t.Fatal("foreign photo", method, rec.Code)
		}
	}
	rec = privateRequest(router, "GET", "valid")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(rec.Code, rec.Header())
	}
	if _, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if rec := privateRequest(router, "DELETE", "valid"); rec.Code != 204 {
			t.Fatal("idempotent delete", rec.Code)
		}
	}
	if rec := privateRequest(router, "GET", "valid"); rec.Code != 404 {
		t.Fatal("deleted photo", rec.Code)
	}
}
func TestMediaMalformedAndLimits(t *testing.T) {
	for _, fields := range [][][2]string{{}, {{"place_id", "0"}}, {{"place_id", "7"}, {"place_id", "8"}}, {{"place_id", "7"}, {"user_id", "1"}}} {
		rec := uploadRequest(t, mediaRouter(&fakeMedia{}), fields, testPNG(t), "image/png", "valid")
		if rec.Code != 400 {
			t.Fatal(fields, rec.Code)
		}
	}
	for _, entry := range []struct {
		body   []byte
		mime   string
		status int
	}{{testPNG(t), "image/jpeg", 415}, {[]byte("bad image"), "image/png", 400}, {testPNG(t), "text/html", 415}, {bytes.Repeat([]byte("x"), media.MaxFileBytes+1), "image/jpeg", 413}} {
		rec := uploadRequest(t, mediaRouter(&fakeMedia{}), [][2]string{{"place_id", "7"}}, entry.body, entry.mime, "valid")
		if rec.Code != entry.status {
			t.Fatal(entry.mime, rec.Code, rec.Body.String())
		}
	}
	router := mediaRouter(&fakeMedia{})
	for i := 0; i < 7; i++ {
		rec := uploadRequest(t, router, [][2]string{{"place_id", "7"}}, testPNG(t), "image/png", "valid")
		if i == 6 && (rec.Code != 429 || rec.Header().Get("Retry-After") == "") {
			t.Fatal("upload limit", rec.Code)
		}
	}
	for _, err := range []error{media.ErrUnavailable, errors.New("secret/storage/key")} {
		rec := privateRequest(mediaRouter(&fakeMedia{err: err}), "GET", "valid")
		want := 503
		if err != media.ErrUnavailable {
			want = 500
		}
		if rec.Code != want || strings.Contains(rec.Body.String(), "secret") {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
}

type hangingMedia struct{ fakeMedia }

func (s *hangingMedia) Read(ctx context.Context, _ int64, _ string) (visits.Photo, []byte, error) {
	<-ctx.Done()
	return visits.Photo{}, nil, ctx.Err()
}
func TestMediaTimeoutKeepsPrivateHeaders(t *testing.T) {
	r := chi.NewRouter()
	r.Use(MediaResponses(5 * time.Millisecond))
	RegisterMedia(r, mediaAuth{}, &hangingMedia{})
	rec := privateRequest(r, "GET", "valid")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "media_unavailable") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rec.Code, rec.Body.String(), rec.Header())
	}
}

type blockingReadMedia struct {
	fakeMedia
	started chan struct{}
	release chan struct{}
}

func (s *blockingReadMedia) Read(_ context.Context, _ int64, _ string) (visits.Photo, []byte, error) {
	s.started <- struct{}{}
	<-s.release
	mime := "image/png"
	return visits.Photo{MIMEType: &mime}, s.body, nil
}
func TestMediaConcurrentMemoryBound(t *testing.T) {
	service := &blockingReadMedia{fakeMedia: fakeMedia{body: testPNG(t)}, started: make(chan struct{}, 2), release: make(chan struct{})}
	r := chi.NewRouter()
	r.Use(MediaResponses(5 * time.Second))
	RegisterMedia(r, mediaAuth{}, service)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- privateRequest(r, "GET", "valid").Code }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-service.started:
		case <-time.After(3 * time.Second):
			close(service.release)
			wg.Wait()
			t.Fatal("read did not start")
		}
	}
	rec := privateRequest(r, "GET", "valid")
	close(service.release)
	wg.Wait()
	close(results)
	if rec.Code != 429 {
		t.Fatal("concurrent media operations unbounded", rec.Code)
	}
	for status := range results {
		if status != 200 {
			t.Fatal(status)
		}
	}
}
