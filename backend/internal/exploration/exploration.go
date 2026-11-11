// Package exploration exposes durable discovery achievements. It never awards extra XP.
package exploration

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Achievement struct {
	Code     string     `json:"code"`
	Name     string     `json:"name"`
	Target   int64      `json:"target"`
	Progress int64      `json:"progress"`
	EarnedAt *time.Time `json:"earned_at"`
}
type CollectionPlace struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	PlaceID   *int64 `json:"place_id"`
	Available bool   `json:"available"`
	Visited   bool   `json:"visited"`
}
type Collection struct {
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	City        string            `json:"city"`
	Total       int               `json:"total"`
	Visited     int               `json:"visited"`
	CompletedAt *time.Time        `json:"completed_at"`
	Places      []CollectionPlace `json:"places"`
}

var centerSlugs = []string{"upper-town", "trinity-suburb", "holy-spirit-cathedral"}
var centerNames = []string{"Верхний город", "Троицкое предместье", "Свято-Духов собор — внешний осмотр"}

type State struct {
	Collections      []Collection  `json:"collections"`
	DiscoveredPlaces int64         `json:"discovered_places"`
	Achievements     []Achievement `json:"achievements"`
}

func Definitions(count int64) []Achievement {
	result := []Achievement{{Code: "first_discovery", Name: "Первое открытие", Target: 1}, {Code: "five_discoveries", Name: "Городской исследователь", Target: 5}, {Code: "ten_discoveries", Name: "Знаток города", Target: 10}}
	for i := range result {
		result[i].Progress = count
		if count > result[i].Target {
			result[i].Progress = result[i].Target
		}
	}
	return result
}

type Postgres struct{ Pool *pgxpool.Pool }

func (p Postgres) Read(ctx context.Context, owner int64) (State, error) {
	var state State
	if owner < 1 {
		return state, errors.New("invalid exploration owner")
	}
	tx, err := p.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return state, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM xp_transactions WHERE user_id=$1", owner).Scan(&state.DiscoveredPlaces); err != nil {
		return state, err
	}
	state.Achievements = Definitions(state.DiscoveredPlaces)
	rows, err := tx.Query(ctx, "SELECT code,earned_at FROM user_achievements WHERE user_id=$1", owner)
	if err != nil {
		return state, err
	}
	for rows.Next() {
		var code string
		var at time.Time
		if err = rows.Scan(&code, &at); err != nil {
			rows.Close()
			return state, err
		}
		for i := range state.Achievements {
			if state.Achievements[i].Code == code {
				value := at
				state.Achievements[i].EarnedAt = &value
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, err
	}
	collection, err := readCollection(ctx, tx, owner, "minsk")
	if err != nil {
		return state, err
	}
	state.Collections = []Collection{collection}
	return state, tx.Commit(ctx)
}

func readCollection(ctx context.Context, tx pgx.Tx, owner int64, city string) (Collection, error) {
	collection := Collection{Code: "minsk-historic-center-v1", Name: "Исторический центр Минска", City: city, Total: len(centerSlugs), Places: make([]CollectionPlace, 0, len(centerSlugs))}
	rows, err := tx.Query(ctx, `SELECT expected.slug,p.id,p.name,COALESCE(p.is_active,false),x.created_at
 FROM unnest($2::text[]) WITH ORDINALITY AS expected(slug,ord)
 LEFT JOIN places p ON p.slug=expected.slug AND p.city_id=(SELECT id FROM cities WHERE slug=$3)
 LEFT JOIN xp_transactions x ON x.place_id=p.id AND x.user_id=$1
 ORDER BY expected.ord`, owner, centerSlugs, city)
	if err != nil {
		return collection, err
	}
	var latest *time.Time
	for rows.Next() {
		var item CollectionPlace
		var name *string
		var at *time.Time
		if err = rows.Scan(&item.Slug, &item.PlaceID, &name, &item.Available, &at); err != nil {
			rows.Close()
			return collection, err
		}
		item.Name = centerNames[len(collection.Places)]
		if name != nil {
			item.Name = *name
		}
		item.Visited = at != nil
		if at != nil {
			collection.Visited++
			if latest == nil || at.After(*latest) {
				latest = at
			}
		}
		collection.Places = append(collection.Places, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return collection, err
	}
	if collection.Visited == collection.Total {
		collection.CompletedAt = latest
	}

	return collection, nil
}
