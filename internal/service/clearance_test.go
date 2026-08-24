package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

// fakeCareStore is a minimal CareStore that records which methods were called and lets a
// test inject a transient storage failure on the clearance path. It models the half-grant
// hazard the real store guards against: the atomic GrantClearance either fully applies
// (clearance recorded AND block released) or is rolled back, so a failure changes nothing.
type fakeCareStore struct {
	clearance   domain.Clearance
	releaseDone bool
	grantCalls  int
	failGrant   bool
	failRelease bool
}

func (f *fakeCareStore) GrantClearance(_ context.Context, _ domain.Actor, clearance domain.Clearance) (domain.Clearance, error) {
	f.grantCalls++
	if f.failGrant {
		return domain.Clearance{}, domain.WrapUnavailable("grant clearance", errors.New("disk full"))
	}
	// Single atomic operation: record clearance and release block together or not at all.
	f.clearance = clearance
	f.clearance.ID = 1
	f.releaseDone = true
	return f.clearance, nil
}

// CreateClearanceRecord / ReleaseClearanceTrainingBlock exist on the interface but the
// service must NOT call them for clearance: that split path is what left the half-grant.
func (f *fakeCareStore) CreateClearanceRecord(_ context.Context, _ domain.Actor, clearance domain.Clearance) (domain.Clearance, error) {
	f.clearance = clearance
	f.clearance.ID = 1
	if f.failRelease {
		// Simulate the buggy split path: record committed, block release not yet attempted.
		return f.clearance, nil
	}
	return f.clearance, nil
}

func (f *fakeCareStore) ReleaseClearanceTrainingBlock(_ context.Context, _ domain.Actor, _ int64, _ time.Time) error {
	if f.failRelease {
		return domain.WrapUnavailable("release training block", errors.New("disk full"))
	}
	f.releaseDone = true
	return nil
}

func (f *fakeCareStore) CreateReferral(context.Context, domain.Actor, domain.Referral) (domain.Referral, error) {
	return domain.Referral{}, nil
}
func (f *fakeCareStore) GetReferral(context.Context, int64) (domain.Referral, error) {
	return domain.Referral{}, domain.ErrNotFound
}
func (f *fakeCareStore) AcceptReferral(context.Context, domain.Actor, int64, int64) (domain.Referral, error) {
	return domain.Referral{}, nil
}
func (f *fakeCareStore) ReturnReferral(context.Context, domain.Actor, int64, int64, string) (domain.Referral, error) {
	return domain.Referral{}, nil
}
func (f *fakeCareStore) CreatePlan(context.Context, domain.Actor, domain.RehabPlan) (domain.RehabPlan, error) {
	return domain.RehabPlan{}, nil
}
func (f *fakeCareStore) PublishPlanVersion(context.Context, domain.Actor, int64, int64, domain.RehabPlanVersion) (domain.RehabPlanVersion, error) {
	return domain.RehabPlanVersion{}, nil
}
func (f *fakeCareStore) RecordFollowUp(context.Context, domain.Actor, domain.FollowUp) (domain.FollowUp, error) {
	return domain.FollowUp{}, nil
}
func (f *fakeCareStore) GetFollowUp(context.Context, int64) (domain.FollowUp, error) {
	return domain.FollowUp{}, domain.ErrNotFound
}
func (f *fakeCareStore) LatestClearance(context.Context, int64, time.Time) (domain.Clearance, error) {
	return domain.Clearance{}, domain.ErrNotFound
}

// TestClearanceGrantIsAtomicAcrossPermitAndBlockRelease reproduces the half-grant hazard:
// when granting a conditional clearance the professional's permit, the incident status change
// and the training-block release must all apply together. A storage failure mid-grant must
// leave no half-granted clearance behind, and a retry after recovery must finish in one call.
func TestClearanceGrantIsAtomicAcrossPermitAndBlockRelease(t *testing.T) {
	professional := domain.User{ID: 7, Role: domain.RoleHealthProfessional}
	actor := domain.Actor{UserID: professional.ID, Role: professional.Role, RequestID: "clearance"}
	now := serviceTime.Add(8 * 24 * time.Hour)
	validClearance := domain.Clearance{IncidentID: 3, FollowUpID: 11, Kind: domain.ClearanceConditional,
		Conditions: "non-contact training only", ValidFrom: now.Add(time.Minute), ValidUntil: now.Add(7 * 24 * time.Hour)}

	t.Run("storage failure changes no state and retry completes in one call", func(t *testing.T) {
		fake := &fakeCareStore{failGrant: true}
		care := service.NewCare(fake, clock.Fixed{Time: now})

		if _, err := care.Clear(context.Background(), actor, validClearance); !errors.Is(err, domain.ErrUnavailable) {
			t.Fatalf("transient grant failure returned %v, want ErrUnavailable", err)
		}
		if fake.clearance.ID != 0 {
			t.Fatalf("half-grant left a clearance recorded after failure: %+v", fake.clearance)
		}
		if fake.releaseDone {
			t.Fatal("block was released despite the grant failing")
		}

		// Recovery: retry with the store healthy again. One call finishes the whole grant.
		fake.failGrant = false
		granted, err := care.Clear(context.Background(), actor, validClearance)
		if err != nil {
			t.Fatalf("retry Clear: %v", err)
		}
		if granted.ID == 0 || granted.Status != domain.ClearanceActive || !fake.releaseDone {
			t.Fatalf("retry did not complete the grant atomically: granted=%+v released=%v", granted, fake.releaseDone)
		}
		if fake.grantCalls != 2 {
			t.Fatalf("expected exactly one store call per attempt (grantCalls=%d), want 2", fake.grantCalls)
		}
	})

	t.Run("successful grant is a single atomic store call", func(t *testing.T) {
		fake := &fakeCareStore{}
		care := service.NewCare(fake, clock.Fixed{Time: now})

		granted, err := care.Clear(context.Background(), actor, validClearance)
		if err != nil {
			t.Fatalf("Clear: %v", err)
		}
		if granted.Status != domain.ClearanceActive || !fake.releaseDone {
			t.Fatalf("grant did not release the block: granted=%+v released=%v", granted, fake.releaseDone)
		}
		if fake.grantCalls != 1 {
			t.Fatalf("expected a single GrantClearance call (got %d); the split record/release path must not be used",
				fake.grantCalls)
		}
	})
}
