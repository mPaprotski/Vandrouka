package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidInput = errors.New("invalid credentials input")
	ErrCredentials  = errors.New("invalid credentials")
	ErrEmailExists  = errors.New("email already registered")
	ErrUnauthorized = errors.New("session unavailable")
)

const AccessLifetime = 24 * time.Hour

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}
type LoginResult struct {
	User        User      `json:"user"`
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}
type Credentials struct {
	User         User
	PasswordHash string
}
type Repository interface {
	Register(context.Context, string, string, []byte, time.Time, time.Time) (User, error)
	FindCredentials(context.Context, string) (Credentials, error)
	AddSession(context.Context, int64, []byte, time.Time, time.Time) error
	SessionUser(context.Context, []byte, time.Time) (User, error)
}
type Service struct {
	repository Repository
	dummyHash  string
	lifetime   time.Duration
	now        func() time.Time
}

func New(repository Repository) (*Service, error) {
	return NewWithLifetime(repository, AccessLifetime)
}

func NewWithLifetime(repository Repository, lifetime time.Duration) (*Service, error) {
	if lifetime <= 0 {
		return nil, errors.New("session lifetime must be positive")
	}

	dummy, err := hashPassword("dummy-password-not-a-user-credential")
	if err != nil {
		return nil, err
	}
	return &Service{repository: repository, dummyHash: dummy, now: time.Now, lifetime: lifetime}, nil
}

// Email matching is case-insensitive. No provider-specific dot/plus rewriting.
func normalize(email, password string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 254 || len(email) == 0 || !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || utf8.RuneCountInString(password) > 128 || len(password) > 512 {
		return "", ErrInvalidInput
	}
	for _, c := range email {
		if c > 127 || c <= 32 {
			return "", ErrInvalidInput
		}
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" || !strings.Contains(email, "@") {
		return "", ErrInvalidInput
	}
	return email, nil
}
func (s *Service) Register(ctx context.Context, email, password string) (LoginResult, error) {
	email, err := normalize(email, password)
	if err != nil {
		return LoginResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return LoginResult{}, err
	}
	token, err := newAccessToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now().UTC()
	expires := now.Add(s.lifetime)
	sum := sha256.Sum256([]byte(token))
	user, err := s.repository.Register(ctx, email, hash, sum[:], now, expires)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: user, AccessToken: token, TokenType: "Bearer", ExpiresAt: expires}, nil
}
func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	email, err := normalize(email, password)
	if err != nil {
		return LoginResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	record, err := s.repository.FindCredentials(ctx, email)
	if err != nil && !errors.Is(err, ErrCredentials) {
		return LoginResult{}, err
	}
	hash := record.PasswordHash
	if errors.Is(err, ErrCredentials) {
		hash = s.dummyHash
	}
	valid := verifyPassword(hash, password)
	if !valid || err != nil {
		return LoginResult{}, ErrCredentials
	}
	token, err := newAccessToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now().UTC()
	expires := now.Add(s.lifetime)
	sum := sha256.Sum256([]byte(token))
	if err = s.repository.AddSession(ctx, record.User.ID, sum[:], now, expires); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: record.User, AccessToken: token, TokenType: "Bearer", ExpiresAt: expires}, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return User{}, ErrUnauthorized
	}
	sum := sha256.Sum256([]byte(token))
	return s.repository.SessionUser(ctx, sum[:], s.now().UTC())
}
