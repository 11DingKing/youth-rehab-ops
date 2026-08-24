package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

func TestReferralReturnFailureLeavesWorkflowUnchanged(t *testing.T) {
	fixture := newWorkflowFixture(t)
	ctx := context.Background()
	_, err := fixture.store.TriageIncident(ctx, domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "private-triage"},
		fixture.incident.ID, repository.TriageInput{Severity: domain.SeverityHigh, StopTraining: true,
			PublicGuidance: "seek specialist review", Expected: fixture.incident.Version}, 4)
	if err != nil {
		t.Fatalf("prepare triaged incident: %v", err)
	}
	referral, err := fixture.store.CreateReferral(ctx, domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "private-refer"},
		domain.Referral{IncidentID: fixture.incident.ID, Organization: "Regional Youth Clinic", Reason: "specialist assessment required",
			Status: domain.ReferralRequested, Version: 1, CreatedAt: testTime.Add(time.Minute), UpdatedAt: testTime.Add(time.Minute)})
	if err != nil {
		t.Fatalf("prepare referral: %v", err)
	}
	accepted, err := fixture.store.AcceptReferral(ctx, domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "private-accept"}, referral.ID, referral.Version)
	if err != nil {
		t.Fatalf("prepare accepted referral: %v", err)
	}

	_, err = fixture.store.db.Exec(`CREATE TRIGGER reject_incident_reopen BEFORE UPDATE OF status ON incidents
		WHEN OLD.status='referred' AND NEW.status='triaged'
		BEGIN SELECT RAISE(ABORT, 'forced incident reopen failure'); END`)
	if err != nil {
		t.Fatalf("install persistence failure: %v", err)
	}
	care := service.NewCare(fixture.store, clock.Fixed{Time: testTime.Add(2 * time.Minute)})
	actor := domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "private-return"}
	if returned, err := care.ReturnReferral(ctx, actor, referral.ID, accepted.Version, "guardian authorization is incomplete"); err == nil {
		t.Fatalf("return unexpectedly succeeded: %+v", returned)
	}

	persistedReferral, err := fixture.store.GetReferral(ctx, referral.ID)
	if err != nil {
		t.Fatalf("read referral after failure: %v", err)
	}
	persistedIncident, err := fixture.store.GetIncident(ctx, fixture.incident.ID)
	if err != nil {
		t.Fatalf("read incident after failure: %v", err)
	}
	if persistedReferral.Status != domain.ReferralAccepted || persistedReferral.Version != accepted.Version {
		t.Fatalf("failed return changed referral: %+v", persistedReferral)
	}
	if persistedIncident.Status != domain.IncidentReferred {
		t.Fatalf("failed return changed incident: %+v", persistedIncident)
	}

	if _, err := fixture.store.db.Exec(`DROP TRIGGER reject_incident_reopen`); err != nil {
		t.Fatalf("remove persistence failure: %v", err)
	}
	returned, err := care.ReturnReferral(ctx, actor, referral.ID, accepted.Version, "guardian authorization is incomplete")
	if err != nil {
		t.Fatalf("return after recovery: %v", err)
	}
	incidentAfterRecovery, err := fixture.store.GetIncident(ctx, fixture.incident.ID)
	if err != nil {
		t.Fatalf("read recovered incident: %v", err)
	}
	if returned.Status != domain.ReferralReturned || incidentAfterRecovery.Status != domain.IncidentTriaged {
		t.Fatalf("valid return did not complete: referral=%+v incident=%+v", returned, incidentAfterRecovery)
	}
}
