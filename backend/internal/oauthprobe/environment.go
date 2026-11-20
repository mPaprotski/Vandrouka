package oauthprobe

import "os"

// FromEnvironment shares provider settings between the API and standalone probe.
func FromEnvironment() (*Probe, error) {
	return New(os.Getenv("OAUTH_PROBE_ORIGIN"), map[string]Provider{
		"google": {ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"), AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", UserURL: "https://openidconnect.googleapis.com/v1/userinfo", Scope: "openid email"},
		"yandex": {ClientID: os.Getenv("YANDEX_CLIENT_ID"), ClientSecret: os.Getenv("YANDEX_CLIENT_SECRET"), AuthorizeURL: "https://oauth.yandex.ru/authorize", TokenURL: "https://oauth.yandex.ru/token", UserURL: "https://login.yandex.ru/info?format=json", Scope: "login:email"},
	})
}
