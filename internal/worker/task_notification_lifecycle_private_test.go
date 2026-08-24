package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

func TestTimedOutDeliveryDoesNotOutliveWorkerAttempt(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan struct{})
	repo := &fakeJobs{jobs: []repository.NotificationJob{{ID: 41, NotificationID: 410, MaxAttempts: 3}}}
	worker := testWorker(repo, senderFunc(func(context.Context, int64) error {
		close(started)
		<-release
		close(delivered)
		return nil
	}))

	finished := make(chan error, 1)
	go func() { finished <- worker.ProcessDue(context.Background()) }()
	<-started
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("worker returned before delivery stopped: %v", err)
		}
		repo.mu.Lock()
		retried := append([]int64(nil), repo.retried...)
		repo.mu.Unlock()
		if len(retried) != 0 {
			t.Fatalf("retry persisted while original delivery was still running: %v", retried)
		}
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("original delivery did not stop after release")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("worker did not finish after sender returned")
	}
}
