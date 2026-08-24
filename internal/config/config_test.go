package config

import (
	"testing"
	"time"
)

func TestLoadUsesProductionSafeDefaults(t *testing.T) {
	for _, name := range []string{"HTTP_ADDR", "DB_PATH", "SESSION_TTL", "WORKER_INTERVAL", "WORKER_ATTEMPT_TIMEOUT", "WORKER_MAX_ATTEMPTS", "BOOTSTRAP_ADMIN_EMAIL", "BOOTSTRAP_ADMIN_PASSWORD"} {
		t.Setenv(name, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.DBPath != "data/rehab.db" {
		t.Fatalf("unexpected endpoint defaults: %+v", cfg)
	}
	if cfg.SessionTTL != 12*time.Hour || cfg.WorkerInterval != 2*time.Second || cfg.WorkerAttemptTimeout != 5*time.Second {
		t.Fatalf("unexpected duration defaults: %+v", cfg)
	}
	if cfg.WorkerMaxAttempts != 5 {
		t.Fatalf("unexpected max attempts: %d", cfg.WorkerMaxAttempts)
	}
}

func TestLoadParsesExplicitEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("DB_PATH", "/tmp/rehab-test.db")
	t.Setenv("SESSION_TTL", "45m")
	t.Setenv("WORKER_INTERVAL", "250ms")
	t.Setenv("WORKER_ATTEMPT_TIMEOUT", "3s")
	t.Setenv("WORKER_MAX_ATTEMPTS", "9")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", " officer@example.test ")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "correct horse battery staple")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" || cfg.DBPath != "/tmp/rehab-test.db" || cfg.SessionTTL != 45*time.Minute {
		t.Fatalf("explicit values not parsed: %+v", cfg)
	}
	if cfg.WorkerInterval != 250*time.Millisecond || cfg.WorkerAttemptTimeout != 3*time.Second || cfg.WorkerMaxAttempts != 9 {
		t.Fatalf("worker values not parsed: %+v", cfg)
	}
	if cfg.BootstrapEmail != "officer@example.test" {
		t.Fatalf("bootstrap email not trimmed: %q", cfg.BootstrapEmail)
	}
}

func TestLoadRejectsInvalidDurationsAndAttempts(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"bad session", "SESSION_TTL", "later"},
		{"zero session", "SESSION_TTL", "0s"},
		{"bad interval", "WORKER_INTERVAL", "-1s"},
		{"bad timeout", "WORKER_ATTEMPT_TIMEOUT", "0"},
		{"zero attempts", "WORKER_MAX_ATTEMPTS", "0"},
		{"many attempts", "WORKER_MAX_ATTEMPTS", "21"},
		{"text attempts", "WORKER_MAX_ATTEMPTS", "five"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, key := range []string{"SESSION_TTL", "WORKER_INTERVAL", "WORKER_ATTEMPT_TIMEOUT", "WORKER_MAX_ATTEMPTS"} {
				t.Setenv(key, "")
			}
			t.Setenv(test.key, test.value)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q accepted", test.key, test.value)
			}
		})
	}
}

func TestLoadRequiresCompleteBootstrapPair(t *testing.T) {
	tests := []struct {
		email    string
		password string
		valid    bool
	}{
		{"", "", true},
		{"officer@example.test", "password value", true},
		{"officer@example.test", "", false},
		{"", "password value", false},
	}
	for _, test := range tests {
		t.Setenv("BOOTSTRAP_ADMIN_EMAIL", test.email)
		t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", test.password)
		_, err := Load()
		if (err == nil) != test.valid {
			t.Errorf("email=%q password_set=%v: err=%v", test.email, test.password != "", err)
		}
	}
}
