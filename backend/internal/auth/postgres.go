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

func (p Postgres) Register(ctx context.Context, email, hash string, token []byte, now, expires time.Time) (User, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	var user User
	err = tx.QueryRow(ctx, "INSERT INTO users(email,password_hash,created_at) VALUES($1,$2,$3) RETURNING id,email,created_at", email, hash, now).Scan(&user.ID, &user.Email, &user.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrEmailExists
	}
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO sessions(user_id,access_token_hash,created_at,expires_at) VALUES($1,$2,$3,$4)", user.ID, token, now, expires); err != nil {
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
func (p Postgres) AddSession(ctx context.Context, id int64, token []byte, now, expires time.Time) error {
	_, err := p.Pool.Exec(ctx, "INSERT INTO sessions(user_id,access_token_hash,created_at,expires_at) VALUES($1,$2,$3,$4)", id, token, now, expires)
	return err
}
func (p Postgres) SessionUser(ctx context.Context, token []byte, now time.Time) (User, error) {
	var user User
	err := p.Pool.QueryRow(ctx, `SELECT u.id,u.email,u.created_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.access_token_hash=$1 AND s.expires_at>$2 AND s.revoked_at IS NULL`, token, now).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	return user, err
}
