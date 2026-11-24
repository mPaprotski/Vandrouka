package quests

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/places"
)

type PlaceReader interface {
	GetPlace(context.Context, int64) (places.Place, error)
}

type Postgres struct {
	Pool    *pgxpool.Pool
	Catalog func(pgx.Tx) PlaceReader
}

type scanner interface {
	Scan(...any) error
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func inTx[T any](ctx context.Context, pool *pgxpool.Pool, work func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	result, err := work(tx)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return result, nil
}

// These lists use the UUID as an immutable cursor and always query within
// the current actor's scope.
const PageSize = 30

func readPage[T any](rows pgx.Rows, err error, scan func(scanner) (T, error), id func(T) string) (Page[T], error) {
	if err != nil {
		return Page[T]{}, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) { return scan(row) })
	if err != nil {
		return Page[T]{}, err
	}
	if items == nil {
		items = []T{}
	}
	page := Page[T]{Items: items}
	if len(items) > PageSize {
		page.Items = items[:PageSize]
		page.NextCursor = id(items[PageSize-1])
	}
	return page, nil
}
