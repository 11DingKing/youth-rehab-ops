package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

func TestCancelledFollowUpDoesNotPersist(t *testing.T) {
	fixture := newServiceFixture(t)
	incident, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "yr007-report"), reportInput(fixture.participant.ID, "yr007"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.incidents.Triage(context.Background(), actor(fixture.officer, "yr007-triage"), incident.ID,
		service.TriageIncidentInput{Severity: domain.SeverityModerate, StopTraining: true, PublicGuidance: "clinical review", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	referral, err := fixture.care.Refer(context.Background(), actor(fixture.officer, "yr007-refer"), incident.ID, "Youth Clinic", "ankle review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.care.AcceptReferral(context.Background(), actor(fixture.professional, "yr007-accept"), referral.ID, referral.Version); err != nil {
		t.Fatal(err)
	}
	plan, err := fixture.care.CreatePlan(context.Background(), actor(fixture.professional, "yr007-plan"), referral.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.care.PublishPlan(context.Background(), actor(fixture.professional, "yr007-publish"), plan.ID, 0,
		"restore ankle control", "no sprinting", "balance drills", serviceTime.Add(7*24*60*60*1e9)); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	follow, err := fixture.care.FollowUp(cancelled, actor(fixture.professional, "yr007-cancelled"), domain.FollowUp{
		PlanID: plan.ID, PlanVersion: 1, PainScore: 2, MobilityScore: 8, LoadTolerance: 8,
		Notes: "request was canceled before persistence", AssessedAt: serviceTime.Add(8 * 24 * 60 * 60 * 1e9),
		ValidUntil: serviceTime.Add(15 * 24 * 60 * 60 * 1e9),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled follow-up returned follow=%+v err=%v", follow, err)
	}
	if follow.ID != 0 {
		t.Fatalf("canceled follow-up was persisted: %+v", follow)
	}
}
