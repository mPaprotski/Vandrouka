package history

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("visit not found")

// Hide also tombstones the photo; ledger and idempotency record are retained.
func (p Postgres) Hide(ctx context.Context, owner, id int64) error {
	if owner < 1 || id < 1 {
		return ErrInvalid
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var user int64
	if err = tx.QueryRow(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", owner).Scan(&user); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var photo string
	err = tx.QueryRow(ctx, "SELECT photo_id::text FROM visits WHERE id=$1 AND user_id=$2 AND status='confirmed' FOR UPDATE", id, owner).Scan(&photo)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE visit_photos SET deleted_at=COALESCE(deleted_at,NOW()) WHERE id=$1 AND user_id=$2", photo, owner); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE visits SET deleted_at=COALESCE(deleted_at,NOW()) WHERE id=$1", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
