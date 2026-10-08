package importer

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TEST_DATABASE_DSN must point to a disposable database with migrations applied.
func TestImportIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_DSN to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	p := validPlace()
	p.Slug = "import-integration-test"
	defer pool.Exec(context.Background(), "DELETE FROM places WHERE slug=$1", p.Slug)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if err := Import(ctx, pool, "minsk", []Place{p}, now); err != nil {
			t.Fatal(err)
		}
	}
	var id int64
	var count int
	if err := pool.QueryRow(ctx, "SELECT id FROM places WHERE slug=$1", p.Slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"places", "place_categories", "opening_hours", "ticket_prices", "place_links"} {
		query := "SELECT COUNT(*) FROM " + table + " WHERE place_id=$1"
		if table == "places" {
			query = "SELECT COUNT(*) FROM places WHERE id=$1"
		}
		if err := pool.QueryRow(ctx, query, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", table, count)
		}
	}
	p.Name = "Updated"
	p.OpeningHours = map[string][]string{"tuesday": {"09:00-10:00"}}
	p.OpeningExceptions = []Exception{{Date: "2026-10-09", Closed: true}}
	if err := Import(ctx, pool, "minsk", []Place{p}, now); err != nil {
		t.Fatal(err)
	}
	var updatedID int64
	var name string
	var weekday int
	if err := pool.QueryRow(ctx, "SELECT id,name FROM places WHERE slug=$1", p.Slug).Scan(&updatedID, &name); err != nil {
		t.Fatal(err)
	}
	if updatedID != id || name != "Updated" {
		t.Fatalf("update changed identity or failed: %d %q", updatedID, name)
	}
	if err := pool.QueryRow(ctx, "SELECT weekday FROM opening_hours WHERE place_id=$1", id).Scan(&weekday); err != nil {
		t.Fatal(err)
	}
	if weekday != 2 {
		t.Fatalf("old hours not replaced: weekday=%d", weekday)
	}
	bad := validPlace()
	bad.Slug = "invalid"
	bad.Importance = 0
	p.Name = "Must not persist"
	if err := Import(ctx, pool, "minsk", []Place{p, bad}, now); err == nil {
		t.Fatal("expected validation error")
	}
	if err := pool.QueryRow(ctx, "SELECT name FROM places WHERE id=$1", id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Updated" {
		t.Fatal("invalid batch changed existing data")
	}
	// Force a database error after saving the first place to check transaction rollback.
	bad = validPlace()
	bad.Slug = "invalid"
	bad.OpeningExceptions = []Exception{{Date: "2026-10-09", Closed: true, Note: string(make([]byte, 501))}}
	if err := Import(ctx, pool, "minsk", []Place{p, bad}, now); err == nil {
		t.Fatal("expected database error")
	}
	if err := pool.QueryRow(ctx, "SELECT name FROM places WHERE id=$1", id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Updated" {
		t.Fatal("failed transaction changed existing data")
	}
}
