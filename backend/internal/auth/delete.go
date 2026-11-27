package auth

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"unicode/utf8"
)

// DeleteAccount requires the current password and removes all families by FK cascade.
// Media deletion jobs survive the account; delay them beyond any in-flight upload lease.
func (p Postgres) DeleteAccount(ctx context.Context, owner int64, password string) error {
	if owner < 1 || !utf8.ValidString(password) || len(password) > 512 || utf8.RuneCountInString(password) < 12 {
		return ErrInvalidInput
	}
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var hash *string
	err = tx.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1 FOR UPDATE", owner).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if hash == nil || !verifyPassword(*hash, password) {
		return ErrCredentials
	}
	if err = DeleteConfirmedAccount(ctx, tx, owner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteConfirmedAccount requires a freshly verified credential in the caller transaction.
func DeleteConfirmedAccount(ctx context.Context, tx pgx.Tx, owner int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO media_deletions(object_key,not_before)
 SELECT object_key,GREATEST(NOW(),upload_expires_at) FROM visit_photos WHERE user_id=$1
 ON CONFLICT(object_key) DO UPDATE SET not_before=GREATEST(media_deletions.not_before,EXCLUDED.not_before)`, owner)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM users WHERE id=$1", owner); err != nil {
		return err
	}
	return nil
}
