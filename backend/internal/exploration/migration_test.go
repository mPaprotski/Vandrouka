package exploration

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAchievementMigrationBackfill(t *testing.T) {
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
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	schema := pgx.Identifier{"award_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	_, err = tx.Exec(ctx, "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema+"; CREATE TABLE users(id BIGINT PRIMARY KEY); CREATE TABLE xp_transactions(id BIGSERIAL PRIMARY KEY,user_id BIGINT,created_at TIMESTAMPTZ); INSERT INTO users VALUES(1),(2)")
	must(err)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		_, err = tx.Exec(ctx, "INSERT INTO xp_transactions(user_id,created_at) VALUES(1,$1)", at.Add(time.Duration(i)*time.Hour))
		must(err)
	}
	sql, err := os.ReadFile("../../migrations/000016_achievements.sql")
	must(err)
	parts := strings.Split(string(sql), "-- +goose Down")
	_, err = tx.Exec(ctx, parts[0])
	must(err)
	for i, code := range []string{"first_discovery", "five_discoveries", "ten_discoveries"} {
		var earned time.Time
		must(tx.QueryRow(ctx, "SELECT earned_at FROM user_achievements WHERE user_id=1 AND code=$1", code).Scan(&earned))
		if !earned.Equal(at.Add(time.Duration([]int{0, 4, 9}[i]) * time.Hour)) {
			t.Fatal("backfill time", code, earned)
		}
	}
	for i := 0; i < 11; i++ {
		_, err = tx.Exec(ctx, "INSERT INTO xp_transactions(user_id,created_at) VALUES(2,$1)", at.Add(time.Duration(i)*time.Hour))
		must(err)
	}
	var count int
	must(tx.QueryRow(ctx, "SELECT count(*) FROM user_achievements WHERE user_id=2").Scan(&count))
	if count != 3 {
		t.Fatal("duplicate awards", count)
	}
	_, err = tx.Exec(ctx, parts[1])
	must(err)
}
