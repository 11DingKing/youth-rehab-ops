package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

func TestFailedPlanVersionInsertPreservesRetryableHead(t *testing.T) {
	fixture := newWorkflowFixture(t)
	officer := domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "setup-triage"}
	professional := domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "publish-version"}
	if _, err := fixture.store.TriageIncident(context.Background(), officer, fixture.incident.ID,
		repository.TriageInput{Severity: domain.SeverityModerate, StopTraining: true, PublicGuidance: "clinical review", Expected: 1}, 3); err != nil {
		t.Fatal(err)
	}
	referral, err := fixture.store.CreateReferral(context.Background(), officer, domain.Referral{IncidentID: fixture.incident.ID,
		Organization: "Youth Clinic", Reason: "progressive recovery", Status: domain.ReferralRequested, Version: 1, CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.store.AcceptReferral(context.Background(), professional, referral.ID, 1); err != nil {
		t.Fatal(err)
	}
	plan, err := fixture.store.CreatePlan(context.Background(), professional, domain.RehabPlan{ReferralID: referral.ID,
		ProfessionalID: fixture.professional.ID, Active: true, CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.db.Exec(`CREATE TRIGGER reject_plan_version BEFORE INSERT ON rehab_plan_versions BEGIN SELECT RAISE(ABORT, 'version storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	version := domain.RehabPlanVersion{Goals: "pain-free movement", Restrictions: "no contact", Exercises: "graded loading",
		ReviewDueAt: testTime.Add(7 * 24 * time.Hour), PublishedBy: fixture.professional.ID, PublishedAt: testTime}
	if _, err = fixture.store.PublishPlanVersion(context.Background(), professional, plan.ID, 0, version); err == nil {
		t.Fatal("version storage failure was reported as success")
	}
	var currentVersion, historyCount int
	if err = fixture.store.db.QueryRow(`SELECT current_version FROM rehab_plans WHERE id=?`, plan.ID).Scan(&currentVersion); err != nil {
		t.Fatal(err)
	}
	if err = fixture.store.db.QueryRow(`SELECT COUNT(*) FROM rehab_plan_versions WHERE plan_id=?`, plan.ID).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if currentVersion != 0 || historyCount != 0 {
		t.Fatalf("failed publication left unusable head: current_version=%d history=%d", currentVersion, historyCount)
	}
	if _, err = fixture.store.db.Exec(`DROP TRIGGER reject_plan_version`); err != nil {
		t.Fatal(err)
	}
	published, err := fixture.store.PublishPlanVersion(context.Background(), professional, plan.ID, 0, version)
	if err != nil || published.Version != 1 {
		t.Fatalf("retry after storage recovery failed: version=%+v err=%v", published, err)
	}
}
