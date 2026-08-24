package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr             string
	DBPath               string
	SessionTTL           time.Duration
	WorkerInterval       time.Duration
	WorkerAttemptTimeout time.Duration
	WorkerMaxAttempts    int
	BootstrapEmail       string
	BootstrapPassword    string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:             value("HTTP_ADDR", ":8080"),
		DBPath:               value("DB_PATH", "data/rehab.db"),
		SessionTTL:           12 * time.Hour,
		WorkerInterval:       2 * time.Second,
		WorkerAttemptTimeout: 5 * time.Second,
		WorkerMaxAttempts:    5,
		BootstrapEmail:       strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")),
		BootstrapPassword:    os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
	}
	var err error
	if cfg.SessionTTL, err = duration("SESSION_TTL", cfg.SessionTTL); err != nil {
		return Config{}, err
	}
	if cfg.WorkerInterval, err = duration("WORKER_INTERVAL", cfg.WorkerInterval); err != nil {
		return Config{}, err
	}
	if cfg.WorkerAttemptTimeout, err = duration("WORKER_ATTEMPT_TIMEOUT", cfg.WorkerAttemptTimeout); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("WORKER_MAX_ATTEMPTS")); raw != "" {
		cfg.WorkerMaxAttempts, err = strconv.Atoi(raw)
		if err != nil || cfg.WorkerMaxAttempts < 1 || cfg.WorkerMaxAttempts > 20 {
			return Config{}, fmt.Errorf("WORKER_MAX_ATTEMPTS must be between 1 and 20")
		}
	}
	if strings.TrimSpace(cfg.HTTPAddr) == "" || strings.TrimSpace(cfg.DBPath) == "" {
		return Config{}, errors.New("HTTP_ADDR and DB_PATH must not be empty")
	}
	if (cfg.BootstrapEmail == "") != (cfg.BootstrapPassword == "") {
		return Config{}, errors.New("bootstrap email and password must be set together")
	}
	return cfg, nil
}

func value(name, fallback string) string {
	if current := strings.TrimSpace(os.Getenv(name)); current != "" {
		return current
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}
