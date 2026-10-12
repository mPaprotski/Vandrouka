package config

import (
	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	PlannerWalkPenalty       float64 `env:"PLANNER_WALK_PENALTY" env-default:"0.5"`
	PlannerCostPenalty       float64 `env:"PLANNER_COST_PENALTY" env-default:"0.1"`
	PlannerUnverifiedPenalty float64 `env:"PLANNER_UNVERIFIED_PENALTY" env-default:"0.5"`
	Env                      string  `env:"ENV" env-default:"development"`
	HTTPPort                 int     `env:"HTTP_PORT" env-default:"8080"`
	PostgresDSN              string  `env:"POSTGRES_DSN" env-required:"true"`
	MapboxURL                string  `env:"MAPBOX_URL" env-default:"https://api.mapbox.com"`
	MapboxAccessToken        string  `env:"MAPBOX_ACCESS_TOKEN"`
}

func Load() (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
