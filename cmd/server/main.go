package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/config"
	"github.com/11DingKing/youth-rehab-ops/internal/httpapi"
	"github.com/11DingKing/youth-rehab-ops/internal/logging"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
	"github.com/11DingKing/youth-rehab-ops/internal/storage/sqlite"
	"github.com/11DingKing/youth-rehab-ops/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	logger := logging.New(os.Stdout, slog.LevelInfo)
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := sqlite.Open(root, cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	now := clock.System{}
	auth := service.NewAuth(store, now, cfg.SessionTTL)
	if err := auth.EnsureBootstrapOfficer(root, cfg.BootstrapEmail, cfg.BootstrapPassword); err != nil {
		return fmt.Errorf("bootstrap safety officer: %w", err)
	}
	incidents := service.NewIncidents(store, store, now, cfg.WorkerMaxAttempts)
	care := service.NewCare(store, now)
	schedule := service.NewSchedule(store, now)
	api := &httpapi.API{Auth: auth, Incidents: incidents, Care: care, Schedule: schedule, Store: store, Logger: logger}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	workerInstance := worker.NewNotifications(store, store, worker.LogSender{Logger: logger}, now, logger,
		fmt.Sprintf("worker-%d", os.Getpid()), cfg.WorkerInterval, cfg.WorkerAttemptTimeout)
	workerDone := make(chan error, 1)
	go func() { workerDone <- workerInstance.Run(root) }()
	serverDone := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", cfg.HTTPAddr)
		serverDone <- server.ListenAndServe()
	}()
	select {
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case err := <-workerDone:
		if !errors.Is(err, context.Canceled) {
			return fmt.Errorf("notification worker stopped: %w", err)
		}
	case <-root.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	logger.Info("service stopped")
	return nil
}
