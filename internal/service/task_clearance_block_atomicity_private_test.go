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

type failingBlockReleaseRepo struct {
	repository.CareStore
	created         *domain.Clearance
	incidentCleared bool
	blockActive     bool
	releaseErr      error
}

func (r *failingBlockReleaseRepo) CreateClearanceRecord(_ context.Context, _ domain.Actor, clearance domain.Clearance) (domain.Clearance, error) {
	clearance.ID = 900
	r.created = &clearance
	r.incidentCleared = true
	return clearance, nil
}

func (r *failingBlockReleaseRepo) ReleaseClearanceTrainingBlock(context.Context, domain.Actor, int64, time.Time) error {
	if r.releaseErr == nil {
		r.blockActive = false
	}
	return r.releaseErr
}

func (r *failingBlockReleaseRepo) GrantClearance(_ context.Context, _ domain.Actor, clearance domain.Clearance) (domain.Clearance, error) {
	if r.releaseErr != nil {
		return domain.Clearance{}, r.releaseErr
	}
	clearance.ID = 900
	r.created = &clearance
	r.incidentCleared = true
	r.blockActive = false
	return clearance, nil
}

func TestFailedBlockReleaseDoesNotLeaveClearanceCommitted(t *testing.T) {
	storeErr := errors.New("training block store unavailable")
	now := time.Date(2026, 8, 25, 3, 0, 0, 0, time.UTC)
	clearance := domain.Clearance{
		IncidentID: 31, FollowUpID: 88, Kind: domain.ClearanceConditional, Conditions: "non-contact only",
		ValidFrom: now.Add(time.Hour), ValidUntil: now.Add(48 * time.Hour),
	}
	t.Run("failure rolls back every workflow entity", func(t *testing.T) {
		repo := &failingBlockReleaseRepo{releaseErr: storeErr, blockActive: true}
		care := NewCare(repo, clock.Fixed{Time: now})
		_, err := care.Clear(context.Background(), domain.Actor{UserID: 72, Role: domain.RoleHealthProfessional}, clearance)
		if !errors.Is(err, storeErr) {
			t.Fatalf("clearance returned %v", err)
		}
		if repo.created != nil || repo.incidentCleared || !repo.blockActive {
			t.Fatalf("failed operation changed workflow: clearance=%+v incident_cleared=%t block_active=%t", repo.created, repo.incidentCleared, repo.blockActive)
		}
	})
	t.Run("recovered storage completes the workflow", func(t *testing.T) {
		repo := &failingBlockReleaseRepo{blockActive: true}
		care := NewCare(repo, clock.Fixed{Time: now})
		created, err := care.Clear(context.Background(), domain.Actor{UserID: 72, Role: domain.RoleHealthProfessional}, clearance)
		if err != nil || created.ID == 0 || repo.created == nil || !repo.incidentCleared || repo.blockActive {
			t.Fatalf("recovered clearance incomplete: clearance=%+v incident_cleared=%t block_active=%t err=%v", created, repo.incidentCleared, repo.blockActive, err)
		}
	})
}
