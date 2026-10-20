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
const RefreshLifetime = 30 * 24 * time.Hour

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}
type LoginResult struct {
	User             User      `json:"user"`
	AccessToken      string    `json:"access_token"`
	TokenType        string    `json:"token_type"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}
type Credentials struct {
	User         User
	PasswordHash string
}
type SessionInput struct {
	AccessHash, RefreshHash                      []byte
	CreatedAt, AccessExpiresAt, RefreshExpiresAt time.Time
}
type SessionRecord struct {
	User                              User
	AccessExpiresAt, RefreshExpiresAt time.Time
}
type Repository interface {
	Register(context.Context, string, string, SessionInput) (User, error)
	FindCredentials(context.Context, string) (Credentials, error)
	AddSession(context.Context, int64, SessionInput) error
	SessionUser(context.Context, []byte, time.Time) (User, error)
	Rotate(context.Context, []byte, SessionInput) (SessionRecord, error)
	Revoke(context.Context, []byte, time.Time) error
}
type Service struct {
	repository      Repository
	dummyHash       string
	lifetime        time.Duration
	refreshLifetime time.Duration
	now             func() time.Time
}

func New(repository Repository) (*Service, error) {
	return NewWithLifetime(repository, AccessLifetime)
}

func NewWithLifetime(repository Repository, lifetime time.Duration) (*Service, error) {
	return NewWithLifetimes(repository, lifetime, RefreshLifetime)
}

func NewWithLifetimes(repository Repository, lifetime, refreshLifetime time.Duration) (*Service, error) {
	if lifetime <= 0 || refreshLifetime <= 0 {
		return nil, errors.New("session lifetime must be positive")
	}

	dummy, err := hashPassword("dummy-password-not-a-user-credential")
	if err != nil {
		return nil, err
	}
	return &Service{repository: repository, dummyHash: dummy, now: time.Now, lifetime: lifetime, refreshLifetime: refreshLifetime}, nil
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
	result, input, err := s.issue()
	if err != nil {
		return LoginResult{}, err
	}
	user, err := s.repository.Register(ctx, email, hash, input)
	if err != nil {
		return LoginResult{}, err
	}
	result.User = user
	return result, nil
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
	result, input, err := s.issue()
	if err != nil {
		return LoginResult{}, err
	}
	if err = s.repository.AddSession(ctx, record.User.ID, input); err != nil {
		return LoginResult{}, err
	}
	result.User = record.User
	return result, nil
}
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return User{}, ErrUnauthorized
	}
	sum := sha256.Sum256([]byte(token))
	return s.repository.SessionUser(ctx, sum[:], s.now().UTC())
}

func (s *Service) issue() (LoginResult, SessionInput, error) {
	access, err := newAccessToken()
	if err != nil {
		return LoginResult{}, SessionInput{}, err
	}
	refresh, err := newAccessToken()
	if err != nil {
		return LoginResult{}, SessionInput{}, err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	refreshExpires := now.Add(s.refreshLifetime)
	accessExpires := now.Add(s.lifetime)
	if accessExpires.After(refreshExpires) {
		accessExpires = refreshExpires
	}
	accessHash := sha256.Sum256([]byte(access))
	refreshHash := sha256.Sum256([]byte(refresh))
	return LoginResult{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresAt: accessExpires, RefreshExpiresAt: refreshExpires}, SessionInput{AccessHash: accessHash[:], RefreshHash: refreshHash[:], CreatedAt: now, AccessExpiresAt: accessExpires, RefreshExpiresAt: refreshExpires}, nil
}
func tokenHash(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	return sum[:], true
}
func (s *Service) Refresh(ctx context.Context, token string) (LoginResult, error) {
	hash, ok := tokenHash(token)
	if !ok {
		return LoginResult{}, ErrUnauthorized
	}
	result, input, err := s.issue()
	if err != nil {
		return LoginResult{}, err
	}
	record, err := s.repository.Rotate(ctx, hash, input)
	if err != nil {
		return LoginResult{}, err
	}
	result.User = record.User
	result.ExpiresAt = record.AccessExpiresAt
	result.RefreshExpiresAt = record.RefreshExpiresAt
	return result, nil
}
func (s *Service) Logout(ctx context.Context, token string) error {
	hash, ok := tokenHash(token)
	if !ok {
		return ErrInvalidInput
	}
	return s.repository.Revoke(ctx, hash, s.now().UTC())
}
