package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port       string
	DBDriver   string // "sqlite" or "postgres"
	DBDSN      string // sqlite file path, or postgres connection string
	UploadDir  string
	WebDir     string // if set, serve static files live from this directory instead of the embedded copy
	OpenSignup bool
}

func Load() (Config, error) {
	cfg := Config{
		Port:       getEnv("CARAVEL_PORT", "8080"),
		DBDriver:   getEnv("CARAVEL_DB_DRIVER", "sqlite"),
		DBDSN:      getEnv("CARAVEL_DB_DSN", "data/caravel.db"),
		UploadDir:  getEnv("CARAVEL_UPLOAD_DIR", "uploads"),
		WebDir:     os.Getenv("CARAVEL_WEB_DIR"),
		OpenSignup: getEnvBool("CARAVEL_OPEN_SIGNUP", true),
	}

	if cfg.DBDriver != "sqlite" && cfg.DBDriver != "postgres" {
		return Config{}, fmt.Errorf("invalid CARAVEL_DB_DRIVER %q: must be %q or %q", cfg.DBDriver, "sqlite", "postgres")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
