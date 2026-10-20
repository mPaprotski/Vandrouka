package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"
)

type memoryFamily struct {
	user    User
	expires time.Time
	revoked bool
}
type memoryRefresh struct {
	access, family string
	used           bool
}
type memoryRepository struct {
	users  map[string]Credentials
	tokens map[string]struct {
		user    User
		expires time.Time
	}
	refresh      map[string]*memoryRefresh
	families     map[string]*memoryFamily
	accessFamily map[string]string
	failSession  bool
}

func newMemory() *memoryRepository {
	return &memoryRepository{users: map[string]Credentials{}, tokens: map[string]struct {
		user    User
		expires time.Time
	}{}, refresh: map[string]*memoryRefresh{}, families: map[string]*memoryFamily{}, accessFamily: map[string]string{}}
}
func (m *memoryRepository) Register(ctx context.Context, email, hash string, input SessionInput) (User, error) {
	if _, ok := m.users[email]; ok {
		return User{}, ErrEmailExists
	}
	user := User{ID: int64(len(m.users) + 1), Email: email, CreatedAt: input.CreatedAt}
	if m.failSession {
		return User{}, errors.New("session storage failure")
	}
	m.users[email] = Credentials{User: user, PasswordHash: hash}
	m.AddSession(ctx, user.ID, input)
	return user, nil
}
func (m *memoryRepository) FindCredentials(_ context.Context, email string) (Credentials, error) {
	c, ok := m.users[email]
	if !ok {
		return c, ErrCredentials
	}
	return c, nil
}
func (m *memoryRepository) AddSession(_ context.Context, id int64, input SessionInput) error {
	if m.failSession {
		return errors.New("session storage failure")
	}
	for _, record := range m.users {
		if record.User.ID == id {
			family := string(input.RefreshHash)
			m.families[family] = &memoryFamily{user: record.User, expires: input.RefreshExpiresAt}
			m.save(record.User, input, family)
			return nil
		}
	}
	return errors.New("unknown user")
}
func (m *memoryRepository) save(user User, input SessionInput, family string) {
	access, refresh := string(input.AccessHash), string(input.RefreshHash)
	m.tokens[access] = struct {
		user    User
		expires time.Time
	}{user, input.AccessExpiresAt}
	m.accessFamily[access] = family
	m.refresh[refresh] = &memoryRefresh{access: access, family: family}
}
func (m *memoryRepository) SessionUser(_ context.Context, token []byte, now time.Time) (User, error) {
	session, ok := m.tokens[string(token)]
	if !ok || !now.Before(session.expires) {
		return User{}, ErrUnauthorized
	}
	family := m.families[m.accessFamily[string(token)]]
	if family.revoked || !now.Before(family.expires) {
		return User{}, ErrUnauthorized
	}
	return session.user, nil
}
func (m *memoryRepository) Rotate(_ context.Context, token []byte, input SessionInput) (SessionRecord, error) {
	ref, ok := m.refresh[string(token)]
	if !ok {
		return SessionRecord{}, ErrUnauthorized
	}
	family := m.families[ref.family]
	if family.revoked || !input.CreatedAt.Before(family.expires) {
		return SessionRecord{}, ErrUnauthorized
	}
	if ref.used {
		family.revoked = true
		return SessionRecord{}, ErrUnauthorized
	}
	ref.used = true
	delete(m.tokens, ref.access)
	if input.AccessExpiresAt.After(family.expires) {
		input.AccessExpiresAt = family.expires
	}
	m.save(family.user, input, ref.family)
	return SessionRecord{User: family.user, AccessExpiresAt: input.AccessExpiresAt, RefreshExpiresAt: family.expires}, nil
}
func (m *memoryRepository) Revoke(_ context.Context, token []byte, _ time.Time) error {
	if ref, ok := m.refresh[string(token)]; ok {
		m.families[ref.family].revoked = true
	}
	return nil
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

func TestRefreshAndRevocation(t *testing.T) {
	s, err := New(newMemory())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	original, err := s.Register(context.Background(), "rotate@example.com", "refresh password 123")
	if err != nil {
		t.Fatal(err)
	}
	if !original.RefreshExpiresAt.Equal(now.Add(30 * 24 * time.Hour)) {
		t.Fatal("refresh lifetime")
	}
	now = now.Add(24 * time.Hour)
	if _, err = s.Authenticate(context.Background(), original.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("access did not expire")
	}
	renewed, err := s.Refresh(context.Background(), original.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.AccessToken == original.AccessToken || renewed.RefreshToken == original.RefreshToken || !renewed.RefreshExpiresAt.Equal(original.RefreshExpiresAt) {
		t.Fatal("rotation contract")
	}
	if _, err = s.Authenticate(context.Background(), renewed.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Refresh(context.Background(), original.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("refresh replay accepted")
	}
	if _, err = s.Authenticate(context.Background(), renewed.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("replay did not revoke family")
	}
	device, err := s.Login(context.Background(), "rotate@example.com", "refresh password 123")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(context.Background(), renewed.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(context.Background(), device.AccessToken); err != nil {
		t.Fatal("other device revoked")
	}
	if err = s.Logout(context.Background(), device.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(context.Background(), device.RefreshToken); err != nil {
		t.Fatal("logout not idempotent")
	}
	if _, err = s.Refresh(context.Background(), device.RefreshToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("logout refresh accepted")
	}
}
