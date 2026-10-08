package config

import (
	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env         string `env:"ENV" env-default:"development"`
	HTTPPort    int    `env:"HTTP_PORT" env-default:"8080"`
	PostgresDSN string `env:"POSTGRES_DSN" env-required:"true"`
	ValhallaURL string `env:"VALHALLA_URL" env-default:"http://valhalla:8002"`
}

func Load() (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
