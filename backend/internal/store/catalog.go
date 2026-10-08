package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/places"
)

var ErrNotFound = errors.New("not found")

type Catalog struct{ pool *pgxpool.Pool }

func NewCatalog(pool *pgxpool.Pool) *Catalog { return &Catalog{pool: pool} }

func (s *Catalog) ListCities(ctx context.Context) ([]places.City, error) {
	rows, err := s.pool.Query(ctx, "SELECT id,slug,name,center_lat,center_lon,timezone FROM cities ORDER BY slug")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []places.City{}
	for rows.Next() {
		var c places.City
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.CenterLat, &c.CenterLon, &c.Timezone); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *Catalog) cityExists(ctx context.Context, slug string) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM cities WHERE slug=$1)", slug).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *Catalog) ListCategories(ctx context.Context, city string) ([]places.Category, error) {
	if err := s.cityExists(ctx, city); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT c.slug,c.name,COUNT(p.id) FROM categories c
 LEFT JOIN place_categories pc ON pc.category_id=c.id
 LEFT JOIN places p ON p.id=pc.place_id AND p.is_active AND p.city_id=(SELECT id FROM cities WHERE slug=$1)
 GROUP BY c.id ORDER BY c.slug`, city)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []places.Category{}
	for rows.Next() {
		var c places.Category
		if err := rows.Scan(&c.Slug, &c.Name, &c.PlacesCount); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// Correlated aggregates avoid multiplying related rows and provide a consistent snapshot.
const placeSelect = `SELECT p.id,p.slug,c.slug,p.name,COALESCE(p.summary,''),p.lat,p.lon,
 p.importance,p.must_see,p.visit_minutes,p.hours_known,p.price_known,p.verified_at::text,p.updated_at,
 COALESCE((SELECT jsonb_agg(cat.slug ORDER BY cat.slug) FROM place_categories pc JOIN categories cat ON cat.id=pc.category_id WHERE pc.place_id=p.id),'[]'),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('weekday',h.weekday,'open_time',to_char(h.open_time,'HH24:MI'),'close_time',CASE WHEN h.close_time='24:00'::time THEN '24:00' ELSE to_char(h.close_time,'HH24:MI') END) ORDER BY h.weekday,h.open_time) FROM opening_hours h WHERE h.place_id=p.id),'[]'),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('date',e.date::text,'closed',e.is_closed,'open_time',to_char(e.open_time,'HH24:MI'),'close_time',CASE WHEN e.close_time='24:00'::time THEN '24:00' ELSE to_char(e.close_time,'HH24:MI') END,'note',COALESCE(e.note,'')) ORDER BY e.date) FROM opening_exceptions e WHERE e.place_id=p.id),'[]'),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('kind',t.kind,'amount',t.amount,'currency',t.currency) ORDER BY t.kind) FROM ticket_prices t WHERE t.place_id=p.id),'[]'),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('kind',l.kind,'url',l.url,'title',COALESCE(l.title,'')) ORDER BY l.id) FROM place_links l WHERE l.place_id=p.id),'[]')
 FROM places p JOIN cities c ON c.id=p.city_id `

func scanPlace(row pgx.Row) (places.Place, error) {
	var p places.Place
	var categories, hours, exceptions, prices, links []byte
	err := row.Scan(&p.ID, &p.Slug, &p.City, &p.Name, &p.Summary, &p.Lat, &p.Lon, &p.Importance, &p.MustSee, &p.VisitMinutes, &p.HoursKnown, &p.PriceKnown, &p.VerifiedAt, &p.UpdatedAt, &categories, &hours, &exceptions, &prices, &links)
	if err != nil {
		return p, err
	}
	for _, item := range []struct {
		data   []byte
		target any
	}{{categories, &p.Categories}, {hours, &p.OpeningHours}, {exceptions, &p.OpeningExceptions}, {prices, &p.Prices}, {links, &p.Links}} {
		if err := json.Unmarshal(item.data, item.target); err != nil {
			return p, fmt.Errorf("decode place %d: %w", p.ID, err)
		}
	}
	return p, nil
}

func (s *Catalog) ListPlaces(ctx context.Context, city, category, query string) ([]places.Place, error) {
	if err := s.cityExists(ctx, city); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, placeSelect+`WHERE c.slug=$1 AND p.is_active
 AND ($2='' OR EXISTS(SELECT 1 FROM place_categories pc JOIN categories cat ON cat.id=pc.category_id WHERE pc.place_id=p.id AND cat.slug=$2))
 AND ($3='' OR strpos(lower(p.name),lower($3))>0 OR strpos(lower(COALESCE(p.summary,'')),lower($3))>0)
 ORDER BY p.importance DESC,p.slug`, city, category, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []places.Place{}
	for rows.Next() {
		p, err := scanPlace(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Catalog) GetPlace(ctx context.Context, id int64) (places.Place, error) {
	p, err := scanPlace(s.pool.QueryRow(ctx, placeSelect+"WHERE p.id=$1 AND p.is_active", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}
