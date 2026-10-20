package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/vandrouka/backend/internal/auth"
	"github.com/yourname/vandrouka/backend/internal/config"
	"github.com/yourname/vandrouka/backend/internal/content"
	"github.com/yourname/vandrouka/backend/internal/data"
	apihttp "github.com/yourname/vandrouka/backend/internal/http"
	"github.com/yourname/vandrouka/backend/internal/media"
	"github.com/yourname/vandrouka/backend/internal/planner"
	"github.com/yourname/vandrouka/backend/internal/routes"
	"github.com/yourname/vandrouka/backend/internal/routing"
	"github.com/yourname/vandrouka/backend/internal/store"
	"github.com/yourname/vandrouka/backend/internal/visits"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Load reference data from DATA_DIR at startup
	loader := data.NewLoader(cfg.DataDir)

	// Example: load all places from YAML files
	placesByCity, err := loader.LoadPlaces()
	if err != nil {
		slog.Warn("failed to load places from data dir", "error", err, "data_dir", cfg.DataDir)
	} else {
		total := 0
		for city, ps := range placesByCity {
			total += len(ps)
			slog.Info("loaded places", "city", city, "count", len(ps))
		}
		slog.Info("reference data loaded", "total_places", total, "data_dir", cfg.DataDir)
	}

	jsonFiles, err := loader.LoadAllJSON()
	if err != nil {
		slog.Warn("failed to load JSON reference files", "error", err)
	} else {
		slog.Info("JSON reference files loaded", "count", len(jsonFiles))
	}

	reviewed, err := content.Load(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("load reviewed catalog: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pool, err := store.Open(connectCtx, cfg.PostgresDSN)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	r := chi.NewRouter()
	r.Use(apihttp.RequestLogging)
	r.Get("/catalog-media/{filename}", reviewed.Media)
	r.Head("/catalog-media/{filename}", reviewed.Media)
	engine, err := planner.NewWithWeights(planner.ScoringWeights{WalkKM: cfg.PlannerWalkPenalty, CostBYN: cfg.PlannerCostPenalty, Unverified: cfg.PlannerUnverifiedPenalty})
	if err != nil {
		return fmt.Errorf("configure planner: %w", err)
	}
	catalog := store.NewCatalog(pool)
	client := routing.NewMapboxClient(cfg.MapboxURL, cfg.MapboxAccessToken, 20*time.Second)
	service := routes.NewService(catalog, routes.WithMatrixBuilder(planner.NewRouterMatrixBuilder(client)), routes.WithVariantPlanner(engine), routes.WithDirections(client))
	r.Group(func(r chi.Router) {
		r.Use(apihttp.CatalogResponses(10 * time.Second))
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			pingCtx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if err := pool.Ping(pingCtx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"status":"unavailable"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})
		apihttp.RegisterCatalog(r, content.Catalog{Base: catalog, Registry: reviewed})
	})
	authentication, err := auth.NewWithLifetimes(auth.Postgres{Pool: pool}, cfg.AuthAccessTTL, cfg.AuthRefreshTTL)
	if err != nil {
		return fmt.Errorf("configure authentication: %w", err)
	}
	r.Group(func(r chi.Router) {
		r.Use(apihttp.AuthResponses(10 * time.Second))
		apihttp.RegisterAuth(r, authentication)
	})

	confirmations, err := visits.New(visits.Postgres{Pool: pool}, visits.Policy{RadiusM: cfg.VisitRadiusM, AccuracyM: cfg.VisitAccuracyM, MaxAge: cfg.VisitMaxAge, FutureSkew: cfg.VisitFutureSkew})
	if err != nil {
		return fmt.Errorf("invalid visit policy")
	}
	r.Group(func(r chi.Router) {
		r.Use(apihttp.AuthResponses(10 * time.Second))
		apihttp.RegisterVisits(r, authentication, confirmations)
	})

	objects, err := media.NewS3(cfg.MediaS3Endpoint, cfg.MediaS3Bucket, cfg.MediaS3AccessKey, cfg.MediaS3SecretKey)
	if err != nil {
		return fmt.Errorf("invalid media configuration")
	}
	if cfg.MediaS3Endpoint != "" {
		initialize, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := objects.Init(initialize); err != nil {
			slog.Warn("media storage unavailable", "code", "media_unavailable")
		}
		cancel()
	}
	photos := media.New(media.Postgres{Pool: pool}, objects)
	r.Group(func(r chi.Router) {
		r.Use(apihttp.MediaResponses(45 * time.Second))
		apihttp.RegisterMedia(r, authentication, photos)
	})
	if cfg.MediaS3Endpoint != "" {
		workerCtx, workerCancel := context.WithCancel(ctx)
		workerDone := make(chan struct{})
		go func() {
			defer close(workerDone)
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				cycle, cancel := context.WithTimeout(workerCtx, 30*time.Second)
				if err := photos.Cleanup(cycle); err != nil && workerCtx.Err() == nil {
					slog.Warn("media cleanup pending", "code", "media_unavailable")
				}
				cancel()
				select {
				case <-workerCtx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
		defer func() { workerCancel(); <-workerDone }()
	}
	limiter, err := apihttp.NewRouteLimiter(apihttp.RouteLimitOptions{
		RequestsPerMinute: cfg.RouteRequestsPerMinute, Burst: cfg.RouteBurst,
		MaxConcurrent: cfg.RouteMaxConcurrent, MaxClients: cfg.RouteMaxClients,
		IdleTTL: time.Duration(cfg.RouteClientTTLSeconds) * time.Second,
	})
	if err != nil {
		return fmt.Errorf("configure route limits: %w", err)
	}
	r.Group(func(r chi.Router) {
		r.Use(apihttp.RouteResponses(apihttp.RouteTimeout), limiter.Middleware)
		apihttp.RegisterRoutes(r, service)
		apihttp.RegisterRecalculation(r, service)
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.HTTPPort), Handler: r,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: apihttp.RouteTimeout + 10*time.Second, IdleTimeout: 60 * time.Second}
	errors := make(chan error, 1)
	go func() { errors <- srv.ListenAndServe() }()
	slog.Info("API started", "port", cfg.HTTPPort)
	select {
	case err := <-errors:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	slog.Info("API stopped gracefully")
	return nil
}
