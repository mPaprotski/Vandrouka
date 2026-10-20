package config

import (
	"testing"
	"time"
)

func TestAuthAccessTTL(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://localhost/test")
	for _, value := range []struct {
		env  string
		want time.Duration
	}{{"24h", 24 * time.Hour}, {"72h", 72 * time.Hour}} {
		t.Setenv("AUTH_ACCESS_TTL", value.env)
		cfg, err := Load()
		if err != nil || cfg.AuthAccessTTL != value.want {
			t.Fatalf("AUTH_ACCESS_TTL=%s: err=%v", value.env, err)
		}
	}
	t.Setenv("AUTH_ACCESS_TTL", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("invalid duration accepted")
	}
}
