package config

import (
	"errors"
	"os"
)

type Config struct {
	DatabaseURL string
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL environment variable is not set")
	}

	return Config{
		DatabaseURL: databaseURL,
	}, nil
}
