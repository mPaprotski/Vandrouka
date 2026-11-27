package oauthlogin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/auth"
)

var (
	ErrInvalid   = errors.New("invalid oauth input")
	ErrAttempt   = errors.New("invalid, expired or used attempt")
	ErrDisabled  = errors.New("login provider is unavailable")
	ErrCancelled = errors.New("login cancelled")
)

type Service struct {
	pool      *pgxpool.Pool
	auth      *auth.Service
	providers map[string]provider
	origin    string
}

func New(origin string, pool *pgxpool.Pool, accounts *auth.Service) (*Service, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("OAUTH_LOGIN_ORIGIN must be a public HTTPS origin")
	}
	return &Service{pool: pool, auth: accounts, providers: configuredProviders(), origin: strings.TrimSuffix(origin, "/")}, nil
}

const prefix = "/v1/auth/oauth"

func (s *Service) callback(provider string) string {
	return s.origin + prefix + "/callback/" + provider
}
func token() string {
	var b [32]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func digest(value string) []byte { h := sha256.Sum256([]byte(value)); return h[:] }
func validToken(s string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil && len(s) == 43 && len(b) == 32
}

type Attempt struct {
	ID         string    `json:"attempt_id"`
	BrowserURL string    `json:"browser_url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (s *Service) Start(ctx context.Context, name, challenge string) (Attempt, error) {
	if name != "google" && name != "yandex" {
		return Attempt{}, ErrInvalid
	}
	if _, ok := s.providers[name]; !ok {
		return Attempt{}, ErrDisabled
	}
	raw, err := hex.DecodeString(challenge)
	if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != challenge {
		return Attempt{}, ErrInvalid
	}
	id := token()
	a := Attempt{ID: id, BrowserURL: s.origin + prefix + "/begin/" + id}
	err = s.pool.QueryRow(ctx, `INSERT INTO oauth_login_attempts(id,provider,device_hash,expires_at)
        VALUES($1,$2,$3,clock_timestamp()+interval '5 minutes') RETURNING expires_at`, id, name, raw).Scan(&a.ExpiresAt)
	return a, err
}

type BrowserStart struct{ URL, State, Cookie string }

func (s *Service) Begin(ctx context.Context, id string) (BrowserStart, error) {
	if !validToken(id) {
		return BrowserStart{}, ErrAttempt
	}
	state, cookie, verifier, nonce := token(), token(), token(), token()
	var name string
	err := s.pool.QueryRow(ctx, `UPDATE oauth_login_attempts SET phase='browser',state_hash=$2,cookie_hash=$3,verifier=$4,nonce=$5
        WHERE id=$1 AND phase='new' AND expires_at>clock_timestamp() RETURNING provider`, id, digest(state), digest(cookie), verifier, nonce).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return BrowserStart{}, ErrAttempt
	}
	if err != nil {
		return BrowserStart{}, err
	}
	p, ok := s.providers[name]
	if !ok {
		return BrowserStart{}, ErrDisabled
	}
	return BrowserStart{p.authorize(s.callback(name), state, verifier, nonce), state, cookie}, nil
}

type BrowserResult struct{ ID, Code string }

func (s *Service) Callback(ctx context.Context, name, state, cookie, code, providerError string) (BrowserResult, error) {
	p, ok := s.providers[name]
	if !ok || !validToken(state) || !validToken(cookie) {
		return BrowserResult{}, ErrAttempt
	}
	var id, verifier, nonce string
	var deviceHash []byte
	var expiry time.Time
	var purpose string
	var owner *int64
	var access []byte
	// Claim once before contacting the provider. A crash during the external
	// exchange requires a fresh login since its authorization code is single-use.
	err := s.pool.QueryRow(ctx, `DELETE FROM oauth_login_attempts WHERE state_hash=$1 AND cookie_hash=$2
        AND provider=$3 AND phase='browser' AND expires_at>clock_timestamp()
        RETURNING id,device_hash,verifier,nonce,expires_at,purpose,owner_id,access_hash`, digest(state), digest(cookie), name).Scan(&id, &deviceHash, &verifier, &nonce, &expiry, &purpose, &owner, &access)
	if errors.Is(err, pgx.ErrNoRows) {
		return BrowserResult{}, ErrAttempt
	}
	if err != nil {
		return BrowserResult{}, err
	}

	outcome := "provider_failed"
	var user identity
	if providerError == "access_denied" {
		outcome = "cancelled"
	} else if providerError == "" && code != "" && len(code) <= 4096 {
		user, err = p.exchange(ctx, s.callback(name), code, verifier, nonce)
		if err == nil {
			outcome = "verified"
		}
	}
	result := BrowserResult{id, token()}
	tag, err := s.pool.Exec(ctx, `INSERT INTO oauth_login_attempts(id,provider,device_hash,expires_at,phase,return_hash,outcome,subject,email,purpose,owner_id,access_hash)
        SELECT $1,$2,$3,least($4::timestamptz,clock_timestamp()+interval '1 minute'),'ready',$5,$6,$7,$8,$9,$10,$11
        WHERE $4::timestamptz>clock_timestamp()`, id, name, deviceHash, expiry, digest(result.Code), outcome, user.subject, user.email, purpose, owner, access)
	if err == nil && tag.RowsAffected() == 0 {
		return BrowserResult{}, ErrAttempt
	}
	return result, err
}

func (s *Service) Complete(ctx context.Context, id, code, secret string) (auth.LoginResult, error) {
	// Mobile generates 32 random bytes, encodes them as lowercase hex, and
	// hashes the UTF-8 string to form the challenge.
	raw, err := hex.DecodeString(secret)
	if !validToken(id) || !validToken(code) || err != nil || len(raw) != 32 || hex.EncodeToString(raw) != secret {
		return auth.LoginResult{}, ErrAttempt
	}
	return s.complete(ctx, id, code, secret, "login", "", false)
}

func (s *Service) complete(ctx context.Context, id, code, secret, purpose, access string, execute bool) (auth.LoginResult, error) {
	var result auth.LoginResult
	var terminal error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var identity auth.ExternalIdentity
		var email, outcome string
		var owner *int64
		var accessHash []byte
		err := tx.QueryRow(ctx, `DELETE FROM oauth_login_attempts WHERE id=$1 AND return_hash=$2 AND device_hash=$3
            AND phase='ready' AND expires_at>clock_timestamp() AND purpose=$4 RETURNING provider,subject,email,outcome,owner_id,access_hash`, id, digest(code), digest(secret), purpose).Scan(&identity.Provider, &identity.Subject, &email, &outcome, &owner, &accessHash)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAttempt
		}
		if err != nil {
			return err
		}
		switch outcome {
		case "cancelled":
			terminal = ErrCancelled
		case "provider_failed":
			terminal = ErrProvider
		case "verified":
			if purpose != "login" {
				actual, err := s.auth.Authenticate(ctx, access)
				if err != nil || owner == nil || actual.ID != *owner || !bytes.Equal(accessHash, digest(access)) {
					return auth.ErrUnauthorized
				}
				if purpose == "link" {
					return auth.LinkExternalIdentity(ctx, tx, actual.ID, identity)
				}
				var matches bool
				if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_external_identities WHERE user_id=$1 AND provider=$2 AND subject=$3)", actual.ID, identity.Provider, identity.Subject).Scan(&matches); err != nil {
					return err
				}
				if !matches {
					return auth.ErrCredentials
				}
				if !execute {
					return ErrInvalid
				}
				return auth.DeleteConfirmedAccount(ctx, tx, actual.ID)
			}
			result, err = s.auth.LoginExternal(ctx, tx, identity, email)
			if errors.Is(err, auth.ErrExternalLinkRequired) || errors.Is(err, auth.ErrExternalEmailRequired) {
				terminal = err
				return nil
			}
			return err
		default:
			return ErrAttempt
		}
		return nil
	})
	if err != nil {
		return auth.LoginResult{}, err
	}
	return result, terminal
}

func (s *Service) CleanExpired(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		_, err := s.pool.Exec(ctx, `DELETE FROM oauth_login_attempts WHERE id IN (SELECT id FROM oauth_login_attempts
            WHERE expires_at<=clock_timestamp() ORDER BY expires_at LIMIT 1000 FOR UPDATE SKIP LOCKED)`)
		if err != nil && ctx.Err() == nil {
			slog.Warn("OAuth attempt cleanup failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// StartProtected binds linking or deletion to the exact current access session.
func (s *Service) StartProtected(ctx context.Context, provider, challenge, purpose, access, password string) (Attempt, error) {
	if purpose != "link" && purpose != "delete" {
		return Attempt{}, ErrInvalid
	}
	user, err := s.auth.Authenticate(ctx, access)
	if err != nil {
		return Attempt{}, err
	}
	if purpose == "link" {
		if err = s.auth.ConfirmPassword(ctx, user.ID, user.Email, password); err != nil {
			var recent bool
			if dbErr := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM sessions WHERE access_token_hash=$1 AND external_login AND created_at>clock_timestamp()-interval '5 minutes')", digest(access)).Scan(&recent); dbErr != nil {
				return Attempt{}, dbErr
			}
			if !recent {
				return Attempt{}, auth.ErrCredentials
			}
		}
	}
	a, err := s.Start(ctx, provider, challenge)
	if err != nil {
		return a, err
	}
	_, err = s.pool.Exec(ctx, "UPDATE oauth_login_attempts SET purpose=$2,owner_id=$3,access_hash=$4 WHERE id=$1", a.ID, purpose, user.ID, digest(access))
	return a, err
}
func (s *Service) CompleteProtected(ctx context.Context, id, code, secret, purpose, access string) error {
	if purpose != "link" && purpose != "delete" {
		return ErrInvalid
	}
	raw, err := hex.DecodeString(secret)
	if !validToken(id) || !validToken(code) || err != nil || len(raw) != 32 || hex.EncodeToString(raw) != secret {
		return ErrAttempt
	}
	_, err = s.complete(ctx, id, code, secret, purpose, access, true)
	return err
}

type Methods struct {
	Password  bool     `json:"password"`
	Providers []string `json:"providers"`
}

func (s *Service) Methods(ctx context.Context, access string) (Methods, error) {
	u, err := s.auth.Authenticate(ctx, access)
	if err != nil {
		return Methods{}, err
	}
	m := Methods{Providers: []string{}}
	err = s.pool.QueryRow(ctx, "SELECT password_hash IS NOT NULL FROM users WHERE id=$1", u.ID).Scan(&m.Password)
	if err != nil {
		return m, err
	}
	rows, err := s.pool.Query(ctx, "SELECT provider FROM user_external_identities WHERE user_id=$1 ORDER BY provider", u.ID)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			return m, err
		}
		m.Providers = append(m.Providers, p)
	}
	return m, rows.Err()
}
