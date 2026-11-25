package apihttp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/vandrouka/backend/internal/auth"
	"github.com/yourname/vandrouka/backend/internal/oauthlogin"
)

type oauthRoundTrip func(*http.Request) (*http.Response, error)

func (f oauthRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBrowserOAuthLoginPostgres(t *testing.T) {
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
	tag := uuid.NewString()
	googleEmail, yandexEmail := tag+"-google@example.invalid", tag+"-yandex@example.invalid"
	defer pool.Exec(ctx, "DELETE FROM users WHERE email=ANY($1)", []string{googleEmail, yandexEmail})
	const origin = "https://api.example.invalid"
	for _, name := range []string{"GOOGLE", "YANDEX"} {
		t.Setenv(name+"_CLIENT_ID", name+"-client")
		t.Setenv(name+"_CLIENT_SECRET", "fixture")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
	if err != nil {
		t.Fatal(err)
	}
	var authorize url.Values
	var scenario string
	// The real adapters and OIDC verifier run with a simulated provider transport.
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()
	http.DefaultTransport = oauthRoundTrip(func(r *http.Request) (*http.Response, error) {
		var data any
		switch r.URL.Host + r.URL.Path {
		case "www.googleapis.com/oauth2/v3/certs":
			data = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Use: "sig", Algorithm: "RS256"}}}
		case "oauth2.googleapis.com/token", "oauth.yandex.ru/token":
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			verifier := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if r.Form.Get("code") != "test-code" || r.Form.Get("redirect_uri") != authorize.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(verifier[:]) != authorize.Get("code_challenge") || r.Form.Get("client_id") != authorize.Get("client_id") || r.Form.Get("client_secret") != "fixture" {
				return nil, errors.New("bad token request")
			}
			if scenario == "network" {
				return nil, errors.New("provider offline")
			}
			if scenario == "empty" {
				data = map[string]string{}
				break
			}
			if r.URL.Host == "oauth.yandex.ru" {
				data = map[string]string{"access_token": "fixture-access"}
				break
			}
			claims := map[string]any{"iss": "https://accounts.google.com", "aud": "GOOGLE-client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "sub": tag, "nonce": authorize.Get("nonce"), "email": googleEmail, "email_verified": true}
			switch scenario {
			case "wrong_client":
				claims["aud"] = "other-client"
			case "wrong_nonce":
				claims["nonce"] = "other-nonce"
			case "expired_token":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "email_conflict":
				claims["sub"] = tag + "-other"
			case "unverified":
				claims["sub"] = tag + "-other"
				claims["email_verified"] = false
			}
			payload, err := json.Marshal(claims)
			if err != nil {
				return nil, err
			}
			signed, err := signer.Sign(payload)
			if err != nil {
				return nil, err
			}
			jwt, err := signed.CompactSerialize()
			if err != nil {
				return nil, err
			}
			if scenario == "bad_signature" {
				parts := strings.Split(jwt, ".")
				parts[2] = base64.RawURLEncoding.EncodeToString(make([]byte, 256))
				jwt = strings.Join(parts, ".")
			}
			data = map[string]string{"id_token": jwt}
		case "login.yandex.ru/info":
			if r.Header.Get("Authorization") != "OAuth fixture-access" {
				return nil, errors.New("missing authorization")
			}
			profile := map[string]string{"id": tag, "default_email": yandexEmail, "client_id": "YANDEX-client"}
			if scenario == "wrong_client" {
				profile["client_id"] = "other-client"
			}
			if scenario == "missing_id" {
				delete(profile, "id")
			}
			data = profile
		default:
			return nil, fmt.Errorf("unexpected provider request to %s", r.URL.Host)
		}
		b, err := json.Marshal(data)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b)))}, err
	})
	newHandler := func() http.Handler {
		s, err := oauthlogin.New(origin, pool, accounts)
		if err != nil {
			t.Fatal(err)
		}
		r := chi.NewRouter()
		r.Use(AuthResponses(25 * time.Second))
		RegisterOAuthLogin(r, s)
		return r
	}
	const prefix = "/v1/auth/oauth/"
	const device = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	challenge := sha256.Sum256([]byte(device))
	request := func(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	var owner int64
	for _, tc := range []struct {
		name, mode string
		want       int
	}{
		{"google", "success", 200}, {"google", "repeat", 200}, {"yandex", "success", 200},
		{"google", "cancelled", 400}, {"yandex", "cancelled", 400},
		{"google", "wrong_client", 503}, {"yandex", "wrong_client", 503},
		{"google", "wrong_nonce", 503}, {"google", "bad_signature", 503}, {"google", "expired_token", 503},
		{"google", "empty", 503}, {"yandex", "missing_id", 503}, {"google", "network", 503}, {"yandex", "network", 503},
		{"google", "email_conflict", 409}, {"google", "unverified", 422}, {"yandex", "expired_attempt", 401},
	} {
		t.Run(tc.name+"/"+tc.mode, func(t *testing.T) {
			scenario = tc.mode
			h := newHandler()
			begin := request(h, "POST", prefix+"attempts", fmt.Sprintf(`{"provider":%q,"challenge":%q}`, tc.name, hex.EncodeToString(challenge[:])), nil)
			var a oauthlogin.Attempt
			if begin.Code != 201 || json.Unmarshal(begin.Body.Bytes(), &a) != nil || a.BrowserURL != origin+prefix+"begin/"+a.ID {
				t.Fatal("start", begin.Code)
			}
			defer pool.Exec(ctx, "DELETE FROM oauth_login_attempts WHERE id=$1", a.ID)
			b := request(h, "GET", a.BrowserURL, "", nil)
			if b.Code != 302 || len(b.Result().Cookies()) != 1 {
				t.Fatal("begin", b.Code)
			}
			if request(h, "GET", a.BrowserURL, "", nil).Code != 401 {
				t.Fatal("begin replay")
			}
			c := b.Result().Cookies()[0]
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
				t.Fatal("cookie attributes")
			}
			location, err := url.Parse(b.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			authorize = location.Query()
			if authorize.Get("redirect_uri") != origin+prefix+"callback/"+tc.name || authorize.Get("code_challenge_method") != "S256" {
				t.Fatal("authorize parameters")
			}
			callback := prefix + "callback/" + tc.name + "?code=test-code&state=" + authorize.Get("state")
			if scenario == "cancelled" {
				callback += "&error=access_denied"
			}
			if request(h, "GET", callback, "", nil).Code != 401 {
				t.Fatal("missing cookie accepted")
			}
			other := "yandex"
			if tc.name == "yandex" {
				other = "google"
			}
			if request(h, "GET", strings.Replace(callback, "callback/"+tc.name, "callback/"+other, 1), "", c).Code != 401 {
				t.Fatal("provider mismatch")
			}
			if request(h, "GET", strings.Replace(callback, "state=", "state=x", 1), "", c).Code != 401 {
				t.Fatal("state mismatch")
			}
			cb := request(h, "GET", callback, "", c)
			if cb.Code != 200 || cb.Header().Get("Cache-Control") != "no-store" || cb.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("callback", cb.Code)
			}
			if request(h, "GET", callback, "", c).Code != 401 {
				t.Fatal("callback replay")
			}
			match := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(cb.Body.String())
			if len(match) != 2 {
				t.Fatal("missing return link")
			}
			link, err := url.Parse(html.UnescapeString(match[1]))
			if err != nil || link.Scheme != "vandrouka" || link.Host != "auth-return" || link.Query().Get("attempt") != a.ID {
				t.Fatal("invalid return link")
			}
			proof := func(secret, code string) string {
				return fmt.Sprintf(`{"attempt_id":%q,"device_secret":%q,"code":%q}`, a.ID, secret, code)
			}
			code := link.Query().Get("code")
			h = newHandler() // a different API process can complete the same attempt
			if request(h, "POST", prefix+"complete", proof(strings.Repeat("0", 64), code), nil).Code != 401 {
				t.Fatal("foreign device")
			}
			if request(h, "POST", prefix+"complete", proof(device, strings.Repeat("A", 43)), nil).Code != 401 {
				t.Fatal("incorrect handoff")
			}
			if scenario == "expired_attempt" {
				if _, err := pool.Exec(ctx, "UPDATE oauth_login_attempts SET expires_at=now()-interval '1 second' WHERE id=$1", a.ID); err != nil {
					t.Fatal(err)
				}
			}
			finish := func() *httptest.ResponseRecorder {
				return request(h, "POST", prefix+"complete", proof(device, code), nil)
			}
			results := make(chan *httptest.ResponseRecorder, 2)
			for i := 0; i < 2; i++ {
				go func() { results <- finish() }()
			}
			first, second := <-results, <-results
			if first.Code == 401 {
				first, second = second, first
			}
			if first.Code != tc.want || second.Code != 401 {
				t.Fatalf("complete got %d and %d, expected %d and 401", first.Code, second.Code, tc.want)
			}
			if tc.want != 200 {
				return
			}
			var login auth.LoginResult
			if err = json.Unmarshal(first.Body.Bytes(), &login); err != nil {
				t.Fatal(err)
			}
			me, err := accounts.Authenticate(ctx, login.AccessToken)
			if err != nil || me.ID != login.User.ID {
				t.Fatal("access session", err)
			}
			refreshed, err := accounts.Refresh(ctx, login.RefreshToken)
			if err != nil || refreshed.User.ID != me.ID {
				t.Fatal("refresh", err)
			}
			if tc.name == "google" {
				if owner != 0 && owner != me.ID {
					t.Fatal("duplicate account")
				}
				owner = me.ID
			} else if me.ID == owner {
				t.Fatal("provider subjects mixed")
			}
		})
	}
}
