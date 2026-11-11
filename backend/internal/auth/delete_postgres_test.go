package auth

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/history"
	"os"
	"testing"
	"time"
)

func TestDeleteAccountAndHistoryPostgres(t *testing.T) {
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
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	repo := Postgres{pool}
	s, err := New(repo)
	must(err)
	tag := uuid.NewString()
	password := "long password for deletion"
	a, err := s.Register(ctx, tag+"@example.invalid", password)
	must(err)
	b, err := s.Register(ctx, "other-"+tag+"@example.invalid", password)
	must(err)
	var city, place, visit int64
	photo := uuid.NewString()
	key := "deletion-test/" + tag
	defer func() {
		pool.Exec(context.Background(), "DELETE FROM users WHERE id=ANY($1)", []int64{a.User.ID, b.User.ID})
		pool.Exec(context.Background(), "DELETE FROM cities WHERE id=$1", city)
		pool.Exec(context.Background(), "DELETE FROM media_deletions WHERE object_key=$1", key)
	}()
	must(pool.QueryRow(ctx, "INSERT INTO cities(slug,name,center_lat,center_lon) VALUES($1,'Delete test',53,27) RETURNING id", tag).Scan(&city))
	must(pool.QueryRow(ctx, "INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at) VALUES($1,'fixture','Delete test',53,27,1,10,CURRENT_DATE) RETURNING id", city).Scan(&place))
	_, err = pool.Exec(ctx, `INSERT INTO visit_photos(id,user_id,place_id,object_key,status,mime_type,size_bytes,width,height,sha256,upload_expires_at,ready_at) VALUES($1,$2,$3,$4,'ready','image/jpeg',100,1,1,$5,NOW()+INTERVAL '15 minutes',NOW())`, photo, a.User.ID, place, key, make([]byte, 32))
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m) VALUES($1,$2,$3,$3,$4,'confirmed',NOW(),NOW(),0,5) RETURNING id`, a.User.ID, place, photo, make([]byte, 32)).Scan(&visit))
	_, err = pool.Exec(ctx, "INSERT INTO xp_transactions(user_id,place_id,visit_id,amount) VALUES($1,$2,$3,100)", a.User.ID, place, visit)
	must(err)
	h := history.Postgres{Pool: pool}
	if err = h.Hide(ctx, b.User.ID, visit); !errors.Is(err, history.ErrNotFound) {
		t.Fatal("foreign hide", err)
	}
	must(h.Hide(ctx, a.User.ID, visit))
	must(h.Hide(ctx, a.User.ID, visit))
	page, err := h.List(ctx, a.User.ID, 20, "")
	must(err)
	if len(page.Visits) != 0 {
		t.Fatal("hidden visit visible")
	}
	var xp int
	must(pool.QueryRow(ctx, "SELECT sum(amount) FROM xp_transactions WHERE user_id=$1", a.User.ID).Scan(&xp))
	if xp != 100 {
		t.Fatal("XP removed")
	}
	if err = repo.DeleteAccount(ctx, a.User.ID, "wrong password value"); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, a.AccessToken); err != nil {
		t.Fatal("wrong password removed account")
	}
	must(repo.DeleteAccount(ctx, a.User.ID, password))
	if _, err = s.Authenticate(ctx, a.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("access survived", err)
	}
	if _, err = s.Refresh(ctx, a.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("refresh survived", err)
	}
	if _, err = s.Authenticate(ctx, b.AccessToken); err != nil {
		t.Fatal("other user lost session", err)
	}
	var jobs int
	must(pool.QueryRow(ctx, "SELECT count(*) FROM media_deletions WHERE object_key=$1 AND not_before>=NOW()", key).Scan(&jobs))
	if jobs != 1 {
		t.Fatal("durable upload lease missing")
	}
	var count int
	must(pool.QueryRow(ctx, "SELECT count(*) FROM visits WHERE user_id=$1", a.User.ID).Scan(&count))
	if count != 0 {
		t.Fatal("visits survived")
	}
}
