package exploration

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

func TestDiscoveryAwardsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	tag := uuid.NewString()
	var owner, other, city int64
	must(pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", tag+"@example.invalid").Scan(&owner))
	defer func() {
		pool.Exec(context.Background(), "DELETE FROM users WHERE id=ANY($1)", []int64{owner, other})
		pool.Exec(context.Background(), "DELETE FROM cities WHERE id=$1", city)
		pool.Exec(context.Background(), "DELETE FROM media_deletions WHERE object_key LIKE $1", "exploration-test/"+tag+"/%")
	}()
	must(pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id", "other-"+tag+"@example.invalid").Scan(&other))
	must(pool.QueryRow(ctx, "INSERT INTO cities(slug,name,center_lat,center_lon) VALUES($1,'Discovery fixture',53,27) RETURNING id", tag).Scan(&city))
	checkCollection := func(want int) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		must(err)
		defer tx.Rollback(ctx)
		c, err := readCollection(ctx, tx, owner, tag)
		must(err)
		if c.Total != 3 || c.Visited != want || (c.CompletedAt != nil) != (want == 3) {
			t.Fatal("collection", c)
		}
		if want == 0 {
			for _, p := range c.Places {
				if p.PlaceID != nil || p.Available || p.Visited {
					t.Fatal("missing place became available")
				}
			}
		}
	}
	checkCollection(0)
	repo := Postgres{pool}
	empty, err := repo.Read(ctx, owner)
	must(err)
	if empty.DiscoveredPlaces != 0 || len(empty.Achievements) != 3 || empty.Achievements[0].EarnedAt != nil {
		t.Fatal(empty)
	}
	var visits, places []int64
	for i := 0; i < 10; i++ {
		var place, visit int64
		photo := uuid.NewString()
		slug := fmt.Sprint(i)
		if i < 3 {
			slug = centerSlugs[i]
		}
		must(pool.QueryRow(ctx, "INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at) VALUES($1,$2,'Fixture',53,27,1,10,CURRENT_DATE) RETURNING id", city, slug).Scan(&place))
		_, err = pool.Exec(ctx, `INSERT INTO visit_photos(id,user_id,place_id,object_key,status,mime_type,size_bytes,width,height,sha256,upload_expires_at,ready_at) VALUES($1,$2,$3,$4,'ready','image/jpeg',100,1,1,$5,NOW()+INTERVAL '15 minutes',NOW())`, photo, owner, place, "exploration-test/"+tag+"/"+photo, make([]byte, 32))
		must(err)
		must(pool.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m) VALUES($1,$2,$3,$3,$4,'confirmed',NOW(),NOW(),0,5) RETURNING id`, owner, place, photo, make([]byte, 32)).Scan(&visit))
		visits = append(visits, visit)
		places = append(places, place)
		_, err = pool.Exec(ctx, "INSERT INTO xp_transactions(user_id,place_id,visit_id,amount) VALUES($1,$2,$3,100)", owner, place, visit)
		must(err)
		wantCollection := i + 1
		if wantCollection > 3 {
			wantCollection = 3
		}
		checkCollection(wantCollection)
		state, err := repo.Read(ctx, owner)
		must(err)
		if state.DiscoveredPlaces != int64(i+1) {
			t.Fatal(state)
		}
		for j, target := range []int{1, 5, 10} {
			if (state.Achievements[j].EarnedAt != nil) != (i+1 >= target) {
				t.Fatal(i, state.Achievements)
			}
		}
	}
	before, err := repo.Read(ctx, owner)
	must(err)
	// Replaying duplicate rewards concurrently cannot change the durable awards.
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := pool.Exec(ctx, "INSERT INTO xp_transactions(user_id,place_id,visit_id,amount) VALUES($1,$2,$3,100) ON CONFLICT DO NOTHING", owner, places[0], visits[0])
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(err)
	}
	_, err = pool.Exec(ctx, "UPDATE visits SET deleted_at=NOW() WHERE user_id=$1", owner)
	must(err)
	_, err = pool.Exec(ctx, "UPDATE visit_photos SET deleted_at=NOW() WHERE user_id=$1", owner)
	must(err)
	_, err = pool.Exec(ctx, "UPDATE places SET is_active=false WHERE id=$1", places[0])
	must(err)
	checkCollection(3)
	after, err := repo.Read(ctx, owner)
	must(err)
	for i := range before.Achievements {
		if !before.Achievements[i].EarnedAt.Equal(*after.Achievements[i].EarnedAt) {
			t.Fatal("award changed")
		}
	}
	foreign, err := repo.Read(ctx, other)
	must(err)
	if foreign.DiscoveredPlaces != 0 || foreign.Achievements[0].EarnedAt != nil {
		t.Fatal("owner leak")
	}
	_, err = pool.Exec(ctx, "DELETE FROM users WHERE id=$1", owner)
	must(err)
	var awards int
	must(pool.QueryRow(ctx, "SELECT count(*) FROM user_achievements WHERE user_id=$1", owner).Scan(&awards))
	if awards != 0 {
		t.Fatal("account deletion retained awards")
	}
}
