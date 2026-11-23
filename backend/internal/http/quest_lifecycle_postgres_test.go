package apihttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/auth"
	"github.com/yourname/vandrouka/backend/internal/history"
	"github.com/yourname/vandrouka/backend/internal/media"
	"github.com/yourname/vandrouka/backend/internal/quests"
	"github.com/yourname/vandrouka/backend/internal/store"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

func TestQuestLifecyclePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	mustQuest(t, err)
	defer pool.Close()
	accounts := auth.Postgres{Pool: pool}
	authn, err := auth.New(accounts)
	mustQuest(t, err)
	const password = "a long test password"
	tag := uuid.NewString()
	var owners []int64
	var city int64
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM quests WHERE city_id=$1", city)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=ANY($1)", owners)
		_, _ = pool.Exec(context.Background(), "DELETE FROM cities WHERE id=$1", city)
	}()
	createAccount := func(label string) auth.LoginResult {
		user, err := authn.Register(ctx, label+tag+"@example.com", password)
		mustQuest(t, err)
		owners = append(owners, user.User.ID)
		return user
	}
	teacher := createAccount("teacher")
	otherTeacher := createAccount("other")
	student := createAccount("student")
	friend := createAccount("friend")
	mustQuest(t, pool.QueryRow(ctx, `INSERT INTO cities(slug,name,center_lat,center_lon)
		VALUES($1,'Quest test',53.9,27.55) RETURNING id`, tag).Scan(&city))
	steps := make([]quests.Step, 3)
	for i := range steps {
		mustQuest(t, pool.QueryRow(ctx, `INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at)
			VALUES($1,$2,$3,53.9,27.55,1,10,CURRENT_DATE) RETURNING id`, city,
			fmt.Sprintf("place-%d", i), fmt.Sprintf("Secret place %d", i)).Scan(&steps[i].PlaceID))
		steps[i].Clue = fmt.Sprintf("Find the facade %d", i)
		steps[i].PhotoTask = "Photograph the entrance"
		steps[i].RevealText = fmt.Sprintf("Reveal after approval %d", i)
	}

	db := quests.Postgres{Pool: pool, Catalog: func(tx pgx.Tx) quests.PlaceReader { return store.NewCatalog(tx) }}
	objectStore := &questObjects{data: make(map[string][]byte)}
	photos := media.New(media.Postgres{Pool: pool}, objectStore)
	filter := &questAnonymizer{replacement: questPNG(t, color.Black)}
	photos.Anonymizer = filter
	r := chi.NewRouter()
	RegisterQuests(r, authn, db)
	mediaHTTP := RegisterMedia(r, authn, photos)
	// The flow intentionally simulates multiple uploads by the same participant
	// in one second. Advance the request window without altering DB deadlines.
	window := time.Now()
	mediaHTTP.now = func() time.Time {
		window = window.Add(time.Minute)
		return window
	}
	mediaHTTP.RegisterQuestPhotos(r, db, photos)
	normalVisits, err := visits.New(visits.Postgres{Pool: pool}, visits.DefaultPolicy())
	mustQuest(t, err)
	RegisterVisits(r, authn, normalVisits)
	RegisterDeletions(r, authn, accounts, history.Postgres{Pool: pool})
	do := func(actor auth.LoginResult, method, path string, body any, key string, want int) []byte {
		t.Helper()
		response := questRequest(t, ctx, r, actor, method, path, body, key)
		if response.Code != want {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.Code, want, response.Body)
		}
		return response.Body.Bytes()
	}
	input := questPNG(t, color.White)
	upload := func(actor auth.LoginResult, path string, want int, place int64) []byte {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		if place > 0 {
			mustQuest(t, form.WriteField("place_id", fmt.Sprint(place)))
		}
		part, err := form.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="file"; filename="test.png"`},
			"Content-Type":        {"image/png"},
		})
		mustQuest(t, err)
		_, err = part.Write(input)
		mustQuest(t, err)
		mustQuest(t, form.Close())
		method := "PUT"
		if place > 0 {
			method = "POST"
		}
		req := httptest.NewRequestWithContext(ctx, method, path, &body)
		req.Header.Set("Authorization", "Bearer "+actor.AccessToken)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("upload returned %d, want %d: %s", w.Code, want, w.Body)
		}
		return w.Body.Bytes()
	}

	base := "/v1/quests"
	draft := questDecode[quests.Editor](t, do(teacher, "POST", base, map[string]any{"city_id": city}, "", 201))
	q := base + "/" + draft.ID
	do(otherTeacher, "GET", q+"/editor", nil, "", 404)
	draft = questDecode[quests.Editor](t, do(teacher, "PUT", q, quests.SaveInput{
		ExpectedRevision: draft.Revision, Title: "Test quest", Steps: &steps,
	}, "", 200))
	pub := questDecode[quests.Quest](t, do(teacher, "POST", q+"/publish", map[string]any{"expected_revision": draft.Revision}, "", 200))
	createRun := func(title string) quests.Invite {
		return questDecode[quests.Invite](t, do(teacher, "POST", q+"/runs",
			map[string]any{"expected_revision": pub.Revision, "title": title}, "", 201))
	}
	invite := createRun("Class trip")
	run := "/v1/quest-runs/" + invite.ID
	join := func(actor auth.LoginResult, code string) quests.Progress {
		return questDecode[quests.Progress](t, do(actor, "POST", "/v1/quest-participations/join",
			map[string]string{"code": code, "display_name": "Participant"}, "", 200))
	}
	joined := join(student, invite.JoinCode)
	groupmate := join(friend, invite.JoinCode)
	if joined.CurrentTask != nil || joined.Status != "waiting" || join(student, invite.JoinCode).ID != joined.ID {
		t.Fatalf("unexpected waiting or replay result: %+v", joined)
	}
	do(teacher, "POST", run+"/start", nil, "", 200)
	readProgress := func(actor auth.LoginResult, id string) quests.Progress {
		return questDecode[quests.Progress](t, do(actor, "GET", "/v1/quest-participations/"+id, nil, "", 200))
	}
	started := readProgress(student, joined.ID)
	if started.CurrentTask == nil || started.CurrentTask.Position != 1 || started.StartedAt == nil {
		t.Fatalf("first task missing: %+v", started)
	}
	studentPath := "/v1/quest-participations/" + joined.ID
	do(friend, "GET", studentPath, nil, "", 404)
	projected := string(do(student, "GET", studentPath, nil, "", 200))
	for _, forbidden := range []string{"place_id", "Secret place", "Reveal after", steps[1].Clue, steps[2].Clue} {
		if strings.Contains(projected, forbidden) {
			t.Fatalf("locked quest data leaked: %s", forbidden)
		}
	}
	var nextTask string
	mustQuest(t, pool.QueryRow(ctx, "SELECT id::text FROM quest_run_steps WHERE run_id=$1 AND position=2", invite.ID).Scan(&nextTask))
	do(student, "POST", studentPath+"/attempts", map[string]string{"task_id": nextTask}, "", 404)

	attemptPath := func(a quests.Attempt) string { return "/v1/quest-attempts/" + a.ID }
	createAttempt := func(actor auth.LoginResult, p quests.Progress) quests.Attempt {
		return questDecode[quests.Attempt](t, do(actor, "POST", "/v1/quest-participations/"+p.ID+"/attempts",
			map[string]string{"task_id": p.CurrentTask.ID}, "", 200))
	}
	location := func() visits.Location {
		return visits.Location{Lat: 53.9, Lon: 27.55, AccuracyM: 10, ObservedAt: time.Now().UTC()}
	}
	send := func(actor auth.LoginResult, a quests.Attempt, key string, fix visits.Location, status int) []byte {
		return do(actor, "POST", attemptPath(a)+"/submit", map[string]any{"location": fix}, key, status)
	}
	submit := func(actor auth.LoginResult, a quests.Attempt) quests.Attempt {
		return questDecode[quests.Attempt](t, send(actor, a, uuid.NewString(), location(), 200))
	}
	review := func(a quests.Attempt, decision, reason string) quests.Attempt {
		return questDecode[quests.Attempt](t, do(teacher, "POST", attemptPath(a)+"/review",
			quests.ReviewInput{ExpectedRevision: a.Revision, Decision: decision, Reason: reason}, "", 200))
	}
	a := createAttempt(student, started)
	photoPath := attemptPath(a) + "/photo"
	filter.fail = media.ErrAnonymizerBusy
	upload(student, photoPath, 429, 0)
	filter.fail = errors.New("processing failure")
	upload(student, photoPath, 503, 0)
	filter.fail = nil
	if objectStore.puts != 0 {
		t.Fatal("a failed transform wrote an object")
	}
	filter.entered, filter.release = make(chan struct{}), make(chan struct{})
	doneUpload := make(chan []byte, 1)
	go func() { doneUpload <- upload(student, photoPath, 200, 0) }()
	select {
	case <-filter.entered:
	case <-ctx.Done():
		t.Fatal("upload did not reach the processor")
	}
	duplicate := upload(student, photoPath, 409, 0)
	close(filter.release)
	if !bytes.Contains(duplicate, []byte("photo_upload_in_progress")) {
		t.Fatalf("overlapping upload was not refused: %s", duplicate)
	}
	select {
	case encoded := <-doneUpload:
		a = questDecode[quests.Attempt](t, encoded)
	case <-ctx.Done():
		t.Fatal("initial upload stalled")
	}
	filter.entered, filter.release = nil, nil
	upload(student, photoPath, 200, 0)
	if objectStore.puts != 1 || !a.PhotoReady {
		t.Fatal("upload replay didn't reuse the processed file")
	}
	do(teacher, "GET", photoPath, nil, "", 404)
	do(friend, "GET", photoPath, nil, "", 404)
	do(auth.LoginResult{}, "GET", photoPath, nil, "", 401)
	content := do(student, "GET", photoPath, nil, "", 200)
	if !bytes.Equal(content, filter.replacement) || bytes.Equal(content, input) {
		t.Fatal("returned photo is not the processed variant")
	}
	var photoID, objectKey string
	mustQuest(t, pool.QueryRow(ctx, `SELECT f.id::text,f.object_key FROM visit_photos f JOIN quest_attempts a ON a.photo_id=f.id
		WHERE a.id=$1`, a.ID).Scan(&photoID, &objectKey))
	do(student, "GET", "/v1/visits/photos/"+photoID, nil, "", 404)
	do(student, "POST", fmt.Sprintf("/v1/places/%d/visits", steps[0].PlaceID),
		visits.Input{PhotoID: photoID, Location: location()}, uuid.NewString(), 404)

	far, inaccurate, stale, future := location(), location(), location(), location()
	far.Lat = 54
	inaccurate.AccuracyM = 45
	stale.ObservedAt = stale.ObservedAt.Add(-5 * time.Minute)
	future.ObservedAt = future.ObservedAt.Add(time.Minute)
	for code, bad := range map[string]visits.Location{"too_far": far, "poor_accuracy": inaccurate, "stale_location": stale, "future_location": future} {
		if body := send(student, a, uuid.NewString(), bad, 422); !bytes.Contains(body, []byte(code)) {
			t.Fatalf("missing GPS error %s: %s", code, body)
		}
	}
	key, fix := uuid.NewString(), location()
	a = questDecode[quests.Attempt](t, send(student, a, key, fix, 200))
	send(student, a, key, fix, 200)
	fix.AccuracyM++
	send(student, a, key, fix, 409)
	do(teacher, "GET", photoPath, nil, "", 200)
	do(otherTeacher, "GET", photoPath, nil, "", 404)
	do(otherTeacher, "POST", attemptPath(a)+"/review",
		quests.ReviewInput{ExpectedRevision: a.Revision, Decision: "accept"}, "", 404)
	// Retention must be based on submission, even for an old uploaded draft.
	mustQuest(t, execQuest(ctx, pool, "UPDATE visit_photos SET created_at=NOW()-INTERVAL '2 days' WHERE id=$1", photoID))
	mustQuest(t, photos.Cleanup(ctx))
	do(teacher, "GET", photoPath, nil, "", 200)

	declined := review(a, "reject", "Show the whole entrance")
	if declined.RejectionReason == nil || *declined.RejectionReason != "Show the whole entrance" {
		t.Fatal("rejection comment not returned")
	}
	mustQuest(t, execQuest(ctx, pool, "UPDATE quest_attempts SET photo_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1", a.ID))
	do(student, "GET", photoPath, nil, "", 404)
	mustQuest(t, db.ExpirePhotos(ctx))
	mustQuest(t, photos.Cleanup(ctx))
	if _, present := objectStore.data[objectKey]; present {
		t.Fatal("expired rejected object was not deleted")
	}

	a = createAttempt(student, readProgress(student, joined.ID))
	upload(student, attemptPath(a)+"/photo", 200, 0)
	a = submit(student, a)
	mustQuest(t, execQuest(ctx, pool, "UPDATE quest_attempts SET location_observed_at=NOW()-INTERVAL '5 minutes' WHERE id=$1", a.ID))
	body := quests.ReviewInput{ExpectedRevision: a.Revision, Decision: "accept"}
	var approvals [2]*httptest.ResponseRecorder
	var wg sync.WaitGroup
	for i := range approvals {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			approvals[i] = questRequest(t, ctx, r, teacher, "POST", attemptPath(a)+"/review", body, "")
		}(i)
	}
	wg.Wait()
	for _, approval := range approvals {
		if approval.Code != 200 || questDecode[quests.Attempt](t, approval.Body.Bytes()).XPAwarded != 100 {
			t.Fatalf("approval or replay failed: %d %s", approval.Code, approval.Body)
		}
	}
	first := readProgress(student, joined.ID)
	if first.CompletedSteps != 1 || first.CurrentTask == nil || first.CurrentTask.Position != 2 || len(first.Stories) != 1 ||
		first.Stories[0].Text != steps[0].RevealText || first.XPAwarded != 100 {
		t.Fatalf("wrong step after approval: %+v", first)
	}
	if result := readProgress(friend, groupmate.ID); result.CompletedSteps != 0 || result.CurrentTask.Position != 1 {
		t.Fatalf("another participant's progress changed: %+v", result)
	}

	normal := questDecode[visits.Photo](t, upload(student, "/v1/visits/photo-upload", 201, steps[1].PlaceID))
	do(student, "POST", fmt.Sprintf("/v1/places/%d/visits", steps[1].PlaceID),
		visits.Input{PhotoID: normal.ID, Location: location()}, uuid.NewString(), 201)
	b := createAttempt(student, first)
	upload(student, attemptPath(b)+"/photo", 200, 0)
	if result := review(submit(student, b), "accept", ""); result.XPAwarded != 0 {
		t.Fatalf("duplicate first-visit reward: %+v", result)
	}
	c := createAttempt(student, readProgress(student, joined.ID))
	upload(student, attemptPath(c)+"/photo", 200, 0)
	c = submit(student, c)

	due := createAttempt(friend, readProgress(friend, groupmate.ID))
	upload(friend, attemptPath(due)+"/photo", 200, 0)
	due = submit(friend, due)
	mustQuest(t, execQuest(ctx, pool, "UPDATE quest_attempts SET photo_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1", due.ID))
	do(teacher, "GET", attemptPath(due)+"/photo", nil, "", 404)
	do(teacher, "POST", attemptPath(due)+"/review", quests.ReviewInput{ExpectedRevision: due.Revision, Decision: "accept"}, "", 409)
	if result := readProgress(friend, groupmate.ID); result.ReviewPending || result.CurrentAttempt == nil || result.CurrentAttempt.Status != "expired" {
		t.Fatalf("expired work still in waiting state: %+v", result)
	}
	retry := createAttempt(friend, readProgress(friend, groupmate.ID))
	if retry.ID == due.ID {
		t.Fatal("expired submission blocked a new attempt")
	}

	do(teacher, "POST", run+"/finish", nil, "", 200)
	do(friend, "POST", "/v1/quest-participations/join",
		map[string]string{"code": invite.JoinCode, "display_name": "Participant"}, "", 404)
	if result := readProgress(student, joined.ID); !result.ReviewPending || result.CurrentTask != nil {
		t.Fatalf("closed run exposes a new clue or hides existing review: %+v", result)
	}
	review(c, "accept", "")
	done := readProgress(student, joined.ID)
	if done.Status != "completed" || done.CompletedSteps != 3 || done.XPAwarded != 200 || len(done.Stories) != 3 || done.CompletedAt == nil {
		t.Fatalf("wrong completed quest: %+v", done)
	}

	waitingRun := createRun("Waiting group")
	waitingStudent := join(student, waitingRun.JoinCode)
	nextRun := createRun("Organizer deletion")
	nextStudent := join(friend, nextRun.JoinCode)
	do(teacher, "POST", "/v1/quest-runs/"+nextRun.ID+"/start", nil, "", 200)
	pending := createAttempt(friend, readProgress(friend, nextStudent.ID))
	upload(friend, attemptPath(pending)+"/photo", 200, 0)
	pending = submit(friend, pending)
	do(teacher, "DELETE", "/v1/me", map[string]string{"password": password}, "", 204)
	if result := readProgress(friend, nextStudent.ID); result.Status != "closed" || result.ReviewPending {
		t.Fatalf("deleting organizer left a participant unable to move on: %+v", result)
	}
	do(friend, "GET", attemptPath(pending)+"/photo", nil, "", 404)
	if result := readProgress(student, waitingStudent.ID); result.Status != "closed" || result.StartedAt != nil {
		t.Fatalf("orphaned waiting run has wrong state: %+v", result)
	}
	if result := readProgress(student, joined.ID); result.CompletedSteps != 3 || result.XPAwarded != 200 {
		t.Fatalf("deleting organizer removed learner progress: %+v", result)
	}
	mustQuest(t, pool.QueryRow(ctx, "SELECT photo_id::text FROM quest_attempts WHERE id=$1", a.ID).Scan(&photoID))
	do(student, "DELETE", "/v1/visits/photos/"+photoID, nil, "", 204)
	do(student, "GET", attemptPath(a)+"/photo", nil, "", 404)
	if result := readProgress(student, joined.ID); len(result.Stories) != 3 || result.XPAwarded != 200 {
		t.Fatalf("deleting the photo lost progress: %+v", result)
	}
	var xp int
	mustQuest(t, pool.QueryRow(ctx, "SELECT sum(amount) FROM xp_transactions WHERE user_id=$1", student.User.ID).Scan(&xp))
	if xp != 300 {
		t.Fatalf("total XP %d, want 300", xp)
	}
	do(student, "DELETE", "/v1/me", map[string]string{"password": password}, "", 204)
	do(student, "GET", studentPath, nil, "", 401)
	var rows int
	mustQuest(t, pool.QueryRow(ctx, "SELECT count(*) FROM quest_participations WHERE user_id=$1", student.User.ID).Scan(&rows))
	if rows != 0 {
		t.Fatalf("deleted learner still has %d participations", rows)
	}
}

func mustQuest(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func execQuest(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) error {
	_, err := pool.Exec(ctx, sql, args...)
	return err
}

func questRequest(t *testing.T, ctx context.Context, r http.Handler, actor auth.LoginResult, method, path string, body any, key string) *httptest.ResponseRecorder {
	t.Helper()
	var content []byte
	if body != nil {
		var err error
		content, err = json.Marshal(body)
		mustQuest(t, err)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(content))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if actor.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+actor.AccessToken)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func questDecode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var result T
	mustQuest(t, json.Unmarshal(body, &result))
	return result
}

func questPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	var out bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, c)
	mustQuest(t, png.Encode(&out, img))
	return out.Bytes()
}

type questAnonymizer struct {
	replacement []byte
	fail        error
	entered     chan struct{}
	release     chan struct{}
}

func (a *questAnonymizer) Anonymize(ctx context.Context, input media.Image) (media.Image, error) {
	if a.fail != nil {
		return media.Image{}, a.fail
	}
	if a.entered != nil {
		close(a.entered)
		select {
		case <-a.release:
		case <-ctx.Done():
			return media.Image{}, ctx.Err()
		}
	}
	input.Body = a.replacement
	hash := sha256.Sum256(input.Body)
	input.SHA256 = hash[:]
	return input, nil
}

type questObjects struct {
	mu   sync.Mutex
	puts int
	data map[string][]byte
}

func (s *questObjects) Init(context.Context) error { return nil }
func (s *questObjects) Put(_ context.Context, key string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	s.data[key] = bytes.Clone(data)
	return nil
}
func (s *questObjects) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return bytes.Clone(data), nil
}
func (s *questObjects) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}
