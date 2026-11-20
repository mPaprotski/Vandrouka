// oauth-probe checks browser/provider compatibility without touching the application DB.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/yourname/vandrouka/backend/internal/oauthprobe"
)

func main() {
	origin := os.Getenv("OAUTH_PROBE_ORIGIN")
	providers := map[string]oauthprobe.Provider{
		"google": {ClientID: os.Getenv("GOOGLE_CLIENT_ID"), ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"), AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", UserURL: "https://openidconnect.googleapis.com/v1/userinfo", Scope: "openid email"},
		"yandex": {ClientID: os.Getenv("YANDEX_CLIENT_ID"), ClientSecret: os.Getenv("YANDEX_CLIENT_SECRET"), AuthorizeURL: "https://oauth.yandex.ru/authorize", TokenURL: "https://oauth.yandex.ru/token", UserURL: "https://login.yandex.ru/info?format=json", Scope: "login:email"},
	}
	probe, err := oauthprobe.New(origin, providers)
	if err != nil {
		log.Fatal("Set OAUTH_PROBE_ORIGIN to the public HTTPS origin")
	}
	address := os.Getenv("OAUTH_PROBE_LISTEN")
	if address == "" {
		address = "127.0.0.1:8082"
	}
	server := http.Server{Addr: address, Handler: probe.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 30 * time.Second}
	log.Print("OAuth compatibility probe started")
	log.Fatal(server.ListenAndServe())
}
