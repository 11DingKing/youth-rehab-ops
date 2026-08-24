package domain

import (
	"fmt"
	"strings"
	"time"
)

type IncidentStatus string

const (
	IncidentReported    IncidentStatus = "reported"
	IncidentTriaged     IncidentStatus = "triaged"
	IncidentReferred    IncidentStatus = "referred"
	IncidentRehabActive IncidentStatus = "rehab_active"
	IncidentReviewDue   IncidentStatus = "review_due"
	IncidentCleared     IncidentStatus = "cleared"
	IncidentClosed      IncidentStatus = "closed"
)

type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityModerate Severity = "moderate"
	SeverityHigh     Severity = "high"
	SeverityUrgent   Severity = "urgent"
)

type InjuryKind string

const (
	InjuryStrain       InjuryKind = "strain"
	InjurySprain       InjuryKind = "sprain"
	InjuryOveruse      InjuryKind = "overuse"
	InjuryUndetermined InjuryKind = "undetermined"
)

type Incident struct {
	ID             int64
	PublicID       string
	ParticipantID  int64
	ReporterUserID int64
	Kind           InjuryKind
	BodyArea       string
	OccurredAt     time.Time
	Description    string
	Status         IncidentStatus
	Severity       Severity
	StopTraining   bool
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type IncidentRevision struct {
	ID          int64
	IncidentID  int64
	Revision    int64
	BodyArea    string
	OccurredAt  time.Time
	Description string
	Reason      string
	CorrectedBy int64
	CreatedAt   time.Time
}

func (i Incident) ValidateReport(now time.Time) error {
	if i.ParticipantID <= 0 {
		return &FieldError{Field: "participant_id", Problem: "must be positive"}
	}
	if !i.Kind.Valid() {
		return &FieldError{Field: "kind", Problem: "unsupported injury kind"}
	}
	if strings.TrimSpace(i.BodyArea) == "" {
		return &FieldError{Field: "body_area", Problem: "is required"}
	}
	if strings.TrimSpace(i.Description) == "" {
		return &FieldError{Field: "description", Problem: "is required"}
	}
	if i.OccurredAt.IsZero() || i.OccurredAt.After(now.Add(5*time.Minute)) {
		return &FieldError{Field: "occurred_at", Problem: "must be a real time not in the future"}
	}
	return nil
}

func (k InjuryKind) Valid() bool {
	switch k {
	case InjuryStrain, InjurySprain, InjuryOveruse, InjuryUndetermined:
		return true
	default:
		return false
	}
}

func (s Severity) Valid() bool {
	switch s {
	case SeverityLow, SeverityModerate, SeverityHigh, SeverityUrgent:
		return true
	default:
		return false
	}
}

func (i Incident) CanTransition(to IncidentStatus) bool {
	allowed := map[IncidentStatus][]IncidentStatus{
		IncidentReported:    {IncidentTriaged},
		IncidentTriaged:     {IncidentReferred, IncidentReviewDue, IncidentClosed},
		IncidentReferred:    {IncidentRehabActive, IncidentTriaged},
		IncidentRehabActive: {IncidentReviewDue, IncidentTriaged},
		IncidentReviewDue:   {IncidentCleared, IncidentRehabActive, IncidentTriaged},
		IncidentCleared:     {IncidentClosed, IncidentReviewDue},
	}
	for _, candidate := range allowed[i.Status] {
		if candidate == to {
			return true
		}
	}
	return false
}

func (i *Incident) Transition(to IncidentStatus, now time.Time) error {
	if !i.CanTransition(to) {
		return fmt.Errorf("incident %s cannot transition from %s to %s: %w", i.PublicID, i.Status, to, ErrConflict)
	}
	i.Status = to
	i.Version++
	i.UpdatedAt = now
	return nil
}

func (i Incident) NeedsGuardianNotice() bool {
	return i.StopTraining || i.Severity == SeverityHigh || i.Severity == SeverityUrgent
}
