package visits

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfirmationPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var owner, other, city, place int64
	tag := uuid.NewString()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id", tag+"@example.com").Scan(&owner))
	must(pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id", tag+"-other@example.com").Scan(&other))
	must(pool.QueryRow(ctx, "INSERT INTO cities(slug,name,center_lat,center_lon,timezone) VALUES($1,'Visit test',0,0,'Europe/Minsk') RETURNING id", tag).Scan(&city))
	defer func() {
		pool.Exec(context.Background(), "DELETE FROM users WHERE id=ANY($1)", []int64{owner, other})
		pool.Exec(context.Background(), "DELETE FROM cities WHERE id=$1", city)
		pool.Exec(context.Background(), "DELETE FROM media_deletions WHERE object_key LIKE $1", tag+"/%")
	}()
	must(pool.QueryRow(ctx, `INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at) VALUES($1,'visit','Visit',0,0,1,10,CURRENT_DATE) RETURNING id`, city).Scan(&place))
	photo := func(user int64, ready bool) string {
		t.Helper()
		id := uuid.NewString()
		status := "uploading"
		var at *time.Time
		now := time.Now()
		if ready {
			status = "ready"
			at = &now
		}
		_, err := pool.Exec(ctx, `INSERT INTO visit_photos(id,user_id,place_id,object_key,status,mime_type,size_bytes,width,height,sha256,upload_expires_at,ready_at) VALUES($1,$2,$3,$4,$5,'image/png',10,1,1,$6,NOW()+INTERVAL '15 minutes',$7)`, id, user, place, tag+"/"+id, status, make([]byte, 32), at)
		must(err)
		return id
	}
	s, _ := New(Postgres{pool}, DefaultPolicy())
	now := time.Now().UTC()
	input := func(id string) Input { return Input{id, Location{0, 0, 5, now}} }
	confirm := func(key string, in Input) (Result, error) { return s.Confirm(ctx, owner, place, key, in, now) }
	count := func(table string) int {
		t.Helper()
		var n int
		must(pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE user_id=$1", owner).Scan(&n))
		return n
	}
	firstPhoto := photo(owner, true)
	key := uuid.NewString()
	in := input(firstPhoto)
	first, err := confirm(key, in)
	must(err)
	if first.XPAwarded != 100 || first.Replay || first.Visit.Status != Confirmed {
		t.Fatal(first)
	}
	replay, err := s.Confirm(ctx, owner, place, key, in, now.Add(time.Hour))
	must(err)
	if !replay.Replay || replay.Visit.ID != first.Visit.ID || replay.XPAwarded != 100 {
		t.Fatal(replay)
	}
	changed := in
	changed.Location.AccuracyM++
	if _, err = confirm(key, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = confirm(uuid.NewString(), in); !errors.Is(err, ErrPhotoUsed) {
		t.Fatal(err)
	}
	second, err := confirm(uuid.NewString(), input(photo(owner, true)))
	must(err)
	if second.XPAwarded != 0 {
		t.Fatal(second)
	}
	// Rejected fix does not consume the photo; replay stays rejected even after time changes.
	bad := input(photo(owner, true))
	bad.Location.Lat = 1
	badKey := uuid.NewString()
	rejected, err := confirm(badKey, bad)
	must(err)
	if rejected.Visit.Status != Rejected || *rejected.Visit.RejectionCode != TooFar || rejected.XPAwarded != 0 {
		t.Fatal(rejected)
	}
	replay, err = confirm(badKey, bad)
	must(err)
	if !replay.Replay || replay.Visit.ID != rejected.Visit.ID {
		t.Fatal(replay)
	}
	bad.Location.Lat = 0
	_, err = confirm(uuid.NewString(), bad)
	must(err)
	// Missing / foreign / deleted photo reveal no metadata and add no rows.
	before := count("visits")
	for _, id := range []string{uuid.NewString(), photo(other, true)} {
		if _, err = confirm(uuid.NewString(), input(id)); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
	deleted := photo(owner, true)
	_, err = pool.Exec(ctx, "UPDATE visit_photos SET deleted_at=NOW() WHERE id=$1", deleted)
	must(err)
	if _, err = confirm(uuid.NewString(), input(deleted)); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if count("visits") != before {
		t.Fatal("bad resources wrote visits")
	}
	unready := input(photo(owner, false))
	r, err := confirm(uuid.NewString(), unready)
	must(err)
	if *r.Visit.RejectionCode != PhotoNotReady {
		t.Fatal(r)
	}
	// Business failures are persisted, with stable precedence and no XP.
	for _, code := range []RejectionCode{PoorAccuracy, StaleLocation, FutureLocation, InactivePlace} {
		v := input(photo(owner, true))
		switch code {
		case PoorAccuracy:
			v.Location.AccuracyM = 30.000001
		case StaleLocation:
			v.Location.ObservedAt = now.Add(-121 * time.Second)
		case FutureLocation:
			v.Location.ObservedAt = now.Add(31 * time.Second)
		case InactivePlace:
			_, err = pool.Exec(ctx, "UPDATE places SET is_active=false WHERE id=$1", place)
			must(err)
		}
		r, err = confirm(uuid.NewString(), v)
		must(err)
		if r.Visit.Status != Rejected || *r.Visit.RejectionCode != code || r.XPAwarded != 0 {
			t.Fatal(code, r)
		}
	}
	_, err = pool.Exec(ctx, "UPDATE places SET is_active=true WHERE id=$1", place)
	must(err)
	// Concurrent retry: exactly one new outcome, all others replay it.
	parallel := input(photo(owner, true))
	parallelKey := uuid.NewString()
	var wg sync.WaitGroup
	results := make(chan Result, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); r, e := confirm(parallelKey, parallel); results <- r; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		must(e)
	}
	newCount := 0
	var id int64
	for r := range results {
		if !r.Replay {
			newCount++
		}
		if id != 0 && id != r.Visit.ID {
			t.Fatal("duplicate outcomes")
		}
		id = r.Visit.ID
	}
	if newCount != 1 {
		t.Fatal(newCount)
	}
	// Two different bodies sharing one key: one outcome, one conflict.
	shared := uuid.NewString()
	a, b := input(photo(owner, true)), input(photo(owner, true))
	errs = make(chan error, 2)
	for _, v := range []Input{a, b} {
		wg.Add(1)
		go func(v Input) { defer wg.Done(); _, e := confirm(shared, v); errs <- e }(v)
	}
	wg.Wait()
	close(errs)
	conflicts := 0
	for e := range errs {
		if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			must(e)
		}
	}
	if conflicts != 1 {
		t.Fatal("conflicting race", conflicts)
	}

	// Different keys cannot consume one photo twice under concurrency.
	samePhoto := input(photo(owner, true))
	errs = make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := confirm(uuid.NewString(), samePhoto); errs <- e }()
	}
	wg.Wait()
	close(errs)
	usedCount := 0
	for e := range errs {
		if errors.Is(e, ErrPhotoUsed) {
			usedCount++
		} else {
			must(e)
		}
	}
	if usedCount != 1 {
		t.Fatal("photo consumption race", usedCount)
	}
	if count("xp_transactions") != 1 {
		t.Fatal("duplicate XP")
	}
	// A database failure after visit insert must roll the entire operation back.
	// Unique temporary trigger name, affecting only our test owner; no shared-data mutation.
	name := "fail_xp_" + fmt.Sprint(owner)
	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.user_id=%d THEN RAISE EXCEPTION 'test failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE INSERT ON xp_transactions FOR EACH ROW EXECUTE FUNCTION %s()`, name, owner, name, name))
	must(err)
	defer pool.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON xp_transactions; DROP FUNCTION IF EXISTS %s()", name, name))
	before = count("visits")
	_, err = confirm(uuid.NewString(), input(photo(owner, true)))
	if err == nil {
		t.Fatal("expected XP failure")
	}
	if count("visits") != before {
		t.Fatal("visit survived rollback")
	}
	_, err = pool.Exec(ctx, fmt.Sprintf("DROP TRIGGER %s ON xp_transactions; DROP FUNCTION %s()", name, name))
	must(err)
	// Hiding history and removing its photo must preserve first-award protection.
	_, err = pool.Exec(ctx, "UPDATE visits SET deleted_at=NOW() WHERE id=$1", first.Visit.ID)
	must(err)
	_, err = pool.Exec(ctx, "UPDATE visit_photos SET deleted_at=NOW() WHERE id=$1", firstPhoto)
	must(err)
	r, err = confirm(uuid.NewString(), input(photo(owner, true)))
	must(err)
	if r.XPAwarded != 0 {
		t.Fatal("reward after deletion")
	}
	// A second owner competes on a genuinely first place: different keys/photos yield only one award.
	p1, p2 := photo(other, true), photo(other, true)
	awards := make(chan int, 2)
	errs = make(chan error, 2)
	for _, pid := range []string{p1, p2} {
		wg.Add(1)
		go func(pid string) {
			defer wg.Done()
			r, e := s.Confirm(ctx, other, place, uuid.NewString(), input(pid), now)
			awards <- r.XPAwarded
			errs <- e
		}(pid)
	}
	wg.Wait()
	close(awards)
	close(errs)
	total := 0
	for n := range awards {
		total += n
	}
	for e := range errs {
		must(e)
	}
	if total != 100 {
		t.Fatal("first-place race", total)
	}
}
