package sqlite

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

func TestCanceledClearanceRequestLeavesWorkflowBlocked(t *testing.T) {
	fixture := newWorkflowFixture(t)
	actor := domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "canceled-clearance"}
	_, err := fixture.store.TriageIncident(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "setup-triage"}, fixture.incident.ID,
		repository.TriageInput{Severity: domain.SeverityModerate, StopTraining: true, PublicGuidance: "professional review", Expected: 1}, 3)
	if err != nil {
		t.Fatal(err)
	}
	referral, err := fixture.store.CreateReferral(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "setup-referral"},
		domain.Referral{IncidentID: fixture.incident.ID, Organization: "Youth Clinic", Reason: "functional review", Status: domain.ReferralRequested,
			Version: 1, CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.store.AcceptReferral(context.Background(), actor, referral.ID, 1); err != nil {
		t.Fatal(err)
	}
	plan, err := fixture.store.CreatePlan(context.Background(), actor, domain.RehabPlan{ReferralID: referral.ID, ProfessionalID: fixture.professional.ID,
		Active: true, CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.store.PublishPlanVersion(context.Background(), actor, plan.ID, 0, domain.RehabPlanVersion{Goals: "return safely",
		Restrictions: "no contact", Exercises: "progressive loading", ReviewDueAt: testTime.Add(48 * time.Hour), PublishedAt: testTime}); err != nil {
		t.Fatal(err)
	}
	follow, err := fixture.store.RecordFollowUp(context.Background(), actor, domain.FollowUp{PlanID: plan.ID, PlanVersion: 1,
		PainScore: 2, MobilityScore: 8, LoadTolerance: 8, AssessedAt: testTime.Add(time.Hour), ValidUntil: testTime.Add(24 * time.Hour), CreatedAt: testTime.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	clearanceTime := testTime.Add(2 * time.Hour)
	care := service.NewCare(fixture.store, clock.Fixed{Time: clearanceTime})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = care.Clear(ctx, actor, domain.Clearance{IncidentID: fixture.incident.ID, FollowUpID: follow.ID, Kind: domain.ClearanceFull,
		ValidFrom: clearanceTime, ValidUntil: clearanceTime.Add(24 * time.Hour)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled clearance returned %v", err)
	}

	var clearances, activeBlocks int
	if queryErr := fixture.store.db.QueryRow(`SELECT COUNT(*) FROM clearances WHERE incident_id=?`, fixture.incident.ID).Scan(&clearances); queryErr != nil {
		t.Fatal(queryErr)
	}
	if queryErr := fixture.store.db.QueryRow(`SELECT COUNT(*) FROM training_blocks WHERE incident_id=? AND active=1`, fixture.incident.ID).Scan(&activeBlocks); queryErr != nil {
		t.Fatal(queryErr)
	}
	incident, getErr := fixture.store.GetIncident(context.Background(), fixture.incident.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if clearances != 0 || activeBlocks != 1 || incident.Status != domain.IncidentRehabActive {
		t.Fatalf("canceled request mutated workflow: clearances=%d active_blocks=%d incident=%s", clearances, activeBlocks, incident.Status)
	}

	granted, err := care.Clear(context.Background(), actor, domain.Clearance{IncidentID: fixture.incident.ID, FollowUpID: follow.ID, Kind: domain.ClearanceFull,
		ValidFrom: clearanceTime, ValidUntil: clearanceTime.Add(24 * time.Hour)})
	if err != nil || granted.ID == 0 {
		t.Fatalf("valid clearance after cancellation failed: clearance=%+v err=%v", granted, err)
	}
	incident, getErr = fixture.store.GetIncident(context.Background(), fixture.incident.ID)
	if getErr != nil || incident.Status != domain.IncidentCleared {
		t.Fatalf("valid clearance did not release workflow: incident=%+v err=%v", incident, getErr)
	}
}
