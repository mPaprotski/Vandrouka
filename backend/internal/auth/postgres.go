package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ Pool *pgxpool.Pool }

func createSession(ctx context.Context, tx pgx.Tx, id int64, input SessionInput) error {
	var family string
	if err := tx.QueryRow(ctx, "INSERT INTO session_families(user_id,created_at,expires_at) VALUES($1,$2,$3) RETURNING id::text", id, input.CreatedAt, input.RefreshExpiresAt).Scan(&family); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO sessions(user_id,access_token_hash,created_at,expires_at,family_id,refresh_token_hash) VALUES($1,$2,$3,$4,$5,$6)`, id, input.AccessHash, input.CreatedAt, input.AccessExpiresAt, family, input.RefreshHash)
	return err
}
func (p Postgres) Register(ctx context.Context, email, hash string, input SessionInput) (User, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	var user User
	err = tx.QueryRow(ctx, "INSERT INTO users(email,password_hash,created_at) VALUES($1,$2,$3) RETURNING id,email,created_at", email, hash, input.CreatedAt).Scan(&user.ID, &user.Email, &user.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrEmailExists
	}
	if err != nil {
		return User{}, err
	}
	if err = createSession(ctx, tx, user.ID, input); err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return user, nil
}
func (p Postgres) FindCredentials(ctx context.Context, email string) (Credentials, error) {
	var record Credentials
	err := p.Pool.QueryRow(ctx, "SELECT id,email,created_at,password_hash FROM users WHERE email=$1", email).Scan(&record.User.ID, &record.User.Email, &record.User.CreatedAt, &record.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrCredentials
	}
	return record, err
}
func (p Postgres) AddSession(ctx context.Context, id int64, input SessionInput) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = createSession(ctx, tx, id, input); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (p Postgres) SessionUser(ctx context.Context, token []byte, now time.Time) (User, error) {
	var user User
	err := p.Pool.QueryRow(ctx, `SELECT u.id,u.email,u.created_at FROM sessions s JOIN users u ON u.id=s.user_id
 LEFT JOIN session_families f ON f.id=s.family_id
 WHERE s.access_token_hash=$1 AND s.expires_at>$2 AND s.revoked_at IS NULL
 AND (s.family_id IS NULL OR (f.revoked_at IS NULL AND f.expires_at>$2))`, token, now).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	return user, err
}
func revokeFamily(ctx context.Context, tx pgx.Tx, family string, now time.Time) error {
	if _, err := tx.Exec(ctx, "UPDATE session_families SET revoked_at=COALESCE(revoked_at,$2) WHERE id=$1", family, now); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "UPDATE sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE family_id=$1", family, now)
	return err
}
func (p Postgres) Rotate(ctx context.Context, old []byte, input SessionInput) (SessionRecord, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return SessionRecord{}, err
	}
	defer tx.Rollback(ctx)
	var family string
	var owner int64
	var expiry time.Time
	var revoked *time.Time
	err = tx.QueryRow(ctx, `SELECT f.id::text,f.user_id,f.expires_at,f.revoked_at FROM session_families f
 JOIN sessions s ON s.family_id=f.id WHERE s.refresh_token_hash=$1 FOR UPDATE OF f`, old).Scan(&family, &owner, &expiry, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrUnauthorized
	}
	if err != nil {
		return SessionRecord{}, err
	}
	if revoked != nil || !input.CreatedAt.Before(expiry) {
		return SessionRecord{}, ErrUnauthorized
	}
	var id int64
	var used, sessionRevoked *time.Time
	if err = tx.QueryRow(ctx, "SELECT id,refresh_used_at,revoked_at FROM sessions WHERE refresh_token_hash=$1", old).Scan(&id, &used, &sessionRevoked); err != nil {
		return SessionRecord{}, err
	}
	if used != nil {
		// A consumed refresh is a credential replay: revoke the whole family atomically.
		if err = revokeFamily(ctx, tx, family, input.CreatedAt); err != nil {
			return SessionRecord{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return SessionRecord{}, err
		}
		return SessionRecord{}, ErrUnauthorized
	}
	if sessionRevoked != nil {
		return SessionRecord{}, ErrUnauthorized
	}
	if input.AccessExpiresAt.After(expiry) {
		input.AccessExpiresAt = expiry
	}
	if _, err = tx.Exec(ctx, "UPDATE sessions SET refresh_used_at=$2,revoked_at=$2 WHERE id=$1", id, input.CreatedAt); err != nil {
		return SessionRecord{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sessions(user_id,access_token_hash,created_at,expires_at,family_id,refresh_token_hash) VALUES($1,$2,$3,$4,$5,$6)`, owner, input.AccessHash, input.CreatedAt, input.AccessExpiresAt, family, input.RefreshHash); err != nil {
		return SessionRecord{}, err
	}
	var user User
	if err = tx.QueryRow(ctx, "SELECT id,email,created_at FROM users WHERE id=$1", owner).Scan(&user.ID, &user.Email, &user.CreatedAt); err != nil {
		return SessionRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SessionRecord{}, err
	}
	return SessionRecord{User: user, AccessExpiresAt: input.AccessExpiresAt, RefreshExpiresAt: expiry}, nil
}
func (p Postgres) Revoke(ctx context.Context, refresh []byte, now time.Time) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var family string
	err = tx.QueryRow(ctx, `SELECT f.id::text FROM session_families f JOIN sessions s ON s.family_id=f.id WHERE s.refresh_token_hash=$1 FOR UPDATE OF f`, refresh).Scan(&family)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = revokeFamily(ctx, tx, family, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
