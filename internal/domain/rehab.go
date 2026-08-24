package domain

import (
	"fmt"
	"strings"
	"time"
)

type RehabPlan struct {
	ID             int64
	ReferralID     int64
	ProfessionalID int64
	CurrentVersion int64
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type RehabPlanVersion struct {
	ID           int64
	PlanID       int64
	Version      int64
	Goals        string
	Restrictions string
	Exercises    string
	ReviewDueAt  time.Time
	PublishedBy  int64
	PublishedAt  time.Time
}

func (v RehabPlanVersion) Validate(now time.Time) error {
	if strings.TrimSpace(v.Goals) == "" {
		return &FieldError{Field: "goals", Problem: "is required"}
	}
	if strings.TrimSpace(v.Restrictions) == "" {
		return &FieldError{Field: "restrictions", Problem: "is required"}
	}
	if strings.TrimSpace(v.Exercises) == "" {
		return &FieldError{Field: "exercises", Problem: "is required"}
	}
	if !v.ReviewDueAt.After(now) {
		return &FieldError{Field: "review_due_at", Problem: "must be in the future"}
	}
	return nil
}

func (p *RehabPlan) Publish(v *RehabPlanVersion, expectedVersion int64, now time.Time) error {
	if !p.Active {
		return fmt.Errorf("inactive rehabilitation plan: %w", ErrConflict)
	}
	if p.CurrentVersion != expectedVersion {
		return &ConflictError{Entity: "rehab_plan", Expected: expectedVersion, Actual: p.CurrentVersion}
	}
	if err := v.Validate(now); err != nil {
		return err
	}
	p.CurrentVersion++
	p.UpdatedAt = now
	v.PlanID = p.ID
	v.Version = p.CurrentVersion
	v.PublishedAt = now
	return nil
}

type FollowUp struct {
	ID             int64
	PlanID         int64
	PlanVersion    int64
	ProfessionalID int64
	PainScore      int
	MobilityScore  int
	LoadTolerance  int
	Notes          string
	AssessedAt     time.Time
	ValidUntil     time.Time
	CreatedAt      time.Time
}

func (f FollowUp) Validate() error {
	for name, value := range map[string]int{"pain_score": f.PainScore, "mobility_score": f.MobilityScore, "load_tolerance": f.LoadTolerance} {
		if value < 0 || value > 10 {
			return &FieldError{Field: name, Problem: "must be between 0 and 10"}
		}
	}
	if !f.ValidUntil.After(f.AssessedAt) {
		return &FieldError{Field: "valid_until", Problem: "must follow assessed_at"}
	}
	return nil
}

func (f FollowUp) EligibleForClearance(now time.Time) error {
	if !now.Before(f.ValidUntil) {
		return fmt.Errorf("follow-up expired at %s: %w", f.ValidUntil.Format(time.RFC3339), ErrExpired)
	}
	if f.PainScore > 4 || f.MobilityScore < 6 || f.LoadTolerance < 5 {
		return fmt.Errorf("follow-up thresholds not met: %w", ErrConflict)
	}
	return nil
}
