package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/yourname/vandrouka/backend/internal/config"
	"github.com/yourname/vandrouka/backend/internal/routing"
	"github.com/yourname/vandrouka/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("routing check failed", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := store.Open(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	places, err := store.NewCatalog(pool).ListPlaces(ctx, "minsk", "", "")
	if err != nil {
		return err
	}
	if len(places) == 0 {
		return fmt.Errorf("import places before checking routing")
	}
	if len(places) > 10 {
		places = places[:10]
	}
	points := make([]routing.Point, len(places))
	names := make([]string, len(places))
	for i, p := range places {
		points[i] = routing.Point{Lat: p.Lat, Lon: p.Lon}
		names[i] = p.Slug
	}
	client := routing.NewClient(cfg.ValhallaURL, 10*time.Second)
	start := time.Now()
	matrix, err := client.Matrix(ctx, points)
	if err != nil {
		return err
	}
	milliseconds := time.Since(start).Milliseconds()
	for i, row := range matrix {
		for j, leg := range row {
			if !leg.Reachable {
				return fmt.Errorf("no pedestrian connection: %s -> %s", names[i], names[j])
			}
		}
	}
	route, err := client.Route(ctx, []routing.Point{{Lat: 53.903333, Lon: 27.556111}, {Lat: 53.908333, Lon: 27.555833}})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"places": names, "matrix": matrix, "matrix_ms": milliseconds, "route_town_hall_to_trinity": route})
}
