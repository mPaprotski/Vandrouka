package history

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestHistoryPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tag := uuid.NewString()
	var owner, other, city, place int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id", tag+"@example.invalid").Scan(&owner))
	defer func() {
		pool.Exec(context.Background(), "DELETE FROM users WHERE id=ANY($1)", []int64{owner, other})
		pool.Exec(context.Background(), "DELETE FROM cities WHERE id=$1", city)
		pool.Exec(context.Background(), "DELETE FROM media_deletions WHERE object_key LIKE $1", "history-test/"+tag+"/%")
	}()
	must(pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id", "other-"+tag+"@example.invalid").Scan(&other))
	must(pool.QueryRow(ctx, "INSERT INTO cities(slug,name,center_lat,center_lon,timezone) VALUES($1,'History fixture',53.9,27.5,'Europe/Minsk') RETURNING id", tag).Scan(&city))
	must(pool.QueryRow(ctx, "INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at) VALUES($1,'fixture','History fixture',53.9,27.5,1,10,CURRENT_DATE) RETURNING id", city).Scan(&place))
	at := time.Date(2026, 10, 30, 12, 0, 0, 123456000, time.UTC)
	insert := func(user int64, when time.Time, hidden, rejected bool) (int64, string) {
		t.Helper()
		photo := uuid.NewString()
		var id int64
		_, err := pool.Exec(ctx, `INSERT INTO visit_photos(id,user_id,place_id,object_key,status,mime_type,size_bytes,width,height,sha256,upload_expires_at,ready_at) VALUES($1,$2,$3,$4,'ready','image/jpeg',100,1,1,$5,NOW()+INTERVAL '15 minutes',NOW())`, photo, user, place, fmt.Sprintf("history-test/%s/%s", tag, photo), make([]byte, 32))
		must(err)
		if rejected {
			must(pool.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,rejection_code,created_at) VALUES($1,$2,$3,$3,$4,'rejected','too_far',$5) RETURNING id`, user, place, photo, make([]byte, 32), when).Scan(&id))
		} else {
			must(pool.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m,created_at) VALUES($1,$2,$3,$3,$4,'confirmed',$5,$5,0,5,$5) RETURNING id`, user, place, photo, make([]byte, 32), when).Scan(&id))
		}
		if hidden {
			_, err = pool.Exec(ctx, "UPDATE visits SET deleted_at=NOW() WHERE id=$1", id)
			must(err)
		}
		return id, photo
	}
	repo := Postgres{pool}
	empty, err := repo.List(ctx, owner, 2, "")
	must(err)
	if len(empty.Visits) != 0 || empty.Visits == nil || empty.NextCursor != nil {
		t.Fatal(empty)
	}
	oldest, oldPhoto := insert(owner, at.Add(-time.Second), false, false)
	middle, _ := insert(owner, at, false, false)
	newest, _ := insert(owner, at, false, false)
	insert(other, at.Add(time.Hour), false, false)
	insert(owner, at.Add(time.Hour), true, false)
	insert(owner, at.Add(time.Hour), false, true)
	_, err = pool.Exec(ctx, "INSERT INTO xp_transactions(user_id,place_id,visit_id,amount) VALUES($1,$2,$3,100)", owner, place, oldest)
	must(err)
	_, err = pool.Exec(ctx, "UPDATE visit_photos SET deleted_at=NOW() WHERE id=$1", oldPhoto)
	must(err)
	_, err = pool.Exec(ctx, "UPDATE places SET is_active=false WHERE id=$1", place)
	must(err)
	first, err := repo.List(ctx, owner, 2, "")
	must(err)
	if len(first.Visits) != 2 || first.Visits[0].ID != newest || first.Visits[1].ID != middle || first.NextCursor == nil {
		t.Fatal(first)
	}
	insert(owner, at.Add(time.Minute), false, false) // New head must not shift the next page.
	last, err := repo.List(ctx, owner, 2, *first.NextCursor)
	must(err)
	if len(last.Visits) != 1 || last.Visits[0].ID != oldest || last.NextCursor != nil || last.Visits[0].PhotoID != nil || last.Visits[0].XPAwarded != 100 || last.Visits[0].PlaceActive {
		t.Fatal(last)
	}
	for _, tc := range []struct {
		owner  int64
		limit  int
		cursor string
	}{{0, 20, ""}, {owner, 0, ""}, {owner, 51, ""}, {owner, 20, "bad"}} {
		if _, err := repo.List(ctx, tc.owner, tc.limit, tc.cursor); err != ErrInvalid {
			t.Fatal(tc, err)
		}
	}
}
