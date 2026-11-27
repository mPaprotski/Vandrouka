package oauthlogin

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/auth"
)

func TestAccountActionsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	accounts, err := auth.New(auth.Postgres{Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New("https://example.invalid", pool, accounts)
	if err != nil {
		t.Fatal(err)
	}
	service.providers["google"] = provider{}
	email := uuid.NewString() + "@example.invalid"
	password := "account action password"
	login, err := accounts.Register(ctx, email, password)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM users WHERE id=$1", login.User.ID)
	secret := hex.EncodeToString(make([]byte, 32))
	challenge := hex.EncodeToString(digest(secret))
	if _, err = service.StartProtected(ctx, "google", challenge, "link", login.AccessToken, "wrong password"); !errors.Is(err, auth.ErrCredentials) {
		t.Fatal("unconfirmed link", err)
	}
	prepare := func(purpose, subject string) (Attempt, string) {
		a, err := service.StartProtected(ctx, "google", challenge, purpose, login.AccessToken, password)
		if err != nil {
			t.Fatal(err)
		}
		code := token()
		_, err = pool.Exec(ctx, "UPDATE oauth_login_attempts SET phase='ready',return_hash=$2,outcome='verified',subject=$3,email='' WHERE id=$1", a.ID, digest(code), subject)
		if err != nil {
			t.Fatal(err)
		}
		return a, code
	}
	subject := uuid.NewString()
	a, code := prepare("link", subject)
	if err = service.CompleteProtected(ctx, a.ID, code, secret, "delete", login.AccessToken); !errors.Is(err, ErrAttempt) {
		t.Fatal("purpose confusion", err)
	}
	if err = service.CompleteProtected(ctx, a.ID, code, secret, "link", login.AccessToken); err != nil {
		t.Fatal(err)
	}
	m, err := service.Methods(ctx, login.AccessToken)
	if err != nil || len(m.Providers) != 1 || !m.Password {
		t.Fatal(m, err)
	}
	if err = service.CompleteProtected(ctx, a.ID, code, secret, "link", login.AccessToken); !errors.Is(err, ErrAttempt) {
		t.Fatal("replay", err)
	}
	a, code = prepare("delete", "different-subject")
	if err = service.CompleteProtected(ctx, a.ID, code, secret, "delete", login.AccessToken); !errors.Is(err, auth.ErrCredentials) {
		t.Fatal("wrong provider identity", err)
	}
	a, code = prepare("delete", subject)
	if err = service.CompleteProtected(ctx, a.ID, code, secret, "delete", login.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err = accounts.Authenticate(ctx, login.AccessToken); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal("deleted session", err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM user_external_identities WHERE user_id=$1", login.User.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("identity cascade", err, count)
	}

}
