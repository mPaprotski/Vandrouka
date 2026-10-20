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

func TestRefreshPostgresIntegration(t *testing.T) {
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
	email := fmt.Sprintf("refresh-test-%d@example.com", time.Now().UnixNano())
	defer pool.Exec(context.Background(), "DELETE FROM users WHERE email=$1", email)
	service, err := New(Postgres{pool})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC()
	service.now = func() time.Time { return clock }
	original, err := service.Register(ctx, email, "refresh test password 123")
	if err != nil {
		t.Fatal(err)
	}
	refreshHash := sha256.Sum256([]byte(original.RefreshToken))
	var stored []byte
	if err = pool.QueryRow(ctx, "SELECT refresh_token_hash FROM sessions WHERE user_id=$1", original.User.ID).Scan(&stored); err != nil || string(stored) != string(refreshHash[:]) {
		t.Fatal("refresh hash not persisted", err)
	}
	clock = clock.Add(AccessLifetime)
	if _, err = service.Authenticate(ctx, original.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("access expiry", err)
	}
	renewed, err := service.Refresh(ctx, original.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed.RefreshExpiresAt.Equal(original.RefreshExpiresAt) || renewed.RefreshToken == original.RefreshToken {
		t.Fatal("absolute deadline/rotation")
	}
	if _, err = service.Authenticate(ctx, original.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old access accepted")
	}
	if _, err = service.Authenticate(ctx, renewed.AccessToken); err != nil {
		t.Fatal(err)
	}
	// A separate device must survive logout/replay of the original family.
	device, err := service.Login(ctx, email, "refresh test password 123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Refresh(ctx, original.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("replay accepted")
	}
	if _, err = service.Authenticate(ctx, renewed.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("replayed family active")
	}
	if _, err = service.Refresh(ctx, renewed.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("replayed chain active")
	}
	if _, err = service.Authenticate(ctx, device.AccessToken); err != nil {
		t.Fatal("other device revoked")
	}
	if err = service.Logout(ctx, device.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err = service.Logout(ctx, device.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(ctx, device.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout access active")
	}
	if _, err = service.Refresh(ctx, device.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout refresh active")
	}
	// Two refreshes serialize; replay revokes even the token returned to the winner.
	race, err := service.Login(ctx, email, "refresh test password 123")
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		session LoginResult
		err     error
	}
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, err := service.Refresh(ctx, race.RefreshToken)
			outcomes <- outcome{session, err}
		}()
	}
	wg.Wait()
	close(outcomes)
	success, rejected := 0, 0
	var winner LoginResult
	for result := range outcomes {
		if result.err == nil {
			success++
			winner = result.session
		} else if errors.Is(result.err, ErrUnauthorized) {
			rejected++
		} else {
			t.Fatal(result.err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatal("concurrent refresh", success, rejected)
	}
	if _, err = service.Authenticate(ctx, winner.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("concurrent replay not revoked")
	}
	expiry, err := service.Login(ctx, email, "refresh test password 123")
	if err != nil {
		t.Fatal(err)
	}
	clock = expiry.RefreshExpiresAt.Add(-time.Minute)
	last, err := service.Refresh(ctx, expiry.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if !last.ExpiresAt.Equal(expiry.RefreshExpiresAt) {
		t.Fatal("access outlives family")
	}
	clock = expiry.RefreshExpiresAt
	if _, err = service.Refresh(ctx, last.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expiry boundary")
	}
	if _, err = service.Authenticate(ctx, last.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("access at family expiry")
	}
}
