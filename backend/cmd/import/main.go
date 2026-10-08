package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourname/vandrouka/backend/internal/config"
	"github.com/yourname/vandrouka/backend/internal/importer"
	"github.com/yourname/vandrouka/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("import failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	city := flag.String("city", "minsk", "city slug")
	checkLinks := flag.Bool("check-links", true, "check source URLs after import (warnings only)")
	flag.Parse()
	if flag.NArg() != 1 {
		return fmt.Errorf("usage: import [-city minsk] [-check-links=false] <places-directory>")
	}
	places, err := importer.ReadDir(flag.Arg(0))
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	pool, err := store.Open(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	location, err := time.LoadLocation("Europe/Minsk")
	if err != nil {
		return err
	}
	if err := importer.Import(ctx, pool, *city, places, time.Now().In(location)); err != nil {
		return err
	}
	slog.Info("import completed", "city", *city, "places", len(places))
	if *checkLinks {
		linkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		warnings := importer.CheckLinks(linkCtx, places, &http.Client{Timeout: 5 * time.Second}, 5*time.Second)
		for _, warning := range warnings {
			slog.Warn("source link availability not confirmed", "place", warning.PlaceSlug, "url", warning.URL, "reason", warning.Reason)
		}
		slog.Info("source links checked", "warnings", len(warnings))
	}
	return nil
}
