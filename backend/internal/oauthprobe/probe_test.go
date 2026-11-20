package oauthprobe

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func post(t *testing.T, h http.Handler, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}
func startTest(t *testing.T, p *Probe, provider string) (string, string, *http.Cookie, string) {
	t.Helper()
	secret := strings.Repeat("a", 64)
	response := post(t, p.Handler(), "/oauth/attempts", map[string]string{"provider": provider, "challenge": digest(secret)})
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var result struct {
		ID  string `json:"id"`
		URL string `json:"browser_url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	p.Handler().ServeHTTP(response, httptest.NewRequest("GET", result.URL, nil))
	if response.Code != 302 {
		t.Fatal("no browser redirect", response.Code)
	}
	u, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("code_challenge") == "" {
		t.Fatal("missing PKCE")
	}
	return result.ID, secret, response.Result().Cookies()[0], u.Query().Get("state")
}
func TestBrowserExchange(t *testing.T) {
	for _, provider := range []string{"google", "yandex"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/token" {
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("client_secret") != "server-secret" || r.Form.Get("code_verifier") == "" || r.Form.Get("code") != "one-time-code" {
						t.Error("bad exchange")
					}
					io.WriteString(w, `{"access_token":"private-provider-token"}`)
					return
				}
				scheme := "Bearer "
				if provider == "yandex" {
					scheme = "OAuth "
				}
				if r.Header.Get("Authorization") != scheme+"private-provider-token" {
					t.Error("missing authorization")
				}
				if provider == "google" {
					io.WriteString(w, `{"sub":"google-subject"}`)
				} else {
					io.WriteString(w, `{"id":"yandex-subject"}`)
				}
			}))
			defer upstream.Close()
			config := Provider{ClientID: "client", ClientSecret: "server-secret", AuthorizeURL: "https://provider.test/authorize", TokenURL: upstream.URL + "/token", UserURL: upstream.URL + "/user", Scope: "identity"}
			p, err := New("https://probe.test", map[string]Provider{provider: config})
			if err != nil {
				t.Fatal(err)
			}
			id, secret, cookie, state := startTest(t, p, provider)
			response := post(t, p.Handler(), "/oauth/result", map[string]string{"id": id, "secret": strings.Repeat("b", 64)})
			if response.Code != 401 {
				t.Fatal("foreign device received result")
			}
			callback := "https://probe.test/oauth/callback/" + provider + "?state=" + state + "&code=one-time-code"
			response = httptest.NewRecorder()
			p.Handler().ServeHTTP(response, httptest.NewRequest("GET", callback, nil))
			if response.Code != 400 {
				t.Fatal("callback without cookie accepted")
			}
			request := httptest.NewRequest("GET", callback, nil)
			request.AddCookie(cookie)
			response = httptest.NewRecorder()
			p.Handler().ServeHTTP(response, request)
			if response.Code != 200 || calls.Load() != 2 {
				t.Fatal("exchange failed", response.Code, calls.Load())
			}
			if strings.Contains(response.Body.String(), "private-provider-token") {
				t.Fatal("token leaked")
			}
			response = httptest.NewRecorder()
			p.Handler().ServeHTTP(response, request)
			if response.Code != 400 || calls.Load() != 2 {
				t.Fatal("callback replay exchanged code again")
			}
			var accepted atomic.Int32
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					response := post(t, p.Handler(), "/oauth/result", map[string]string{"id": id, "secret": secret})
					if response.Code == 200 && strings.Contains(response.Body.String(), "success") {
						accepted.Add(1)
					}
				}()
			}
			wg.Wait()
			if accepted.Load() != 1 {
				t.Fatal("result was not single use", accepted.Load())
			}
		})
	}
}
func TestProbeErrorsAndExpiry(t *testing.T) {
	config := Provider{ClientID: "client", ClientSecret: "secret", AuthorizeURL: "https://provider.test/auth"}
	p, _ := New("https://probe.test", map[string]Provider{"google": config})
	response := post(t, p.Handler(), "/oauth/attempts", map[string]string{"provider": "yandex", "challenge": strings.Repeat("a", 64)})
	if response.Code != 503 {
		t.Fatal("unconfigured provider accepted")
	}
	id, secret, cookie, state := startTest(t, p, "google")
	response = httptest.NewRecorder()
	request := httptest.NewRequest("GET", "https://probe.test/oauth/callback/yandex?state="+state+"&error=access_denied", nil)
	request.AddCookie(cookie)
	p.Handler().ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatal("wrong provider accepted")
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest("GET", "https://probe.test/oauth/callback/google?state="+state+"&error=access_denied", nil)
	request.AddCookie(cookie)
	p.Handler().ServeHTTP(response, request)
	response = post(t, p.Handler(), "/oauth/result", map[string]string{"id": id, "secret": secret})
	if !strings.Contains(response.Body.String(), "cancelled") {
		t.Fatal("cancel not retained")
	}
	id, secret, _, _ = startTest(t, p, "google")
	p.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	response = post(t, p.Handler(), "/oauth/result", map[string]string{"id": id, "secret": secret})
	if response.Code != 401 {
		t.Fatal("expired attempt accepted")
	}
	for _, origin := range []string{"", "https://user:secret@probe.test", "https://probe.test/path", "https://probe.test?key=secret", "javascript:alert(1)"} {
		if _, err := New(origin, nil); err == nil {
			t.Fatal("invalid origin accepted")
		}
	}
}
func TestProbeCapacity(t *testing.T) {
	p, _ := New("https://probe.test", map[string]Provider{"google": {ClientID: "client", ClientSecret: "secret"}})
	for i := 0; i < 128; i++ {
		response := post(t, p.Handler(), "/oauth/attempts", map[string]string{"provider": "google", "challenge": strings.Repeat("a", 64)})
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
	}
	response := post(t, p.Handler(), "/oauth/attempts", map[string]string{"provider": "google", "challenge": strings.Repeat("a", 64)})
	if response.Code != 429 {
		t.Fatal("unbounded probe")
	}
}
