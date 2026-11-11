// Package history reads private confirmed visits using stable keyset pagination.
package history

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("invalid history page")

type Item struct {
	ID          int64     `json:"id"`
	PlaceID     int64     `json:"place_id"`
	PlaceName   string    `json:"place_name"`
	PlaceActive bool      `json:"place_active"`
	Timezone    string    `json:"timezone"`
	CreatedAt   time.Time `json:"created_at"`
	ConfirmedAt time.Time `json:"confirmed_at"`
	PhotoID     *string   `json:"photo_id"`
	XPAwarded   int       `json:"xp_awarded"`
}
type Page struct {
	Visits     []Item  `json:"visits"`
	NextCursor *string `json:"next_cursor"`
}
type cursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"at"`
	ID        int64     `json:"id"`
}

func encodeCursor(item Item) string {
	body, _ := json.Marshal(cursor{1, item.CreatedAt.UTC(), item.ID})
	return base64.RawURLEncoding.EncodeToString(body)
}
func decodeCursor(value string) (cursor, error) {
	var c cursor
	if len(value) > 256 {
		return c, ErrInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(body) != value {
		return c, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.Version != 1 || c.ID < 1 || c.CreatedAt.IsZero() {
		return cursor{}, ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return cursor{}, ErrInvalid
	}
	return c, nil
}

type Postgres struct{ Pool *pgxpool.Pool }

func (p Postgres) List(ctx context.Context, owner int64, limit int, token string) (Page, error) {
	page := Page{Visits: make([]Item, 0)}
	if owner < 1 || limit < 1 || limit > 50 {
		return page, ErrInvalid
	}
	var before *time.Time
	var id int64
	if token != "" {
		c, err := decodeCursor(token)
		if err != nil {
			return page, err
		}
		before = &c.CreatedAt
		id = c.ID
	}
	rows, err := p.Pool.Query(ctx, `SELECT v.id,v.place_id,p.name,p.is_active,c.timezone,v.created_at,v.confirmed_at,
 CASE WHEN f.status='ready' AND f.deleted_at IS NULL THEN f.id::text ELSE NULL END,
 COALESCE(x.amount,0)
 FROM visits v JOIN places p ON p.id=v.place_id JOIN cities c ON c.id=p.city_id
 JOIN visit_photos f ON f.id=v.photo_id
 LEFT JOIN xp_transactions x ON x.visit_id=v.id
 WHERE v.user_id=$1 AND v.status='confirmed' AND v.deleted_at IS NULL
 AND ($2::timestamptz IS NULL OR (v.created_at,v.id)<($2,$3))
 ORDER BY v.created_at DESC,v.id DESC LIMIT $4`, owner, before, id, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Item
		if err = rows.Scan(&item.ID, &item.PlaceID, &item.PlaceName, &item.PlaceActive, &item.Timezone, &item.CreatedAt, &item.ConfirmedAt, &item.PhotoID, &item.XPAwarded); err != nil {
			return page, err
		}
		page.Visits = append(page.Visits, item)
	}
	if rows.Err() != nil {
		return page, rows.Err()
	}
	if len(page.Visits) > limit {
		page.Visits = page.Visits[:limit]
		next := encodeCursor(page.Visits[limit-1])
		page.NextCursor = &next
	}
	return page, nil
}
