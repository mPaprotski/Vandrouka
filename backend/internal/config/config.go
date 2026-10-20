package config

import (
	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	RouteRequestsPerMinute   int     `env:"ROUTE_REQUESTS_PER_MINUTE" env-default:"6"`
	RouteBurst               int     `env:"ROUTE_BURST" env-default:"2"`
	RouteMaxConcurrent       int     `env:"ROUTE_MAX_CONCURRENT" env-default:"2"`
	RouteMaxClients          int     `env:"ROUTE_MAX_CLIENTS" env-default:"4096"`
	RouteClientTTLSeconds    int     `env:"ROUTE_CLIENT_TTL_SECONDS" env-default:"600"`
	PlannerWalkPenalty       float64 `env:"PLANNER_WALK_PENALTY" env-default:"0.5"`
	PlannerCostPenalty       float64 `env:"PLANNER_COST_PENALTY" env-default:"0.1"`
	PlannerUnverifiedPenalty float64 `env:"PLANNER_UNVERIFIED_PENALTY" env-default:"0.5"`
	Env                      string  `env:"ENV" env-default:"development"`
	HTTPPort                 int     `env:"HTTP_PORT" env-default:"8080"`
	PostgresDSN              string  `env:"POSTGRES_DSN" env-required:"true"`
	MapboxURL                string  `env:"MAPBOX_URL" env-default:"https://api.mapbox.com"`
	MapboxAccessToken        string  `env:"MAPBOX_ACCESS_TOKEN"`
	DataDir                  string  `env:"DATA_DIR" env-default:"./data"`
}

func Load() (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
