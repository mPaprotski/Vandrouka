package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"
)

type memoryRepository struct {
	users  map[string]Credentials
	tokens map[string]struct {
		user    User
		expires time.Time
	}
	failSession bool
}

func newMemory() *memoryRepository {
	return &memoryRepository{users: map[string]Credentials{}, tokens: map[string]struct {
		user    User
		expires time.Time
	}{}}
}
func (m *memoryRepository) Register(ctx context.Context, email, hash string, token []byte, now, expires time.Time) (User, error) {
	if _, ok := m.users[email]; ok {
		return User{}, ErrEmailExists
	}
	user := User{ID: int64(len(m.users) + 1), Email: email, CreatedAt: now}
	if m.failSession {
		return User{}, errors.New("session storage failure")
	}
	m.users[email] = Credentials{User: user, PasswordHash: hash}
	m.AddSession(ctx, user.ID, token, now, expires)
	return user, nil
}
func (m *memoryRepository) FindCredentials(_ context.Context, email string) (Credentials, error) {
	c, ok := m.users[email]
	if !ok {
		return c, ErrCredentials
	}
	return c, nil
}
func (m *memoryRepository) AddSession(_ context.Context, id int64, token []byte, now, expires time.Time) error {
	if m.failSession {
		return errors.New("session storage failure")
	}
	for _, c := range m.users {
		if c.User.ID == id {
			m.tokens[string(token)] = struct {
				user    User
				expires time.Time
			}{c.User, expires}
			return nil
		}
	}
	return errors.New("unknown user")
}
func (m *memoryRepository) SessionUser(_ context.Context, token []byte, now time.Time) (User, error) {
	s, ok := m.tokens[string(token)]
	if !ok || !now.Before(s.expires) {
		return User{}, ErrUnauthorized
	}
	return s.user, nil
}
func TestPasswords(t *testing.T) {
	first, err := hashPassword("пароль с пробелами 123")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword("пароль с пробелами 123")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !verifyPassword(first, "пароль с пробелами 123") || verifyPassword(first, "пароль с пробелами 124") {
		t.Fatal("salt/password verification")
	}
	for _, bad := range []string{"", "plain password", "$argon2id$v=19$m=999999999,t=100,p=99$bad$bad", passwordPrefix + "bad$bad"} {
		if verifyPassword(bad, "password") {
			t.Fatal("malformed hash accepted")
		}
	}
}
func TestNormalize(t *testing.T) {
	email, err := normalize(" User@Example.com ", " a long password ")
	if err != nil || email != "user@example.com" {
		t.Fatal(email, err)
	}
	for _, pair := range [][2]string{{"missing-at", "long password 123"}, {"User <a@example.com>", "long password 123"}, {"тест@example.com", "long password 123"}, {"a@example.com", "short"}, {"a@example.com", string([]byte{0xff})}} {
		if _, err := normalize(pair[0], pair[1]); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid input accepted", pair[0])
		}
	}
}
func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := newMemory()
	s, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	registration, err := s.Register(ctx, " User@example.com ", "a strong password 123")
	if err != nil {
		t.Fatal(err)
	}
	if registration.User.Email != "user@example.com" || registration.TokenType != "Bearer" || !registration.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatal("response contract")
	}
	hash := sha256.Sum256([]byte(registration.AccessToken))
	if _, ok := repo.tokens[string(hash[:])]; !ok {
		t.Fatal("raw token was stored")
	}
	if _, err = s.Register(ctx, "USER@example.com", "different password 123"); !errors.Is(err, ErrEmailExists) {
		t.Fatal(err)
	}
	for _, email := range []string{"user@example.com", "absent@example.com"} {
		if _, err = s.Login(ctx, email, "incorrect password 123"); !errors.Is(err, ErrCredentials) {
			t.Fatal("credentials discrepancy", err)
		}
	}
	login, err := s.Login(ctx, "USER@example.com", "a strong password 123")
	if err != nil {
		t.Fatal(err)
	}
	if login.AccessToken == registration.AccessToken {
		t.Fatal("reused token")
	}
	user, err := s.Authenticate(ctx, login.AccessToken)
	if err != nil || !reflect.DeepEqual(user, registration.User) {
		t.Fatal(user, err)
	}
	for _, token := range []string{"", login.AccessToken + "=", "bad", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		if _, err = s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("bad token accepted", err)
		}
	}
	now = registration.ExpiresAt
	if _, err = s.Authenticate(ctx, registration.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expiry boundary", err)
	}
	if len(repo.users) != 1 || len(repo.tokens) != 2 {
		t.Fatal("failed logins created sessions")
	}
}
func TestStorageFailureAndCancellation(t *testing.T) {
	repo := newMemory()
	repo.failSession = true
	s, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Register(context.Background(), "test@example.com", "long password 123"); err == nil {
		t.Fatal("failed storage reported success")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Login(ctx, "test@example.com", "long password 123"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConfiguredLifetime(t *testing.T) {
	for _, lifetime := range []time.Duration{0, -time.Second} {
		if _, err := NewWithLifetime(newMemory(), lifetime); err == nil {
			t.Fatal("invalid lifetime accepted")
		}
	}
	s, err := NewWithLifetime(newMemory(), 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	registered, err := s.Register(context.Background(), "custom@example.com", "custom password 123")
	if err != nil {
		t.Fatal(err)
	}
	loggedIn, err := s.Login(context.Background(), "custom@example.com", "custom password 123")
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []LoginResult{registered, loggedIn} {
		if !session.ExpiresAt.Equal(now.Add(72 * time.Hour)) {
			t.Fatal("configured expiry not used")
		}
	}
	now = now.Add(72*time.Hour - time.Nanosecond)
	if _, err = s.Authenticate(context.Background(), registered.AccessToken); err != nil {
		t.Fatal("expired early", err)
	}
	now = now.Add(time.Nanosecond)
	if _, err = s.Authenticate(context.Background(), registered.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("expiry boundary", err)
	}
}
