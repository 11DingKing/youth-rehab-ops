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

type cancelledScheduleRepo struct{ repository.ScheduleStore }

func (cancelledScheduleRepo) AttemptSchedule(ctx context.Context, _ domain.Actor, _ domain.ScheduleAttempt) (domain.ScheduleAttempt, error) {
	if err := ctx.Err(); err != nil {
		return domain.ScheduleAttempt{}, err
	}
	return domain.ScheduleAttempt{ID: 1, Allowed: true}, nil
}

func TestCancelledScheduleDoesNotReachPersistence(t *testing.T) {
	fixed := clock.Fixed{Time: time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)}
	s := service.NewSchedule(cancelledScheduleRepo{}, fixed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempt, err := s.Attempt(ctx, domain.Actor{UserID: 7, Role: domain.RoleCoach}, domain.ScheduleAttempt{
		ParticipantID: 11, IncidentID: 22, SessionStartsAt: fixed.Time.Add(time.Hour), IdempotencyKey: "yr008-cancelled",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled schedule returned attempt=%+v err=%v", attempt, err)
	}
	if attempt.ID != 0 {
		t.Fatalf("canceled schedule reached persistence: %+v", attempt)
	}
}
