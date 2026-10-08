package importer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

func ReadDir(dir string) ([]Place, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	places := make([]Place, 0, len(entries))
	slugs := map[string]bool{}
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		var p Place
		if err := decoder.Decode(&p); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%s: expected exactly one YAML document", entry.Name())
		}
		if slugs[p.Slug] {
			return nil, fmt.Errorf("%s: duplicate slug %q", entry.Name(), p.Slug)
		}
		slugs[p.Slug] = true
		places = append(places, p)
	}
	if len(places) == 0 {
		return nil, fmt.Errorf("no YAML places in %s", dir)
	}
	return places, nil
}

// Import validates the complete dataset before writing; a failed import changes nothing.
func Import(ctx context.Context, pool *pgxpool.Pool, city string, places []Place, now time.Time) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var cityID int64
	if err := tx.QueryRow(ctx, "SELECT id FROM cities WHERE slug=$1", city).Scan(&cityID); err != nil {
		return fmt.Errorf("city %q: %w", city, err)
	}
	rows, err := tx.Query(ctx, "SELECT slug,id FROM categories")
	if err != nil {
		return err
	}
	categories := map[string]int64{}
	for rows.Next() {
		var slug string
		var id int64
		if err := rows.Scan(&slug, &id); err != nil {
			rows.Close()
			return err
		}
		categories[slug] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, p := range places {
		if seen[p.Slug] {
			return fmt.Errorf("duplicate slug %q", p.Slug)
		}
		seen[p.Slug] = true
		if err := p.Validate(categories, city, now); err != nil {
			return fmt.Errorf("%s: %w", p.Slug, err)
		}
	}
	for _, p := range places {
		if err := save(ctx, tx, cityID, p, categories); err != nil {
			return fmt.Errorf("%s: %w", p.Slug, err)
		}
	}
	return tx.Commit(ctx)
}

func save(ctx context.Context, tx pgx.Tx, cityID int64, p Place, categories map[string]int64) error {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO places
 (city_id,slug,name,summary,lat,lon,importance,must_see,visit_minutes,hours_known,price_known,verified_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::date)
 ON CONFLICT (city_id,slug) DO UPDATE SET name=EXCLUDED.name,summary=EXCLUDED.summary,
 lat=EXCLUDED.lat,lon=EXCLUDED.lon,importance=EXCLUDED.importance,must_see=EXCLUDED.must_see,
 visit_minutes=EXCLUDED.visit_minutes,hours_known=EXCLUDED.hours_known,price_known=EXCLUDED.price_known,
 verified_at=EXCLUDED.verified_at,is_active=true,updated_at=NOW() RETURNING id`,
		cityID, p.Slug, p.Name, p.Summary, p.Lat, p.Lon, p.Importance, p.MustSee, p.VisitMinutes, p.HoursKnown, p.PriceKnown, p.VerifiedAt).Scan(&id)
	if err != nil {
		return err
	}
	for _, table := range []string{"place_categories", "opening_hours", "opening_exceptions", "ticket_prices", "place_links"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE place_id=$1", id); err != nil {
			return err
		}
	}
	for _, c := range p.Categories {
		if _, err := tx.Exec(ctx, "INSERT INTO place_categories(place_id,category_id) VALUES ($1,$2)", id, categories[c]); err != nil {
			return err
		}
	}
	for day, hours := range p.OpeningHours {
		for _, h := range hours {
			a, b, _, _, _ := interval(h)
			if _, err := tx.Exec(ctx, "INSERT INTO opening_hours(place_id,weekday,open_time,close_time) VALUES ($1,$2,$3::time,$4::time)", id, weekdays[day], a, b); err != nil {
				return err
			}
		}
	}
	for _, e := range p.OpeningExceptions {
		var a, b any
		if !e.Closed {
			start, end, _, _, _ := interval(e.Hours[0])
			a = start
			b = end
		}
		if _, err := tx.Exec(ctx, "INSERT INTO opening_exceptions(place_id,date,is_closed,open_time,close_time,note) VALUES ($1,$2::date,$3,$4::time,$5::time,$6)", id, e.Date, e.Closed, a, b, e.Note); err != nil {
			return err
		}
	}
	for _, price := range p.Prices {
		if _, err := tx.Exec(ctx, "INSERT INTO ticket_prices(place_id,kind,amount,currency) VALUES ($1,$2,$3,$4)", id, price.Kind, price.Amount, price.Currency); err != nil {
			return err
		}
	}
	for _, link := range p.Links {
		if _, err := tx.Exec(ctx, "INSERT INTO place_links(place_id,url,kind,title) VALUES ($1,$2,$3,$4)", id, link.URL, link.Kind, link.Title); err != nil {
			return err
		}
	}
	return nil
}
