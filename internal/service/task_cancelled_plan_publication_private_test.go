package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

type controlledPlanPublicationRepo struct {
	repository.CareStore
	started   chan struct{}
	release   chan struct{}
	committed chan struct{}
	aborted   chan struct{}
}

func (r *controlledPlanPublicationRepo) StartPlanVersionPublication(ctx context.Context, _ domain.Actor, _ int64, _ int64, _ domain.RehabPlanVersion) <-chan repository.PlanPublicationResult {
	result := make(chan repository.PlanPublicationResult, 1)
	close(r.started)
	go func() {
		select {
		case <-ctx.Done():
			close(r.aborted)
			result <- repository.PlanPublicationResult{Err: ctx.Err()}
		case <-r.release:
			close(r.committed)
			result <- repository.PlanPublicationResult{Version: domain.RehabPlanVersion{Version: 2}}
		}
	}()
	return result
}

func TestCancelledPlanPublicationDoesNotCommitLater(t *testing.T) {
	repo := &controlledPlanPublicationRepo{started: make(chan struct{}), release: make(chan struct{}), committed: make(chan struct{}), aborted: make(chan struct{})}
	care := NewCare(repo, clock.Fixed{Time: time.Date(2026, 8, 25, 2, 0, 0, 0, time.UTC)})
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		_, err := care.PublishPlan(ctx, domain.Actor{UserID: 44, Role: domain.RoleHealthProfessional}, 81, 1,
			"progressive loading", "no sprinting", "balance drills", time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC))
		finished <- err
	}()
	<-repo.started
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publication returned %v", err)
	}
	select {
	case <-repo.aborted:
		return
	case <-time.After(100 * time.Millisecond):
	}
	close(repo.release)
	<-repo.committed
	t.Fatal("plan version committed after the publication request was cancelled")
}
