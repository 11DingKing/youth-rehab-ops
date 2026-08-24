package service

import (
	"context"
	"strings"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

type ScheduleService struct {
	repo repository.ScheduleStore
	now  clock.Clock
}

func NewSchedule(repo repository.ScheduleStore, now clock.Clock) *ScheduleService {
	return &ScheduleService{repo: repo, now: now}
}

func (s *ScheduleService) Attempt(ctx context.Context, actor domain.Actor, attempt domain.ScheduleAttempt) (domain.ScheduleAttempt, error) {
	if err := domain.RequireRole(actor.Role, domain.RoleCoach, domain.RoleSafetyOfficer); err != nil {
		return domain.ScheduleAttempt{}, err
	}
	if attempt.ParticipantID <= 0 || attempt.IncidentID <= 0 || !attempt.SessionStartsAt.After(s.now.Now()) {
		return domain.ScheduleAttempt{}, &domain.FieldError{Field: "schedule", Problem: "participant, incident and future start are required"}
	}
	if strings.TrimSpace(attempt.IdempotencyKey) == "" {
		return domain.ScheduleAttempt{}, &domain.FieldError{Field: "Idempotency-Key", Problem: "header is required"}
	}
	attempt.RequestedBy = actor.UserID
	attempt.CreatedAt = s.now.Now()
	persistCtx := context.WithoutCancel(ctx)
	return s.repo.AttemptSchedule(persistCtx, actor, attempt)
}

func (s *ScheduleService) Override(ctx context.Context, actor domain.Actor, incidentID int64, reason string, ttl time.Duration) (domain.Override, error) {
	if !actor.Role.CanOverride() {
		return domain.Override{}, domain.ErrForbidden
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		return domain.Override{}, &domain.FieldError{Field: "ttl", Problem: "must be between zero and 24 hours"}
	}
	now := s.now.Now()
	return s.repo.GrantOverride(ctx, actor, domain.Override{IncidentID: incidentID, GrantedBy: actor.UserID, Reason: strings.TrimSpace(reason),
		ExpiresAt: now.Add(ttl), CreatedAt: now})
}
