package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

type cancelledReferralRepo struct{ repository.CareStore }

func (cancelledReferralRepo) CreateReferral(ctx context.Context, _ domain.Actor, _ domain.Referral) (domain.Referral, error) {
	if err := ctx.Err(); err != nil {
		return domain.Referral{}, err
	}
	return domain.Referral{ID: 1, Status: domain.ReferralRequested}, nil
}

func TestCancelledReferralDoesNotPersist(t *testing.T) {
	fixed := clock.Fixed{Time: time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)}
	s := service.NewCare(cancelledReferralRepo{}, fixed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	referral, err := s.Refer(ctx, domain.Actor{UserID: 6, Role: domain.RoleSafetyOfficer}, 3, "Youth Clinic", "needs specialist review")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled referral returned referral=%+v err=%v", referral, err)
	}
	if referral.ID != 0 {
		t.Fatalf("canceled referral reached persistence: %+v", referral)
	}
}
