package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

type fakeJobs struct {
	mu        sync.Mutex
	jobs      []repository.NotificationJob
	completed []int64
	retried   []int64
	failed    []int64
	delays    []time.Duration
	audits    []audit.Record
}

func (f *fakeJobs) ClaimNotificationJobs(context.Context, string, int, time.Time, time.Duration) ([]repository.NotificationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := append([]repository.NotificationJob(nil), f.jobs...)
	f.jobs = nil
	return result, nil
}

func (f *fakeJobs) CompleteNotificationJob(_ context.Context, id int64, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, id)
	return nil
}

func (f *fakeJobs) RetryNotificationJob(_ context.Context, id int64, _, _ string, _ time.Time, delay time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retried = append(f.retried, id)
	f.delays = append(f.delays, delay)
	return nil
}

func (f *fakeJobs) FailNotificationJob(_ context.Context, id int64, _, _ string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, id)
	return nil
}

func (f *fakeJobs) AppendAudit(_ context.Context, record audit.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audits = append(f.audits, record)
	return nil
}

func (f *fakeJobs) ListAudit(context.Context, string, string, domain.Page) (domain.PageResult[audit.Record], error) {
	return domain.PageResult[audit.Record]{}, nil
}

type senderFunc func(context.Context, int64) error

func (f senderFunc) Send(ctx context.Context, id int64) error { return f(ctx, id) }

func testWorker(repo *fakeJobs, sender Sender) *NotificationWorker {
	return NewNotifications(repo, repo, sender, clock.Fixed{Time: time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)},
		slog.New(slog.NewTextHandler(io.Discard, nil)), "worker-test", time.Second, 50*time.Millisecond)
}

func TestWorkerCompletesSuccessfulDelivery(t *testing.T) {
	repo := &fakeJobs{jobs: []repository.NotificationJob{{ID: 1, NotificationID: 11, MaxAttempts: 3}}}
	worker := testWorker(repo, senderFunc(func(_ context.Context, id int64) error {
		if id != 11 {
			t.Fatalf("notification id=%d", id)
		}
		return nil
	}))
	if err := worker.ProcessDue(context.Background()); err != nil {
		t.Fatalf("ProcessDue: %v", err)
	}
	if len(repo.completed) != 1 || repo.completed[0] != 1 || len(repo.retried) != 0 || len(repo.failed) != 0 {
		t.Fatalf("completion state completed=%v retried=%v failed=%v", repo.completed, repo.retried, repo.failed)
	}
}

func TestWorkerRetriesTransientFailureWithBoundedBackoff(t *testing.T) {
	for attempt := 0; attempt < 4; attempt++ {
		repo := &fakeJobs{jobs: []repository.NotificationJob{{ID: int64(attempt + 1), NotificationID: 20, Attempts: attempt, MaxAttempts: 10}}}
		worker := testWorker(repo, senderFunc(func(context.Context, int64) error { return errors.New("temporary") }))
		if err := worker.ProcessDue(context.Background()); err == nil {
			t.Fatalf("attempt %d returned nil", attempt)
		}
		want := time.Duration(1<<attempt) * time.Second
		if len(repo.retried) != 1 || len(repo.delays) != 1 || repo.delays[0] != want {
			t.Fatalf("attempt=%d retried=%v delays=%v want=%v", attempt, repo.retried, repo.delays, want)
		}
	}
}

func TestWorkerMarksExplicitPermanentFailureAndAudits(t *testing.T) {
	repo := &fakeJobs{jobs: []repository.NotificationJob{{ID: 7, NotificationID: 77, Attempts: 0, MaxAttempts: 5}}}
	worker := testWorker(repo, senderFunc(func(context.Context, int64) error { return errors.Join(ErrPermanent, errors.New("invalid address")) }))
	if err := worker.ProcessDue(context.Background()); err == nil {
		t.Fatal("permanent failure returned nil")
	}
	if len(repo.failed) != 1 || repo.failed[0] != 7 || len(repo.retried) != 0 {
		t.Fatalf("failure state failed=%v retried=%v", repo.failed, repo.retried)
	}
	if len(repo.audits) != 1 || repo.audits[0].Action != "notification.permanent_failure" || repo.audits[0].Result != audit.Failed {
		t.Fatalf("audit=%+v", repo.audits)
	}
}

func TestWorkerMarksFailureAtAttemptLimit(t *testing.T) {
	repo := &fakeJobs{jobs: []repository.NotificationJob{{ID: 8, NotificationID: 88, Attempts: 2, MaxAttempts: 3}}}
	worker := testWorker(repo, senderFunc(func(context.Context, int64) error { return errors.New("still failing") }))
	if err := worker.ProcessDue(context.Background()); err == nil {
		t.Fatal("attempt-limit failure returned nil")
	}
	if len(repo.failed) != 1 || len(repo.retried) != 0 {
		t.Fatalf("failed=%v retried=%v", repo.failed, repo.retried)
	}
}

func TestWorkerAttemptContextTimesOutSender(t *testing.T) {
	repo := &fakeJobs{jobs: []repository.NotificationJob{{ID: 9, NotificationID: 99, MaxAttempts: 3}}}
	worker := testWorker(repo, senderFunc(func(ctx context.Context, _ int64) error {
		<-ctx.Done()
		return ctx.Err()
	}))
	started := time.Now()
	if err := worker.ProcessDue(context.Background()); err == nil {
		t.Fatal("timed-out sender returned nil")
	}
	if time.Since(started) > time.Second {
		t.Fatalf("attempt timeout took too long: %v", time.Since(started))
	}
	if len(repo.retried) != 1 {
		t.Fatalf("timeout not retried: %+v", repo.retried)
	}
}

func TestWorkerRunStopsOnParentCancellation(t *testing.T) {
	repo := &fakeJobs{}
	worker := testWorker(repo, senderFunc(func(context.Context, int64) error { return nil }))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}
