package config

import (
	"errors"
	"os"
)

type Config struct {
	DatabaseURL string
	RunnerAddr  string
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL environment variable is not set")
	}

	runnerAddr := os.Getenv("RUNNER_ADDR")
	if runnerAddr == "" {
		return Config{}, errors.New("RUNNER_ADDR is required")
	}

	return Config{
		DatabaseURL: databaseURL,
		RunnerAddr:  runnerAddr,
	}, nil
}
