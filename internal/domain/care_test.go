package domain

import (
	"errors"
	"testing"
	"time"
)

func TestReferralLifecyclePreservesOwnerAndVersions(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	referral := Referral{ID: 4, IncidentID: 9, Organization: "Youth Sports Clinic", Reason: "persistent swelling",
		Status: ReferralRequested, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := referral.Validate(); err != nil {
		t.Fatalf("valid referral rejected: %v", err)
	}
	if err := referral.Accept(42, now.Add(time.Hour)); err != nil {
		t.Fatalf("accept failed: %v", err)
	}
	if referral.Status != ReferralAccepted || referral.ProfessionalID == nil || *referral.ProfessionalID != 42 || referral.Version != 2 {
		t.Fatalf("accepted referral mismatch: %+v", referral)
	}
	if err := referral.Complete(now.Add(2 * time.Hour)); err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if referral.Status != ReferralCompleted || referral.Version != 3 {
		t.Fatalf("completed referral mismatch: %+v", referral)
	}
	if err := referral.Return("late return", now.Add(3*time.Hour)); !errors.Is(err, ErrConflict) {
		t.Fatalf("completed referral return should conflict, got %v", err)
	}
}

func TestReferralReturnRequiresReasonAndAllowedState(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []ReferralStatus{ReferralRequested, ReferralAccepted} {
		referral := Referral{Status: status, Version: 2}
		if err := referral.Return("missing consent document", now); err != nil {
			t.Fatalf("return from %s failed: %v", status, err)
		}
		if referral.Status != ReferralReturned || referral.ReturnedReason == "" || referral.Version != 3 {
			t.Fatalf("return mutation incomplete: %+v", referral)
		}
	}
	referral := Referral{Status: ReferralRequested}
	if err := referral.Return(" ", now); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank return reason should fail validation, got %v", err)
	}
}

func TestPlanPublishingCreatesImmutableSequentialVersion(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	plan := RehabPlan{ID: 8, CurrentVersion: 2, Active: true, UpdatedAt: now}
	version := RehabPlanVersion{Goals: "walk without pain", Restrictions: "no jumping", Exercises: "controlled calf raises", ReviewDueAt: now.Add(7 * 24 * time.Hour)}
	if err := plan.Publish(&version, 2, now.Add(time.Hour)); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if plan.CurrentVersion != 3 || version.PlanID != plan.ID || version.Version != 3 || version.PublishedAt.IsZero() {
		t.Fatalf("published values mismatch: plan=%+v version=%+v", plan, version)
	}
	stale := RehabPlanVersion{Goals: "x", Restrictions: "y", Exercises: "z", ReviewDueAt: now.Add(10 * 24 * time.Hour)}
	if err := plan.Publish(&stale, 2, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale plan publication should conflict, got %v", err)
	}
	if stale.Version != 0 {
		t.Fatal("stale publication should not assign a version")
	}
}

func TestPlanVersionRejectsIncompleteOrExpiredInstructions(t *testing.T) {
	now := time.Now().UTC()
	base := RehabPlanVersion{Goals: "goal", Restrictions: "restriction", Exercises: "exercise", ReviewDueAt: now.Add(time.Hour)}
	tests := []struct {
		name   string
		mutate func(*RehabPlanVersion)
	}{
		{"goals", func(v *RehabPlanVersion) { v.Goals = "" }},
		{"restrictions", func(v *RehabPlanVersion) { v.Restrictions = " " }},
		{"exercises", func(v *RehabPlanVersion) { v.Exercises = "" }},
		{"review due", func(v *RehabPlanVersion) { v.ReviewDueAt = now }},
	}
	for _, test := range tests {
		candidate := base
		test.mutate(&candidate)
		if err := candidate.Validate(now); !errors.Is(err, ErrValidation) {
			t.Errorf("%s should fail validation, got %v", test.name, err)
		}
	}
}

func TestFollowUpEligibilityUsesValidityAndAllThresholds(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	eligible := FollowUp{PainScore: 4, MobilityScore: 6, LoadTolerance: 5, AssessedAt: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour)}
	if err := eligible.Validate(); err != nil {
		t.Fatalf("valid follow-up rejected: %v", err)
	}
	if err := eligible.EligibleForClearance(now); err != nil {
		t.Fatalf("threshold follow-up rejected: %v", err)
	}
	for _, mutate := range []func(*FollowUp){
		func(f *FollowUp) { f.PainScore = 5 },
		func(f *FollowUp) { f.MobilityScore = 5 },
		func(f *FollowUp) { f.LoadTolerance = 4 },
	} {
		candidate := eligible
		mutate(&candidate)
		if err := candidate.EligibleForClearance(now); !errors.Is(err, ErrConflict) {
			t.Fatalf("failed threshold should conflict, got %v", err)
		}
	}
	expired := eligible
	expired.ValidUntil = now
	if err := expired.EligibleForClearance(now); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired follow-up should be expired, got %v", err)
	}
}

func TestConditionalAndFullClearanceRules(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	conditional := Clearance{Kind: ClearanceConditional, Conditions: "non-contact drills only", Status: ClearanceActive,
		ValidFrom: now.Add(time.Minute), ValidUntil: now.Add(24 * time.Hour)}
	if err := conditional.Validate(now); err != nil {
		t.Fatalf("conditional clearance invalid: %v", err)
	}
	training := now.Add(time.Hour)
	if err := conditional.AllowsTraining(training, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("unacknowledged conditions should conflict, got %v", err)
	}
	if err := conditional.AllowsTraining(training, true); err != nil {
		t.Fatalf("acknowledged conditions rejected: %v", err)
	}
	full := conditional
	full.Kind = ClearanceFull
	full.Conditions = ""
	if err := full.AllowsTraining(training, false); err != nil {
		t.Fatalf("full clearance should not require acknowledgement: %v", err)
	}
}

func TestClearanceRejectsInactiveAndOutOfWindowUse(t *testing.T) {
	now := time.Now().UTC()
	base := Clearance{Kind: ClearanceFull, Status: ClearanceActive, ValidFrom: now, ValidUntil: now.Add(time.Hour)}
	for _, test := range []struct {
		name string
		at   time.Time
		set  func(*Clearance)
		err  error
	}{
		{"before", now.Add(-time.Second), func(*Clearance) {}, ErrExpired},
		{"at expiry", now.Add(time.Hour), func(*Clearance) {}, ErrExpired},
		{"revoked", now.Add(time.Minute), func(c *Clearance) { c.Status = ClearanceRevoked }, ErrConflict},
		{"expired state", now.Add(time.Minute), func(c *Clearance) { c.Status = ClearanceExpired }, ErrConflict},
	} {
		candidate := base
		test.set(&candidate)
		if err := candidate.AllowsTraining(test.at, true); !errors.Is(err, test.err) {
			t.Errorf("%s: got %v want %v", test.name, err, test.err)
		}
	}
}

func TestSessionAndOverrideExpirationAreExclusive(t *testing.T) {
	now := time.Now().UTC()
	session := Session{ExpiresAt: now.Add(time.Minute)}
	if !session.Usable(now) || session.Usable(session.ExpiresAt) {
		t.Fatal("session expiry boundary is incorrect")
	}
	revoked := now.Add(-time.Second)
	session.RevokedAt = &revoked
	if session.Usable(now) {
		t.Fatal("revoked session remained usable")
	}
	override := Override{ExpiresAt: now.Add(time.Minute)}
	if !override.Active(now) || override.Active(override.ExpiresAt) {
		t.Fatal("override expiry boundary is incorrect")
	}
}
