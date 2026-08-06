// Package config loads runtime configuration from the environment (and a
// local .env file, if present) so secrets like the database connection
// string never need to be hardcoded or committed.
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string
}

// Load reads .env (if it exists) then required environment variables.
func Load() (*Config, error) {
	_ = godotenv.Load() // ignore error: .env is optional (e.g. in production)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set (add it to .env or the environment)")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return &Config{Port: port, DatabaseURL: dbURL}, nil
}
