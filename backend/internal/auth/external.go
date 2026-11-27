package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	ErrExternalEmailRequired = errors.New("a usable provider email is required for a new account")
	ErrExternalLinkRequired  = errors.New("sign in to the existing account to link this identity")
	ErrExternalLinkConflict  = errors.New("external identity or provider already linked")
)

// ExternalIdentity is supplied by a trusted provider adapter after verification.
// Subject is opaque and case-sensitive. It must never come from a client profile.
type ExternalIdentity struct {
	Provider string
	Subject  string
}

// Serialize first login and explicit linking even before a row exists.
// A hash collision only serializes unrelated operations; lookup uses the full key.
func (id ExternalIdentity) lock(ctx context.Context, tx pgx.Tx) error {
	if id.Provider != "google" && id.Provider != "yandex" {
		return ErrInvalidInput
	}
	if len(id.Subject) == 0 || len(id.Subject) > 255 {
		return ErrInvalidInput
	}
	for _, c := range id.Subject {
		if c < '!' || c > '~' {
			return ErrInvalidInput
		}
	}
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "oauth:"+id.Provider+":"+id.Subject)
	return err
}

// ResolveExternalLogin runs in the caller's READ COMMITTED transaction so that
// consuming the OAuth attempt and issuing its session can commit atomically.
// registrationEmail is the provider address approved for registration, or empty.
func ResolveExternalLogin(ctx context.Context, tx pgx.Tx, id ExternalIdentity, registrationEmail string) (User, error) {
	if err := id.lock(ctx, tx); err != nil {
		return User{}, err
	}
	var user User
	err := tx.QueryRow(ctx, `SELECT u.id, u.email, u.created_at
		FROM user_external_identities i JOIN users u ON u.id=i.user_id
		WHERE i.provider=$1 AND i.subject=$2 FOR KEY SHARE OF u`, id.Provider, id.Subject).
		Scan(&user.ID, &user.Email, &user.CreatedAt)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE user_external_identities SET last_login_at=clock_timestamp()
			WHERE provider=$1 AND subject=$2`, id.Provider, id.Subject)
		return user, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}
	email, err := normalizeEmail(registrationEmail)
	if err != nil {
		return User{}, ErrExternalEmailRequired
	}
	err = tx.QueryRow(ctx, `INSERT INTO users(email) VALUES ($1)
		ON CONFLICT (email) DO NOTHING RETURNING id, email, created_at`, email).
		Scan(&user.ID, &user.Email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrExternalLinkRequired
	}
	if err != nil {
		return User{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_external_identities(user_id, provider, subject, last_login_at)
		VALUES ($1,$2,$3,clock_timestamp())`, user.ID, id.Provider, id.Subject)
	return user, err
}

// LinkExternalIdentity expects an authenticated owner from the caller. Retrying
// the same link succeeds; a conflicting owner or a different subject never moves.
func LinkExternalIdentity(ctx context.Context, tx pgx.Tx, owner int64, id ExternalIdentity) error {
	if owner <= 0 {
		return ErrUnauthorized
	}
	if err := id.lock(ctx, tx); err != nil {
		return err
	}
	var exists bool
	err := tx.QueryRow(ctx, "SELECT true FROM users WHERE id=$1 FOR KEY SHARE", owner).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO user_external_identities(user_id, provider, subject)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, owner, id.Provider, id.Subject)
	if err != nil || tag.RowsAffected() == 1 {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_external_identities
		WHERE user_id=$1 AND provider=$2 AND subject=$3)`, owner, id.Provider, id.Subject).Scan(&exists)
	if err == nil && !exists {
		return ErrExternalLinkConflict
	}
	return err
}

// LoginExternal issues the same session as password login, inside the transaction
// that consumes the mobile handoff.
func (s *Service) LoginExternal(ctx context.Context, tx pgx.Tx, identity ExternalIdentity, email string) (LoginResult, error) {
	user, err := ResolveExternalLogin(ctx, tx, identity, email)
	if err != nil {
		return LoginResult{}, err
	}
	result, input, err := s.issue()
	if err != nil {
		return LoginResult{}, err
	}
	if err = createSession(ctx, tx, user.ID, input); err != nil {
		return LoginResult{}, err
	}
	if _, err = tx.Exec(ctx, "UPDATE sessions SET external_login=true WHERE access_token_hash=$1", input.AccessHash); err != nil {
		return LoginResult{}, err
	}
	result.User = user
	return result, nil
}

// ConfirmPassword proves ownership before adding a new login method.
func (s *Service) ConfirmPassword(ctx context.Context, owner int64, email, password string) error {
	if _, err := normalize(email, password); err != nil {
		return ErrCredentials
	}
	c, err := s.repository.FindCredentials(ctx, email)
	if err != nil || c.User.ID != owner || !verifyPassword(c.PasswordHash, password) {
		return ErrCredentials
	}
	return nil
}
