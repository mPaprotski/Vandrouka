// Package oauthprobe is a bounded, ephemeral browser compatibility probe.
// It never creates application accounts or issues application sessions.
package oauthprobe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Provider struct{ ClientID, ClientSecret, AuthorizeURL, TokenURL, UserURL, Scope string }
type attempt struct {
	provider, challenge, state, verifier, cookie, status string
	expires                                              time.Time
}
type Probe struct {
	origin    string
	providers map[string]Provider
	client    *http.Client
	mu        sync.Mutex
	attempts  map[string]*attempt
	now       func() time.Time
}

func New(origin string, providers map[string]Provider) (*Probe, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid probe origin")
	}
	if u.Scheme == "http" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return nil, errors.New("public probe origin must use HTTPS")
	}
	return &Probe{origin: strings.TrimRight(origin, "/"), providers: providers, attempts: map[string]*attempt{}, now: time.Now, client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("unexpected provider redirect") }}}, nil
}
func token() string { return base64.RawURLEncoding.EncodeToString(randomBytes()) }
func randomBytes() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (p *Probe) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /oauth/attempts", p.start)
	mux.HandleFunc("GET /oauth/begin/{id}", p.begin)
	mux.HandleFunc("GET /oauth/callback/{provider}", p.callback)
	mux.HandleFunc("POST /oauth/result", p.result)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}
func readInput(w http.ResponseWriter, r *http.Request, value any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		http.Error(w, "application/json required", 415)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		http.Error(w, "invalid request", 400)
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		http.Error(w, "invalid request", 400)
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func (p *Probe) clean() {
	for id, a := range p.attempts {
		if !p.now().Before(a.expires) {
			delete(p.attempts, id)
		}
	}
}
func (p *Probe) start(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Provider  string `json:"provider"`
		Challenge string `json:"challenge"`
	}
	if !readInput(w, r, &input) {
		return
	}
	config, ok := p.providers[input.Provider]
	if !ok || config.ClientID == "" || config.ClientSecret == "" {
		http.Error(w, "provider is not configured", 503)
		return
	}
	raw, err := hex.DecodeString(input.Challenge)
	if err != nil || len(raw) != 32 || input.Challenge != strings.ToLower(input.Challenge) {
		http.Error(w, "invalid challenge", 400)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clean()
	if len(p.attempts) >= 128 {
		http.Error(w, "probe busy", 429)
		return
	}
	id := token()
	a := &attempt{provider: input.Provider, challenge: input.Challenge, status: "pending", expires: p.now().Add(5 * time.Minute)}
	p.attempts[id] = a
	writeJSON(w, map[string]any{"id": id, "browser_url": p.origin + "/oauth/begin/" + id, "expires_at": a.expires})
}
func (p *Probe) begin(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.clean()
	id := r.PathValue("id")
	a := p.attempts[id]
	if a == nil || a.state != "" {
		p.mu.Unlock()
		http.Error(w, "attempt expired or already opened", 400)
		return
	}
	a.state = token()
	a.verifier = token()
	a.cookie = digest(token())
	cookieValue := a.cookie
	config := p.providers[a.provider]
	hash := sha256.Sum256([]byte(a.verifier))
	values := url.Values{"client_id": {config.ClientID}, "response_type": {"code"}, "scope": {config.Scope}, "redirect_uri": {p.origin + "/oauth/callback/" + a.provider}, "state": {a.state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "probe-" + id, Value: cookieValue, Path: "/oauth/callback/", HttpOnly: true, Secure: strings.HasPrefix(p.origin, "https:"), SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, config.AuthorizeURL+"?"+values.Encode(), http.StatusFound)
}
func (p *Probe) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	p.mu.Lock()
	p.clean()
	var found *attempt
	for id, a := range p.attempts {
		if a.state != "" && equal(a.state, state) && a.provider == r.PathValue("provider") && a.status == "pending" {
			cookie, err := r.Cookie("probe-" + id)
			if err == nil && equal(cookie.Value, a.cookie) {
				found = a
				break
			}
		}
	}
	if found == nil {
		p.mu.Unlock()
		http.Error(w, "invalid or expired callback", 400)
		return
	}
	found.status = "checking"
	config := p.providers[found.provider]
	verifier := found.verifier
	provider := found.provider
	p.mu.Unlock()
	status := "error"
	if r.URL.Query().Get("error") == "access_denied" {
		status = "cancelled"
	} else if code := r.URL.Query().Get("code"); code != "" && len(code) <= 4096 {
		if p.verify(r.Context(), config, provider, code, verifier) == nil {
			status = "success"
		}
	}
	p.mu.Lock()
	found.status = status
	found.verifier = ""
	found.state = ""
	p.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "Проверка завершена. Вернитесь в Вандроўку и нажмите «Проверить результат». Создание аккаунта подключается отдельным этапом.")
}
func (p *Probe) verify(ctx context.Context, c Provider, provider, code, verifier string) error {
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {c.ClientID}, "client_secret": {c.ClientSecret}, "redirect_uri": {p.origin + "/oauth/callback/" + provider}, "code": {code}, "code_verifier": {verifier}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err = p.fetch(request, &tokens); err != nil || tokens.AccessToken == "" {
		return errors.New("provider exchange failed")
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, c.UserURL, nil)
	if err != nil {
		return err
	}
	scheme := "Bearer "
	if provider == "yandex" {
		scheme = "OAuth "
	}
	request.Header.Set("Authorization", scheme+tokens.AccessToken)
	var user struct {
		Sub string `json:"sub"`
		ID  string `json:"id"`
	}
	if err = p.fetch(request, &user); err != nil {
		return err
	}
	if provider == "google" && user.Sub != "" || provider == "yandex" && user.ID != "" {
		return nil
	}
	return errors.New("provider identity missing")
}
func (p *Probe) fetch(request *http.Request, target any) error {
	response, err := p.client.Do(request)
	if err != nil {
		return errors.New("provider unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("provider rejected request")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(body) > 65536 {
		return errors.New("invalid provider response")
	}
	return json.Unmarshal(body, target)
}
func (p *Probe) result(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	if !readInput(w, r, &input) {
		return
	}
	if len(input.Secret) != 64 {
		http.Error(w, "invalid proof", 401)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clean()
	a := p.attempts[input.ID]
	if a == nil || !equal(a.challenge, digest(input.Secret)) {
		http.Error(w, "invalid proof or expired attempt", 401)
		return
	}
	status := a.status
	if status != "pending" && status != "checking" {
		delete(p.attempts, input.ID)
	}
	writeJSON(w, map[string]string{"status": status})
}
