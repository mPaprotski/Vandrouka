package visits

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const insertConfirmed = `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m)
 VALUES($1,$2,$3,$4,decode(repeat('01',32),'hex'),'confirmed',NOW(),NOW(),10,5) RETURNING id`
const insertReward = `INSERT INTO xp_transactions(user_id,place_id,visit_id,amount) VALUES($1,$2,$3,100)`

func rejectedSQL(t *testing.T, ctx context.Context, tx pgx.Tx, code, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(ctx, "SAVEPOINT invalid_case"); err != nil {
		t.Fatal(err)
	}
	_, err := tx.Exec(ctx, query, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("expected SQLSTATE %s, got %v", code, err)
	}
	if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT invalid_case"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "RELEASE SAVEPOINT invalid_case"); err != nil {
		t.Fatal(err)
	}
}
func TestPostgresInvariants(t *testing.T) {
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
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var owner, other, place int64
	err = pool.QueryRow(ctx, `INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at)
 SELECT id,$1,'Visit fixture',53.9,27.5,1,10,CURRENT_DATE FROM cities WHERE slug='minsk' RETURNING id`, "visit-fixture-"+suffix).Scan(&place)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), "DELETE FROM places WHERE id=$1", place)
	for i, target := range []*int64{&owner, &other} {
		err = pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'test-only-not-a-real-password') RETURNING id", fmt.Sprintf("visits-%s-%d@example.com", suffix, i)).Scan(target)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", *target)
	}
	var photos [3]string
	for i := range photos {
		err = pool.QueryRow(ctx, `INSERT INTO visit_photos(user_id,place_id,object_key,status,mime_type,size_bytes,width,height,sha256,upload_expires_at,ready_at)
 VALUES($1,$2,$3,'ready','image/jpeg',1024,100,100,decode(repeat('02',32),'hex'),NOW()+INTERVAL '1 hour',NOW()) RETURNING id::text`, owner, place, fmt.Sprintf("test/%s/%d", suffix, i)).Scan(&photos[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Run("ownership-readiness-and-validation", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		key := "00000000-0000-4000-8000-000000000001"
		rejectedSQL(t, ctx, tx, "23514", insertConfirmed, other, place, photos[0], key)
		rejectedSQL(t, ctx, tx, "23503", `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,rejection_code) VALUES($1,$2,$3,$4,decode(repeat('01',32),'hex'),'rejected','too_far')`, other, place, photos[0], key)

		if _, err = tx.Exec(ctx, "UPDATE visit_photos SET deleted_at=NOW() WHERE id=$1", photos[0]); err != nil {
			t.Fatal(err)
		}
		rejectedSQL(t, ctx, tx, "23514", insertConfirmed, owner, place, photos[0], key)
		if _, err = tx.Exec(ctx, "UPDATE visit_photos SET status='uploading',ready_at=NULL WHERE id=$1", photos[1]); err != nil {
			t.Fatal(err)
		}
		rejectedSQL(t, ctx, tx, "23514", insertConfirmed, owner, place, photos[1], key)
		rejectedSQL(t, ctx, tx, "23514", "UPDATE visit_photos SET mime_type='text/html' WHERE id=$1", photos[2])
		rejectedSQL(t, ctx, tx, "23514", "UPDATE visit_photos SET size_bytes=0 WHERE id=$1", photos[2])
		rejectedSQL(t, ctx, tx, "23514", "UPDATE visit_photos SET sha256=NULL WHERE id=$1", photos[2])
		rejectedSQL(t, ctx, tx, "23514", "UPDATE visit_photos SET upload_expires_at=created_at WHERE id=$1", photos[2])
		for _, bad := range []string{"-1", "'NaN'::float8", "'Infinity'::float8"} {
			query := `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m) VALUES($1,$2,$3,$4,decode(repeat('01',32),'hex'),'confirmed',NOW(),NOW(),` + bad + `,5)`
			rejectedSQL(t, ctx, tx, "23514", query, owner, place, photos[2], key)
		}
	})
	t.Run("idempotency-photo-and-award-uniqueness", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		key1 := "00000000-0000-4000-8000-000000000011"
		key2 := "00000000-0000-4000-8000-000000000012"
		var first, second int64
		if err = tx.QueryRow(ctx, insertConfirmed, owner, place, photos[0], key1).Scan(&first); err != nil {
			t.Fatal(err)
		}
		rejectedSQL(t, ctx, tx, "23505", insertConfirmed, owner, place, photos[1], key1)
		rejectedSQL(t, ctx, tx, "23505", insertConfirmed, owner, place, photos[0], key2)
		if err = tx.QueryRow(ctx, insertConfirmed, owner, place, photos[1], key2).Scan(&second); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, insertReward, owner, place, first); err != nil {
			t.Fatal(err)
		}
		rejectedSQL(t, ctx, tx, "23505", insertReward, owner, place, second)
		rejectedSQL(t, ctx, tx, "23503", insertReward, other, place, second)
		rejectedSQL(t, ctx, tx, "23514", "UPDATE xp_transactions SET amount=200 WHERE visit_id=$1", first)
		rejectedSQL(t, ctx, tx, "23503", "DELETE FROM visits WHERE id=$1", first)
		if _, err = tx.Exec(ctx, "UPDATE visits SET deleted_at=NOW() WHERE id=$1", first); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "UPDATE visit_photos SET deleted_at=NOW() WHERE id=$1", photos[0]); err != nil {
			t.Fatal(err)
		}
		var total int
		if err = tx.QueryRow(ctx, "SELECT SUM(amount) FROM xp_transactions WHERE user_id=$1", owner).Scan(&total); err != nil || total != 100 {
			t.Fatal("soft deletion changed XP", total, err)
		}
		rejectedSQL(t, ctx, tx, "23505", insertReward, owner, place, second)
	})
	t.Run("rejected-does-not-reward-or-consume-photo", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,rejection_code)
 VALUES($1,$2,$3,'00000000-0000-4000-8000-000000000021',decode(repeat('01',32),'hex'),'rejected','too_far') RETURNING id`, owner, place, photos[0]).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		rejectedSQL(t, ctx, tx, "23503", insertReward, owner, place, id)
		var confirmed int64
		if err = tx.QueryRow(ctx, insertConfirmed, owner, place, photos[0], "00000000-0000-4000-8000-000000000022").Scan(&confirmed); err != nil {
			t.Fatal("rejected photo consumed", err)
		}
	})
	t.Run("rollback-is-atomic", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var id int64
		if err = tx.QueryRow(ctx, insertConfirmed, owner, place, photos[0], "00000000-0000-4000-8000-000000000031").Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, insertReward, owner, place, id); err != nil {
			t.Fatal(err)
		}
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var count int
		if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM visits WHERE user_id=$1)+(SELECT count(*) FROM xp_transactions WHERE user_id=$1)", owner).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial transaction persisted", count, err)
		}
	})
	t.Run("concurrent-repeat-rewards-once", func(t *testing.T) {
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				tx, err := pool.Begin(ctx)
				if err != nil {
					results <- err
					return
				}
				defer tx.Rollback(ctx)
				var id int64
				err = tx.QueryRow(ctx, insertConfirmed, owner, place, photos[i], fmt.Sprintf("00000000-0000-4000-8000-%012d", 41+i)).Scan(&id)
				if err == nil {
					_, err = tx.Exec(ctx, insertReward+" ON CONFLICT (user_id,place_id) DO NOTHING", owner, place, id)
				}
				if err == nil {
					err = tx.Commit(ctx)
				}
				results <- err
			}(i)
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal(err)
			}
		}
		var visitsCount, rewards, total int
		err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM visits WHERE user_id=$1),count(*),COALESCE(sum(amount),0) FROM xp_transactions WHERE user_id=$1`, owner).Scan(&visitsCount, &rewards, &total)
		if err != nil || visitsCount != 2 || rewards != 1 || total != 100 {
			t.Fatal(visitsCount, rewards, total, err)
		}
	})
}
