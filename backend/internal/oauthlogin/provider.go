// Package oauthlogin implements browser login for the mobile app.
package oauthlogin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const googleIssuer = "https://accounts.google.com"

var ErrProvider = errors.New("provider could not confirm identity")

type provider struct {
	clientID, secret, authorizeURL, tokenURL, scope string
	profileURL                                      string // Yandex
	google                                          *oidc.IDTokenVerifier
	client                                          *http.Client
}

type identity struct{ subject, email string }

func configuredProviders() map[string]provider {
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	googleID := os.Getenv("GOOGLE_CLIENT_ID")
	jwks := oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), client), "https://www.googleapis.com/oauth2/v3/certs")
	configs := map[string]provider{
		"google": {
			clientID: googleID, secret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			authorizeURL: googleIssuer + "/o/oauth2/v2/auth", tokenURL: "https://oauth2.googleapis.com/token",
			scope: "openid email", client: client,
			google: oidc.NewVerifier(googleIssuer, jwks, &oidc.Config{ClientID: googleID, SupportedSigningAlgs: []string{oidc.RS256}}),
		},
		"yandex": {
			clientID: os.Getenv("YANDEX_CLIENT_ID"), secret: os.Getenv("YANDEX_CLIENT_SECRET"),
			authorizeURL: "https://oauth.yandex.ru/authorize", tokenURL: "https://oauth.yandex.ru/token",
			profileURL: "https://login.yandex.ru/info?format=json", scope: "login:email", client: client,
		},
	}
	for name, p := range configs {
		if p.clientID == "" || p.secret == "" {
			delete(configs, name)
		}
	}
	return configs
}

func (p provider) authorize(callback, state, verifier, nonce string) string {
	hash := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {p.clientID}, "scope": {p.scope}, "response_type": {"code"}, "redirect_uri": {callback}, "state": {state}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
	if p.google != nil {
		q.Set("nonce", nonce)
	}
	return p.authorizeURL + "?" + q.Encode()
}

func (p provider) readJSON(req *http.Request, target any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return ErrProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ErrProvider
	}
	const max = 64 << 10
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil || len(b) > max || json.Unmarshal(b, target) != nil {
		return ErrProvider
	}
	return nil
}

func (p provider) exchange(ctx context.Context, callback, code, verifier, nonce string) (identity, error) {
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {p.clientID}, "client_secret": {p.secret}, "code": {code}, "redirect_uri": {callback}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return identity{}, ErrProvider
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tokens struct {
		ID     string `json:"id_token"`
		Access string `json:"access_token"`
	}
	if err = p.readJSON(req, &tokens); err != nil {
		return identity{}, err
	}
	if p.google != nil {
		return p.googleIdentity(ctx, tokens.ID, nonce)
	}
	return p.yandexIdentity(ctx, tokens.Access)
}

func (p provider) googleIdentity(ctx context.Context, raw, nonce string) (identity, error) {
	token, err := p.google.Verify(ctx, raw)
	if err != nil || nonce == "" || token.Nonce != nonce || token.Subject == "" {
		return identity{}, ErrProvider
	}
	var claims struct {
		Email           string `json:"email"`
		Verified        bool   `json:"email_verified"`
		AuthorizedParty string `json:"azp"`
	}
	if token.Claims(&claims) != nil || (claims.AuthorizedParty != "" && claims.AuthorizedParty != p.clientID) || (len(token.Audience) > 1 && claims.AuthorizedParty != p.clientID) {
		return identity{}, ErrProvider
	}
	if !claims.Verified {
		claims.Email = ""
	}
	return identity{token.Subject, claims.Email}, nil
}

func (p provider) yandexIdentity(ctx context.Context, access string) (identity, error) {
	if access == "" {
		return identity{}, ErrProvider
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.profileURL, nil)
	if err != nil {
		return identity{}, ErrProvider
	}
	req.Header.Set("Authorization", "OAuth "+access)
	var profile struct {
		ID       string `json:"id"`
		ClientID string `json:"client_id"`
		Email    string `json:"default_email"`
	}
	if err = p.readJSON(req, &profile); err != nil || profile.ID == "" || profile.ClientID != p.clientID {
		return identity{}, ErrProvider
	}
	return identity{profile.ID, profile.Email}, nil
}
