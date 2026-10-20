package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

type memoryObjects struct {
	mu                  sync.Mutex
	items               map[string][]byte
	failPut, failDelete bool
	reads               int
}

func (o *memoryObjects) Init(context.Context) error { return nil }
func (o *memoryObjects) Put(_ context.Context, key string, body []byte, _ string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.items[key] = bytes.Clone(body)
	if o.failPut {
		return ErrUnavailable
	}
	return nil
}
func (o *memoryObjects) Get(_ context.Context, key string) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reads++
	value, ok := o.items[key]
	if !ok {
		return nil, ErrUnavailable
	}
	return bytes.Clone(value), nil
}
func (o *memoryObjects) Delete(_ context.Context, key string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failDelete {
		return ErrUnavailable
	}
	delete(o.items, key)
	return nil
}
func (o *memoryObjects) has(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.items[key]
	return ok
}
func TestPrivatePhotosPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	suffix := fmt.Sprint(time.Now().UnixNano())
	var owner, other, place int64
	err = pool.QueryRow(ctx, `INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at)
 SELECT id,$1,'Media fixture',53.9,27.5,1,10,CURRENT_DATE FROM cities WHERE slug='minsk' RETURNING id`, "media-fixture-"+suffix).Scan(&place)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), "DELETE FROM places WHERE id=$1", place)
	for i, target := range []*int64{&owner, &other} {
		err = pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'test-only') RETURNING id", fmt.Sprintf("media-%s-%d@example.com", suffix, i)).Scan(target)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", *target)
	}
	// Remove only our own deletion jobs after cascading test-user cleanup.
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var keys []string
		if err := pool.QueryRow(clean, "SELECT COALESCE(array_agg(object_key),ARRAY[]::text[]) FROM visit_photos WHERE user_id=ANY($1)", []int64{owner, other}).Scan(&keys); err != nil {
			t.Error(err)
			return
		}
		if _, err := pool.Exec(clean, "DELETE FROM users WHERE id=ANY($1)", []int64{owner, other}); err != nil {
			t.Error(err)
			return
		}
		if _, err := pool.Exec(clean, "DELETE FROM media_deletions WHERE object_key=ANY($1)", keys); err != nil {
			t.Error(err)
		}
	}()
	objects := &memoryObjects{items: map[string][]byte{}}
	repo := Postgres{pool}
	service := New(repo, objects)
	upload := func() visits.Photo {
		t.Helper()
		p, err := service.Upload(ctx, owner, place, bytes.NewReader(fixtureImage(t, "jpeg")), "image/jpeg")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := upload()
	if first.Status != visits.Ready || !objects.has(first.ObjectKey) {
		t.Fatal("not ready")
	}
	if _, _, err = service.Read(ctx, other, first.ID); !errors.Is(err, ErrNotFound) || objects.reads != 0 {
		t.Fatal("foreign photo reached store", err)
	}
	if err = service.Delete(ctx, other, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign delete", err)
	}
	if _, _, err = service.Read(ctx, owner, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err = service.Read(ctx, owner, first.ID); err != nil {
		t.Fatal("owner read", err)
	}
	objects.mu.Lock()
	original := bytes.Clone(objects.items[first.ObjectKey])
	objects.items[first.ObjectKey] = bytes.Clone(original)
	objects.items[first.ObjectKey][0] ^= 1
	objects.mu.Unlock()
	if _, _, err = service.Read(ctx, owner, first.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("corrupt object accepted", err)
	}
	objects.mu.Lock()
	objects.items[first.ObjectKey] = original
	objects.mu.Unlock()
	// Existing confirmed visits and awards survive photo deletion.
	var visitID int64
	err = pool.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m)
 VALUES($1,$2,$3,$3,decode(repeat('01',32),'hex'),'confirmed',NOW(),NOW(),10,5) RETURNING id`, owner, place, first.ID).Scan(&visitID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO xp_transactions(user_id,place_id,visit_id,amount) VALUES($1,$2,$3,100)", owner, place, visitID); err != nil {
		t.Fatal(err)
	}

	// Automatic draft cleanup must not remove a confirmed photo; explicit deletion below still can.
	if err = service.Discard(ctx, owner, first.ID); !errors.Is(err, ErrConfirmed) {
		t.Fatal("confirmed draft cleanup", err)
	}
	if _, _, err = service.Read(ctx, owner, first.ID); err != nil {
		t.Fatal("discard hid confirmed photo", err)
	}
	if err = service.Discard(ctx, other, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign discard", err)
	}
	abandoned := upload()
	if err = service.Discard(ctx, owner, abandoned.ID); err != nil {
		t.Fatal(err)
	}
	if objects.has(abandoned.ObjectKey) {
		t.Fatal("draft was not removed")
	}
	if err = service.Discard(ctx, owner, abandoned.ID); err != nil {
		t.Fatal("discard is not idempotent", err)
	}
	// Start discard while a confirmation transaction holds the photo lock. Once
	// confirmation commits, discard must see it in a fresh statement snapshot.
	racing := upload()
	confirmation, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer confirmation.Rollback(context.Background())
	var backendPID int
	if err = confirmation.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil {
		t.Fatal(err)
	}
	if _, err = confirmation.Exec(ctx, "SELECT 1 FROM visit_photos WHERE id=$1 FOR UPDATE", racing.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = confirmation.Exec(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m)
 VALUES($1,$2,$3,$3,decode(repeat('02',32),'hex'),'confirmed',NOW(),NOW(),10,5)`, owner, place, racing.ID); err != nil {
		t.Fatal(err)
	}
	discardDone := make(chan error, 1)
	go func() { discardDone <- service.Discard(ctx, owner, racing.ID) }()
	wait, cancelWait := context.WithTimeout(ctx, 5*time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	blocked := false
	for !blocked {
		if err = pool.QueryRow(wait, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)) AND a.query LIKE 'SELECT object_key FROM visit_photos%')", backendPID).Scan(&blocked); err != nil {
			break
		}
		if !blocked {
			select {
			case <-wait.Done():
				err = wait.Err()
			case <-ticker.C:
			}
		}
		if err != nil {
			break
		}
	}
	ticker.Stop()
	cancelWait()
	if !blocked {
		confirmation.Rollback(context.Background())
		<-discardDone
		t.Fatal("discard did not wait for photo lock", err)
	}
	if err = confirmation.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-discardDone; !errors.Is(err, ErrConfirmed) {
		t.Fatal("late discard removed confirmation", err)
	}
	if _, _, err = service.Read(ctx, owner, racing.ID); err != nil {
		t.Fatal("racing photo unreadable", err)
	}
	if _, err = pool.Exec(ctx, "UPDATE visit_photos SET created_at=NOW()-INTERVAL '25 hours' WHERE id=$1", racing.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.Read(ctx, owner, racing.ID); err != nil {
		t.Fatal("TTL removed confirmed photo", err)
	}
	objects.failDelete = true
	if err = service.Delete(ctx, owner, first.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("deletion failure not reported", err)
	}
	if _, _, err = service.Read(ctx, owner, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("tombstone still readable", err)
	}
	var pending int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM media_deletions WHERE object_key=$1", first.ObjectKey).Scan(&pending); err != nil || pending != 1 {
		t.Fatal("deletion not durable", err)
	}
	if err = service.Cleanup(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("worker did not report deletion failure", err)
	}
	var attempts int
	if err = pool.QueryRow(ctx, "SELECT attempts FROM media_deletions WHERE object_key=$1", first.ObjectKey).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("retry state not persisted", attempts, err)
	}
	objects.failDelete = false
	retryTime := time.Now().Add(61 * time.Second)
	service.Now = func() time.Time { return retryTime }
	if err = service.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if objects.has(first.ObjectKey) {
		t.Fatal("delete not retried")
	}
	if err = service.Delete(ctx, owner, first.ID); err != nil {
		t.Fatal("delete not idempotent", err)
	}
	var xp int
	if err = pool.QueryRow(ctx, "SELECT SUM(amount) FROM xp_transactions WHERE user_id=$1", owner).Scan(&xp); err != nil || xp != 100 {
		t.Fatal("photo deletion changed XP", xp, err)
	}
	// A failed PUT can leave bytes externally: preserve a cleanup record after abort.
	objects.failPut = true
	if _, err = service.Upload(ctx, owner, place, bytes.NewReader(fixtureImage(t, "png")), "image/png"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	var failedID, failedKey string
	err = pool.QueryRow(ctx, "SELECT id::text,object_key FROM visit_photos WHERE user_id=$1 AND status='failed' AND deleted_at IS NOT NULL", owner).Scan(&failedID, &failedKey)
	if err != nil || !objects.has(failedKey) {
		t.Fatal("failed PUT recovery", err)
	}
	if _, _, err = service.Read(ctx, owner, failedID); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed image exposed")
	}
	objects.failPut = false
	future := time.Now().Add(16 * time.Minute)
	service.Now = func() time.Time { return future }
	if err = service.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if objects.has(failedKey) {
		t.Fatal("failed PUT bytes survived cleanup")
	}
	// Orphaned uploading rows expire even when the process crashed before abort.
	waiting, err := repo.Create(ctx, owner, place, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	objects.Put(ctx, waiting.ObjectKey, []byte("partial"), "image/jpeg")
	if err = service.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if objects.has(waiting.ObjectKey) {
		t.Fatal("orphan survived cleanup")
	}
	// Old unused ready photos expire; confirmed photos are retained.
	old := upload()
	used := upload()
	if _, err = pool.Exec(ctx, "UPDATE visit_photos SET created_at=NOW()-INTERVAL '2 days' WHERE id IN ($1,$2)", old.ID, used.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m) VALUES($1,$2,$3,$3,decode(repeat('02',32),'hex'),'confirmed',NOW(),NOW(),5,3)`, owner, place, used.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if objects.has(old.ObjectKey) || !objects.has(used.ObjectKey) {
		t.Fatal("unused TTL removed wrong objects")
	}
	// Janitor must skip a photo locked by a live upload, even if its lease expires.
	locked, err := repo.Create(ctx, owner, place, "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := Sanitize(bytes.NewReader(fixtureImage(t, "jpeg")), "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := repo.Complete(ctx, owner, locked.ID, cleaned, func(ctx context.Context, key string) error {
			close(started)
			<-release
			return objects.Put(ctx, key, cleaned.Body, cleaned.MIME)
		})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("upload not locked")
	}
	cleanupErr := service.Cleanup(ctx)
	close(release)
	completeErr := <-done
	if cleanupErr != nil || completeErr != nil {
		t.Fatal("upload/cleanup race", cleanupErr, completeErr)
	}
	if _, _, err = service.Read(ctx, owner, locked.ID); err != nil {
		t.Fatal("janitor hid a live upload", err)
	}
	// Metadata deletion cascades still enqueue object cleanup; jobs have no owner FK.
	if _, err = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", owner); err != nil {
		t.Fatal(err)
	}
	if err = service.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if objects.has(used.ObjectKey) || objects.has(locked.ObjectKey) {
		t.Fatal("metadata cascade orphaned objects")
	}
}
