package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresSessionsIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	email := fmt.Sprintf("auth-test-%d@example.com", time.Now().UnixNano())
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE email=$1", email)
	repository := Postgres{pool}
	s, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Register(ctx, email, "integration password 123")
	if err != nil {
		t.Fatal(err)
	}
	var storedPassword string
	var storedToken []byte
	err = pool.QueryRow(ctx, "SELECT u.password_hash,s.access_token_hash FROM users u JOIN sessions s ON s.user_id=u.id WHERE u.id=$1", result.User.ID).Scan(&storedPassword, &storedToken)
	if err != nil || storedPassword == "integration password 123" || !verifyPassword(storedPassword, "integration password 123") {
		t.Fatal("password persistence", err)
	}
	expected := sha256.Sum256([]byte(result.AccessToken))
	if string(storedToken) != string(expected[:]) {
		t.Fatal("stored token not hashed")
	}
	restarted, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	user, err := restarted.Authenticate(ctx, result.AccessToken)
	if err != nil || user.ID != result.User.ID {
		t.Fatal("restart authentication", err)
	}
	login, err := s.Login(ctx, email, "integration password 123")
	if err != nil {
		t.Fatal(err)
	}
	// Revoking one session must not revoke the other.
	if _, err = pool.Exec(ctx, "UPDATE sessions SET revoked_at=NOW() WHERE access_token_hash=$1", expected[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, result.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked accepted", err)
	}
	if _, err = s.Authenticate(ctx, login.AccessToken); err != nil {
		t.Fatal("other session revoked", err)
	}
	sum := sha256.Sum256([]byte(login.AccessToken))
	if _, err = pool.Exec(ctx, "UPDATE sessions SET created_at=NOW()-INTERVAL '2 hours',expires_at=NOW()-INTERVAL '1 hour' WHERE access_token_hash=$1", sum[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, login.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expired accepted", err)
	}
	// Concurrent duplicate registrations rely on the UNIQUE constraint.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Register(ctx, email, "different password 123"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, ErrEmailExists) {
			t.Fatal("duplicate register", err)
		}
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=$1", result.User.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("failed register created session", count, err)
	}
	// A session insertion failure must roll back the newly inserted user.
	rollbackEmail := "rollback-" + email
	_, err = repository.Register(ctx, rollbackEmail, storedPassword, storedToken, time.Now(), time.Now().Add(time.Minute))
	if err == nil {
		t.Fatal("duplicate token did not fail")
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE email=$1", rollbackEmail).Scan(&count); err != nil || count != 0 {
		t.Fatal("orphan user after rollback", count, err)
	}
	// A second account sees its own user, not the previously authenticated user.
	secondEmail := "second-" + email
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE email=$1", secondEmail)
	second, err := s.Register(ctx, secondEmail, "second password 123")
	if err != nil {
		t.Fatal(err)
	}
	secondUser, err := s.Authenticate(ctx, second.AccessToken)
	if err != nil || secondUser.ID == result.User.ID || secondUser.Email != secondEmail {
		t.Fatal("session owner isolation", err)
	}
	// Two simultaneous first registrations must produce exactly one account/session.
	raceEmail := "race-" + email
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE email=$1", raceEmail)
	concurrent := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Register(ctx, raceEmail, "concurrent password 123")
			concurrent <- err
		}()
	}
	wg.Wait()
	close(concurrent)
	successes, duplicates := 0, 0
	for err := range concurrent {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrEmailExists) {
			duplicates++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatal("concurrent registration", successes, duplicates)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM sessions s JOIN users u ON u.id=s.user_id WHERE u.email=$1", raceEmail).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate sessions", count, err)
	}

}
