package service

import (
	"context"
	"strings"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

type CareService struct {
	repo repository.CareStore
	now  clock.Clock
}

func NewCare(repo repository.CareStore, now clock.Clock) *CareService {
	return &CareService{repo: repo, now: now}
}

func (s *CareService) Refer(ctx context.Context, actor domain.Actor, incidentID int64, organization, reason string) (domain.Referral, error) {
	if !actor.Role.CanTriage() {
		return domain.Referral{}, domain.ErrForbidden
	}
	now := s.now.Now()
	referral := domain.Referral{IncidentID: incidentID, Organization: strings.TrimSpace(organization), Reason: strings.TrimSpace(reason),
		Status: domain.ReferralRequested, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := referral.Validate(); err != nil {
		return domain.Referral{}, err
	}
	return s.repo.CreateReferral(ctx, actor, referral)
}

func (s *CareService) AcceptReferral(ctx context.Context, actor domain.Actor, id, expected int64) (domain.Referral, error) {
	if !actor.Role.CanManageClinicalCare() {
		return domain.Referral{}, domain.ErrForbidden
	}
	return s.repo.AcceptReferral(ctx, actor, id, expected)
}

func (s *CareService) ReturnReferral(ctx context.Context, actor domain.Actor, id, expected int64, reason string) (domain.Referral, error) {
	if !actor.Role.CanManageClinicalCare() {
		return domain.Referral{}, domain.ErrForbidden
	}
	return s.repo.ReturnReferral(ctx, actor, id, expected, strings.TrimSpace(reason))
}

func (s *CareService) CreatePlan(ctx context.Context, actor domain.Actor, referralID int64) (domain.RehabPlan, error) {
	if !actor.Role.CanManageClinicalCare() {
		return domain.RehabPlan{}, domain.ErrForbidden
	}
	now := s.now.Now()
	return s.repo.CreatePlan(ctx, actor, domain.RehabPlan{ReferralID: referralID, ProfessionalID: actor.UserID, Active: true, CreatedAt: now, UpdatedAt: now})
}

func (s *CareService) PublishPlan(ctx context.Context, actor domain.Actor, planID, expected int64, goals, restrictions, exercises string, due time.Time) (domain.RehabPlanVersion, error) {
	if !actor.Role.CanManageClinicalCare() {
		return domain.RehabPlanVersion{}, domain.ErrForbidden
	}
	version := domain.RehabPlanVersion{Goals: strings.TrimSpace(goals), Restrictions: strings.TrimSpace(restrictions), Exercises: strings.TrimSpace(exercises),
		ReviewDueAt: due.UTC(), PublishedBy: actor.UserID, PublishedAt: s.now.Now()}
	return s.repo.PublishPlanVersion(ctx, actor, planID, expected, version)
}

func (s *CareService) FollowUp(ctx context.Context, actor domain.Actor, follow domain.FollowUp) (domain.FollowUp, error) {
	if !actor.Role.CanManageClinicalCare() {
		return domain.FollowUp{}, domain.ErrForbidden
	}
	follow.ProfessionalID = actor.UserID
	follow.CreatedAt = s.now.Now()
	return s.repo.RecordFollowUp(ctx, actor, follow)
}

func (s *CareService) Clear(ctx context.Context, actor domain.Actor, clearance domain.Clearance) (domain.Clearance, error) {
	if !actor.Role.CanManageClinicalCare() {
		return domain.Clearance{}, domain.ErrForbidden
	}
	now := s.now.Now()
	clearance.ProfessionalID = actor.UserID
	clearance.Status = domain.ClearanceActive
	clearance.Version = 1
	clearance.CreatedAt = now
	clearance.UpdatedAt = now
	if err := clearance.Validate(now); err != nil {
		return domain.Clearance{}, err
	}
	operationCtx := context.WithoutCancel(ctx)
	return s.repo.GrantClearance(operationCtx, actor, clearance)
}
