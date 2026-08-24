package domain

import (
	"fmt"
	"strings"
	"time"
)

type ClearanceKind string

const (
	ClearanceConditional ClearanceKind = "conditional"
	ClearanceFull        ClearanceKind = "full"
)

type ClearanceStatus string

const (
	ClearanceActive  ClearanceStatus = "active"
	ClearanceExpired ClearanceStatus = "expired"
	ClearanceRevoked ClearanceStatus = "revoked"
)

type Clearance struct {
	ID             int64
	IncidentID     int64
	FollowUpID     int64
	ProfessionalID int64
	Kind           ClearanceKind
	Conditions     string
	Status         ClearanceStatus
	ValidFrom      time.Time
	ValidUntil     time.Time
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (c Clearance) Validate(now time.Time) error {
	if c.Kind != ClearanceConditional && c.Kind != ClearanceFull {
		return &FieldError{Field: "kind", Problem: "must be conditional or full"}
	}
	if c.Kind == ClearanceConditional && strings.TrimSpace(c.Conditions) == "" {
		return &FieldError{Field: "conditions", Problem: "required for conditional clearance"}
	}
	if c.ValidFrom.Before(now.Add(-time.Minute)) {
		return &FieldError{Field: "valid_from", Problem: "cannot be in the past"}
	}
	if !c.ValidUntil.After(c.ValidFrom) {
		return &FieldError{Field: "valid_until", Problem: "must follow valid_from"}
	}
	return nil
}

func (c Clearance) AllowsTraining(at time.Time, acknowledgedConditions bool) error {
	if c.Status != ClearanceActive {
		return fmt.Errorf("clearance is %s: %w", c.Status, ErrConflict)
	}
	if at.Before(c.ValidFrom) || !at.Before(c.ValidUntil) {
		return fmt.Errorf("clearance not valid at requested time: %w", ErrExpired)
	}
	if c.Kind == ClearanceConditional && !acknowledgedConditions {
		return fmt.Errorf("conditions require acknowledgement: %w", ErrConflict)
	}
	return nil
}

type TrainingBlock struct {
	ID            int64
	ParticipantID int64
	IncidentID    int64
	Reason        string
	Active        bool
	CreatedAt     time.Time
	ReleasedAt    *time.Time
}

type ScheduleAttempt struct {
	ID                     int64
	ParticipantID          int64
	IncidentID             int64
	RequestedBy            int64
	SessionStartsAt        time.Time
	ConditionsAcknowledged bool
	Allowed                bool
	DecisionCode           string
	IdempotencyKey         string
	CreatedAt              time.Time
}

type Override struct {
	ID             int64
	IncidentID     int64
	GrantedBy      int64
	Reason         string
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	NotificationID *int64
	CreatedAt      time.Time
}

type OverrideGrant struct {
	Override       Override
	MessageClass   string
	JobMaxAttempts int
}

func PrepareOverrideGrant(value Override) (OverrideGrant, error) {
	if strings.TrimSpace(value.Reason) == "" || !value.ExpiresAt.After(value.CreatedAt) {
		return OverrideGrant{}, &FieldError{Field: "override", Problem: "reason and future expiry are required"}
	}
	return OverrideGrant{Override: value, MessageClass: "manual_override", JobMaxAttempts: 5}, nil
}

func (o Override) Active(now time.Time) bool {
	return o.RevokedAt == nil && now.Before(o.ExpiresAt)
}
