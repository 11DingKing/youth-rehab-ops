package domain

import (
	"fmt"
	"strings"
	"time"
)

type ReferralStatus string

const (
	ReferralRequested ReferralStatus = "requested"
	ReferralAccepted  ReferralStatus = "accepted"
	ReferralReturned  ReferralStatus = "returned"
	ReferralCompleted ReferralStatus = "completed"
)

type Referral struct {
	ID             int64
	IncidentID     int64
	Organization   string
	Reason         string
	Status         ReferralStatus
	ReturnedReason string
	ProfessionalID *int64
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (r Referral) Validate() error {
	if r.IncidentID <= 0 {
		return &FieldError{Field: "incident_id", Problem: "must be positive"}
	}
	if strings.TrimSpace(r.Organization) == "" {
		return &FieldError{Field: "organization", Problem: "is required"}
	}
	if strings.TrimSpace(r.Reason) == "" {
		return &FieldError{Field: "reason", Problem: "is required"}
	}
	return nil
}

func (r *Referral) Accept(professionalID int64, now time.Time) error {
	if r.Status != ReferralRequested {
		return fmt.Errorf("accept referral in %s: %w", r.Status, ErrConflict)
	}
	r.Status = ReferralAccepted
	r.ProfessionalID = &professionalID
	r.Version++
	r.UpdatedAt = now
	return nil
}

func (r *Referral) Return(reason string, now time.Time) error {
	if r.Status != ReferralRequested && r.Status != ReferralAccepted {
		return fmt.Errorf("return referral in %s: %w", r.Status, ErrConflict)
	}
	if strings.TrimSpace(reason) == "" {
		return &FieldError{Field: "returned_reason", Problem: "is required"}
	}
	r.Status = ReferralReturned
	r.ReturnedReason = strings.TrimSpace(reason)
	r.Version++
	r.UpdatedAt = now
	return nil
}

func (r *Referral) Complete(now time.Time) error {
	if r.Status != ReferralAccepted {
		return fmt.Errorf("complete referral in %s: %w", r.Status, ErrConflict)
	}
	r.Status = ReferralCompleted
	r.Version++
	r.UpdatedAt = now
	return nil
}
