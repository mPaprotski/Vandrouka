package progress

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestProgressLedgerPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tag := uuid.NewString()
	var owner, city int64
	if err = pool.QueryRow(ctx, "INSERT INTO users(email,password_hash) VALUES($1,'test') RETURNING id", tag+"@example.com").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", owner)
		pool.Exec(context.Background(), "DELETE FROM cities WHERE id=$1", city)
		pool.Exec(context.Background(), "DELETE FROM media_deletions WHERE object_key LIKE $1", "progress-test/"+tag+"/%")
	}()
	repo := Postgres{pool}
	empty, err := repo.Read(ctx, owner)
	if err != nil || empty.TotalXP != 0 || empty.Level != 1 {
		t.Fatal(empty, err)
	}
	// Own fixtures: this test also runs on a freshly migrated CI database with no imported catalog.
	if err = pool.QueryRow(ctx, "INSERT INTO cities(slug,name,center_lat,center_lon,timezone) VALUES($1,'Progress fixture',53.9,27.5,'Europe/Minsk') RETURNING id", tag).Scan(&city); err != nil {
		t.Fatal(err)
	}
	var places []int64
	for i := 0; i < 3; i++ {
		var id int64
		if err = pool.QueryRow(ctx, "INSERT INTO places(city_id,slug,name,lat,lon,importance,visit_minutes,verified_at) VALUES($1,$2,'Progress fixture',53.9,27.5,1,10,CURRENT_DATE) RETURNING id", city, fmt.Sprintf("fixture-%d", i)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		places = append(places, id)
	}
	for i, place := range places {
		photo := uuid.NewString()
		var visit int64
		_, err = pool.Exec(ctx, `INSERT INTO visit_photos(id,user_id,place_id,object_key,status,mime_type,size_bytes,width,height,sha256,upload_expires_at,ready_at)
   VALUES($1,$2,$3,$4,'ready','image/jpeg',100,1,1,$5,NOW()+INTERVAL '15 minutes',NOW())`, photo, owner, place, fmt.Sprintf("progress-test/%s/%d", tag, i), make([]byte, 32))
		if err != nil {
			t.Fatal(err)
		}
		err = pool.QueryRow(ctx, `INSERT INTO visits(user_id,place_id,photo_id,idempotency_key,request_hash,status,confirmed_at,location_observed_at,distance_m,accuracy_m)
   VALUES($1,$2,$3,$3,$4,'confirmed',NOW(),NOW(),0,5) RETURNING id`, owner, place, photo, make([]byte, 32)).Scan(&visit)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, "INSERT INTO xp_transactions(user_id,place_id,visit_id,amount) VALUES($1,$2,$3,100)", owner, place, visit); err != nil {
			t.Fatal(err)
		}
		state, err := repo.Read(ctx, owner)
		if err != nil || state.TotalXP != int64((i+1)*100) {
			t.Fatal(state, err)
		}
	}
	if _, err = pool.Exec(ctx, "UPDATE visits SET deleted_at=NOW() WHERE user_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE visit_photos SET deleted_at=NOW() WHERE user_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	state, err := repo.Read(ctx, owner)
	if err != nil || state.TotalXP != 300 || state.Level != 3 || state.LevelStartXP != 300 || state.NextLevelXP == nil || *state.NextLevelXP != 600 || state.XPToNextLevel == nil || *state.XPToNextLevel != 300 {
		t.Fatal(state, err)
	}
}
