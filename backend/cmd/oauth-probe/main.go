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
	probe, err := oauthprobe.FromEnvironment()
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
