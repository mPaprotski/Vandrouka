package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExternalIdentityPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := Postgres{Pool: pool}
	service, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	tag := uuid.NewString()
	passwordEmail := tag + "-password@example.invalid"
	socialEmail := tag + "-social@example.invalid"
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE email=ANY($1)", []string{passwordEmail, socialEmail})
	google := ExternalIdentity{"google", tag}
	yandex := ExternalIdentity{"yandex", tag}
	resolve := func(id ExternalIdentity, email string) (User, error) {
		var user User
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			var err error
			user, err = ResolveExternalLogin(ctx, tx, id, email)
			return err
		})
		return user, err
	}
	link := func(owner int64, id ExternalIdentity) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return LinkExternalIdentity(ctx, tx, owner, id)
		})
	}
	const password = "a long test password"
	existing, err := service.Register(ctx, passwordEmail, password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(google, strings.ToUpper(passwordEmail)); !errors.Is(err, ErrExternalLinkRequired) {
		t.Fatalf("matching email: %v", err)
	}
	if _, err := resolve(google, ""); !errors.Is(err, ErrExternalEmailRequired) {
		t.Fatalf("registration without email: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := link(existing.User.ID, google); err != nil {
			t.Fatalf("explicit link: %v", err)
		}
	}
	for _, email := range []string{"", socialEmail} {
		user, err := resolve(google, email)
		if err != nil || user != existing.User {
			t.Fatalf("linked login: got %+v, %v", user, err)
		}
	}
	if result, err := service.Login(ctx, passwordEmail, password); err != nil || result.User.ID != existing.User.ID {
		t.Fatalf("password login after linking: %v", err)
	}

	// Both providers can return the same opaque subject for unrelated people.
	type outcome struct {
		user User
		err  error
	}
	start := make(chan struct{})
	results := make(chan outcome, 6)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			u, err := resolve(yandex, " "+strings.ToUpper(socialEmail)+" ")
			results <- outcome{u, err}
		}()
	}
	close(start)
	var social User
	for i := 0; i < cap(results); i++ {
		r := <-results
		if r.err != nil || r.user.Email != socialEmail || r.user.ID == existing.User.ID || (i > 0 && r.user.ID != social.ID) {
			t.Errorf("parallel registration: got %+v, %v", r.user, r.err)
		}
		social = r.user
	}
	if t.Failed() {
		return
	}
	if _, err := service.Login(ctx, socialEmail, password); !errors.Is(err, ErrCredentials) {
		t.Fatalf("password login on passwordless user: %v", err)
	}
	if err := repo.DeleteAccount(ctx, social.ID, password); !errors.Is(err, ErrCredentials) {
		t.Fatalf("password deletion on passwordless user: %v", err)
	}
	if err := link(existing.User.ID, yandex); !errors.Is(err, ErrExternalLinkConflict) {
		t.Fatalf("identity already belongs to someone else: %v", err)
	}
	otherGoogle := ExternalIdentity{"google", tag + "-other"}
	if err := link(existing.User.ID, otherGoogle); !errors.Is(err, ErrExternalLinkConflict) {
		t.Fatalf("provider slot already occupied: %v", err)
	}
	if err := link(social.ID, otherGoogle); err != nil {
		t.Fatal(err)
	}
	u, err := resolve(otherGoogle, passwordEmail)
	if err != nil || u.ID != social.ID || u.Email != socialEmail {
		t.Fatalf("second login method: %+v, %v", u, err)
	}

	// Stage 2 must be able to roll account creation back with a failed attempt.
	aborted := errors.New("abort transaction")
	rollbackEmail := tag + "-rollback@example.invalid"
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := ResolveExternalLogin(ctx, tx, ExternalIdentity{"google", tag + "-rollback"}, rollbackEmail); err != nil {
			return err
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE email=$1)", rollbackEmail).Scan(&exists); err != nil || exists {
		t.Fatal("registration survived rollback", err)
	}
	if err := repo.DeleteAccount(ctx, existing.User.ID, password); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(google, ""); !errors.Is(err, ErrExternalEmailRequired) {
		t.Fatalf("identity survived deletion: %v", err)
	}
}
