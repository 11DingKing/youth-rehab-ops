package worker

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

type retryCancelRepo struct {
	repository.Notifications
	called chan struct{}
	ctxErr chan error
}

func (r *retryCancelRepo) ClaimNotificationJobs(context.Context, string, int, time.Time, time.Duration) ([]repository.NotificationJob, error) {
	return []repository.NotificationJob{{ID: 1, NotificationID: 2, Status: "processing", Attempts: 0, MaxAttempts: 5}}, nil
}
func (r *retryCancelRepo) RetryNotificationJob(ctx context.Context, _ int64, _ string, _ string, _ time.Time, _ time.Duration) error {
	close(r.called)
	r.ctxErr <- ctx.Err()
	return nil
}

type noopAudit struct{ repository.Audits }

func (noopAudit) AppendAudit(context.Context, audit.Record) error { return nil }

type retrySender struct{ cancel context.CancelFunc }

func (s retrySender) Send(context.Context, int64) error {
	s.cancel()
	return errors.New("temporary downstream failure")
}

func TestCancelledWorkerDoesNotPersistRetry(t *testing.T) {
	repo := &retryCancelRepo{called: make(chan struct{}), ctxErr: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	s := retrySender{cancel: cancel}
	w := NewNotifications(repo, noopAudit{}, s, clock.Fixed{Time: time.Date(2026, 8, 25, 4, 0, 0, 0, time.UTC)}, slog.Default(), "owner", time.Hour, time.Second)
	_ = w.ProcessDue(ctx)
	select {
	case <-repo.called:
		t.Fatal("cancelled worker persisted a retry")
	case <-time.After(100 * time.Millisecond):
	}
}
