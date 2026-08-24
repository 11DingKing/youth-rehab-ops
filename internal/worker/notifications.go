package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

var ErrPermanent = errors.New("permanent notification failure")

type Sender interface {
	Send(context.Context, int64) error
}

type NotificationWorker struct {
	repo           repository.Notifications
	audits         repository.Audits
	sender         Sender
	now            clock.Clock
	logger         *slog.Logger
	owner          string
	interval       time.Duration
	attemptTimeout time.Duration
	lease          time.Duration
	batchSize      int
}

func NewNotifications(repo repository.Notifications, audits repository.Audits, sender Sender, now clock.Clock, logger *slog.Logger,
	owner string, interval, attemptTimeout time.Duration) *NotificationWorker {
	return &NotificationWorker{repo: repo, audits: audits, sender: sender, now: now, logger: logger, owner: owner,
		interval: interval, attemptTimeout: attemptTimeout, lease: attemptTimeout * 2, batchSize: 20}
}

func (w *NotificationWorker) Run(ctx context.Context) error {
	if err := w.ProcessDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Error("initial notification pass failed", "error", err)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.ProcessDue(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.Error("notification pass failed", "error", err)
			}
		}
	}
}

func (w *NotificationWorker) ProcessDue(ctx context.Context) error {
	now := w.now.Now()
	jobs, err := w.repo.ClaimNotificationJobs(ctx, w.owner, w.batchSize, now, w.lease)
	if err != nil {
		return fmt.Errorf("claim due notifications: %w", err)
	}
	var combined error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return errors.Join(combined, err)
		}
		if err := w.processOne(ctx, job); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (w *NotificationWorker) processOne(parent context.Context, job repository.NotificationJob) error {
	ctx, cancel := context.WithTimeout(parent, w.attemptTimeout)
	defer cancel()
	err := w.sender.Send(ctx, job.NotificationID)
	now := w.now.Now()
	if err == nil {
		if finishErr := w.repo.CompleteNotificationJob(parent, job.ID, w.owner, now); finishErr != nil {
			return fmt.Errorf("complete notification %d: %w", job.ID, finishErr)
		}
		return nil
	}
	message := err.Error()
	permanent := errors.Is(err, ErrPermanent) || job.Attempts+1 >= job.MaxAttempts
	if permanent {
		if finishErr := w.repo.FailNotificationJob(parent, job.ID, w.owner, message, now); finishErr != nil {
			return errors.Join(err, finishErr)
		}
		if auditErr := w.audits.AppendAudit(parent, audit.Record{ActorID: 0, ActorRole: "system", Action: "notification.permanent_failure",
			ObjectType: "notification", ObjectID: fmt.Sprint(job.NotificationID), Result: audit.Failed, Reason: message,
			RequestID: "worker:" + w.owner, CreatedAt: now}); auditErr != nil {
			return errors.Join(err, fmt.Errorf("audit permanent notification failure: %w", auditErr))
		}
		return fmt.Errorf("notification %d permanently failed: %w", job.NotificationID, err)
	}
	delay := backoff(job.Attempts + 1)
	retryCtx := retryPersistenceContext(parent)
	if finishErr := w.repo.RetryNotificationJob(retryCtx, job.ID, w.owner, message, now, delay); finishErr != nil {
		return errors.Join(err, finishErr)
	}
	return fmt.Errorf("notification %d scheduled for retry: %w", job.NotificationID, err)
}

func retryPersistenceContext(parent context.Context) context.Context {
	if parent == nil {
		return context.Background()
	}
	return context.WithoutCancel(parent)
}

func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	seconds := math.Pow(2, float64(attempt-1))
	if seconds > 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

type LogSender struct{ Logger *slog.Logger }

func (s LogSender) Send(ctx context.Context, notificationID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.Logger.InfoContext(ctx, "notification delivered", "notification_id", notificationID)
	return nil
}
