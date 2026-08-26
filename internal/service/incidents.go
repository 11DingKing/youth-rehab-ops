package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/security"
)

type IncidentService struct {
	repo         repository.IncidentStore
	participants repository.Participants
	now          clock.Clock
	maxAttempts  int
}

type ReportIncidentInput struct {
	ParticipantID  int64             `json:"participant_id"`
	Kind           domain.InjuryKind `json:"kind"`
	BodyArea       string            `json:"body_area"`
	OccurredAt     time.Time         `json:"occurred_at"`
	Description    string            `json:"description"`
	IdempotencyKey string            `json:"-"`
}

type TriageIncidentInput struct {
	Severity        domain.Severity `json:"severity"`
	StopTraining    bool            `json:"stop_training"`
	PublicGuidance  string          `json:"public_guidance"`
	ClinicalNotes   string          `json:"clinical_notes"`
	ExpectedVersion int64           `json:"expected_version"`
}

type IncidentView struct {
	ID            int64                 `json:"id"`
	PublicID      string                `json:"public_id"`
	ParticipantID int64                 `json:"participant_id"`
	Kind          domain.InjuryKind     `json:"kind"`
	BodyArea      string                `json:"body_area"`
	OccurredAt    time.Time             `json:"occurred_at"`
	Description   string                `json:"description"`
	Status        domain.IncidentStatus `json:"status"`
	Severity      domain.Severity       `json:"severity"`
	StopTraining  bool                  `json:"stop_training"`
	Version       int64                 `json:"version"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

func NewIncidents(repo repository.IncidentStore, participants repository.Participants, now clock.Clock, maxAttempts int) *IncidentService {
	return &IncidentService{repo: repo, participants: participants, now: now, maxAttempts: maxAttempts}
}

func (s *IncidentService) Report(ctx context.Context, actor domain.Actor, input ReportIncidentInput) (IncidentView, error) {
	if !actor.Role.CanReportIncident() {
		return IncidentView{}, domain.ErrForbidden
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return IncidentView{}, &domain.FieldError{Field: "Idempotency-Key", Problem: "header is required"}
	}
	participant, err := s.participants.GetParticipant(ctx, input.ParticipantID)
	if err != nil {
		return IncidentView{}, err
	}
	if !participant.Active {
		return IncidentView{}, fmt.Errorf("participant is inactive: %w", domain.ErrConflict)
	}
	publicID, err := security.NewPublicID("inc")
	if err != nil {
		return IncidentView{}, err
	}
	now := s.now.Now()
	incident := domain.Incident{PublicID: publicID, ParticipantID: input.ParticipantID, ReporterUserID: actor.UserID,
		Kind: input.Kind, BodyArea: strings.TrimSpace(input.BodyArea), OccurredAt: input.OccurredAt.UTC(),
		Description: strings.TrimSpace(input.Description), Status: domain.IncidentReported, Severity: domain.SeverityLow,
		StopTraining: true, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := incident.ValidateReport(now); err != nil {
		return IncidentView{}, err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", input.ParticipantID, input.Kind, input.BodyArea, input.OccurredAt.UTC())))
	created, err := s.repo.CreateIncident(ctx, actor, incident, input.IdempotencyKey, hex.EncodeToString(digest[:]))
	if err != nil {
		return IncidentView{}, err
	}
	return projectIncident(created), nil
}

func (s *IncidentService) Get(ctx context.Context, actor domain.Actor, id int64) (IncidentView, error) {
	incident, err := s.repo.GetIncident(ctx, id)
	if err != nil {
		return IncidentView{}, err
	}
	participant, err := s.participants.GetParticipant(ctx, incident.ParticipantID)
	if err != nil {
		return IncidentView{}, err
	}
	if actor.Role == domain.RoleGuardian && participant.GuardianUserID != actor.UserID {
		return IncidentView{}, domain.ErrForbidden
	}
	return projectIncident(incident), nil
}

func (s *IncidentService) Correct(ctx context.Context, actor domain.Actor, id int64, input repository.IncidentCorrection) (IncidentView, error) {
	if err := domain.RequireRole(actor.Role, domain.RoleCoach, domain.RoleSafetyOfficer); err != nil {
		return IncidentView{}, err
	}
	if strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.BodyArea) == "" || strings.TrimSpace(input.Description) == "" {
		return IncidentView{}, &domain.FieldError{Field: "correction", Problem: "reason, body area and description are required"}
	}
	updated, err := s.repo.CorrectIncident(ctx, actor, id, input)
	if err != nil {
		return IncidentView{}, err
	}
	return projectIncident(updated), nil
}

func (s *IncidentService) Triage(ctx context.Context, actor domain.Actor, id int64, input TriageIncidentInput) (IncidentView, error) {
	if !actor.Role.CanTriage() {
		return IncidentView{}, domain.ErrForbidden
	}
	if !input.Severity.Valid() || strings.TrimSpace(input.PublicGuidance) == "" {
		return IncidentView{}, &domain.FieldError{Field: "triage", Problem: "valid severity and public guidance are required"}
	}
	persistCtx := context.WithoutCancel(ctx)
	updated, err := s.repo.TriageIncident(persistCtx, actor, id, repository.TriageInput{Severity: input.Severity, StopTraining: input.StopTraining,
		PublicGuidance: strings.TrimSpace(input.PublicGuidance), ClinicalNotes: strings.TrimSpace(input.ClinicalNotes), Expected: input.ExpectedVersion}, s.maxAttempts)
	if err != nil {
		return IncidentView{}, err
	}
	return projectIncident(updated), nil
}

func projectIncident(incident domain.Incident) IncidentView {
	return IncidentView{ID: incident.ID, PublicID: incident.PublicID, ParticipantID: incident.ParticipantID, Kind: incident.Kind,
		BodyArea: incident.BodyArea, OccurredAt: incident.OccurredAt, Description: incident.Description, Status: incident.Status,
		Severity: incident.Severity, StopTraining: incident.StopTraining, Version: incident.Version, UpdatedAt: incident.UpdatedAt}
}
