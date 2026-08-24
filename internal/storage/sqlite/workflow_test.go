package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

type workflowFixture struct {
	store        *Store
	guardian     domain.User
	coach        domain.User
	officer      domain.User
	professional domain.User
	participant  domain.Participant
	incident     domain.Incident
}

func newWorkflowFixture(t *testing.T) workflowFixture {
	t.Helper()
	store := openTestStore(t)
	guardian := createUser(t, store, "workflow-guardian@example.test", domain.RoleGuardian)
	coach := createUser(t, store, "workflow-coach@example.test", domain.RoleCoach)
	officer := createUser(t, store, "workflow-officer@example.test", domain.RoleSafetyOfficer)
	professional := createUser(t, store, "workflow-professional@example.test", domain.RoleHealthProfessional)
	participant := createParticipant(t, store, guardian)
	incident := createIncident(t, store, coach, participant)
	return workflowFixture{store: store, guardian: guardian, coach: coach, officer: officer, professional: professional,
		participant: participant, incident: incident}
}

func TestScheduleIsBlockedBeforeProfessionalClearance(t *testing.T) {
	fixture := newWorkflowFixture(t)
	attempt, err := fixture.store.AttemptSchedule(context.Background(), domain.Actor{UserID: fixture.coach.ID, Role: fixture.coach.Role, RequestID: "schedule-blocked"},
		domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: fixture.incident.ID, RequestedBy: fixture.coach.ID,
			SessionStartsAt: testTime.Add(2 * time.Hour), IdempotencyKey: "schedule-before-clearance", CreatedAt: testTime})
	if err != nil {
		t.Fatalf("AttemptSchedule: %v", err)
	}
	if attempt.Allowed || attempt.DecisionCode != "active_training_block" {
		t.Fatalf("uncleared schedule was not blocked: %+v", attempt)
	}
	var auditResult, reason string
	if err := fixture.store.db.QueryRow(`SELECT result,reason FROM audit_events WHERE action='schedule.attempted' ORDER BY id DESC LIMIT 1`).Scan(&auditResult, &reason); err != nil {
		t.Fatalf("read schedule audit: %v", err)
	}
	if auditResult != "denied" || reason != "active_training_block" {
		t.Fatalf("audit result=%q reason=%q", auditResult, reason)
	}
}

func TestManualOverrideAllowsOnlyCoveredTrainingTime(t *testing.T) {
	fixture := newWorkflowFixture(t)
	now := time.Now().UTC()
	override, err := fixture.store.GrantOverride(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "override"},
		domain.Override{IncidentID: fixture.incident.ID, GrantedBy: fixture.officer.ID, Reason: "supervised mobility assessment",
			ExpiresAt: now.Add(90 * time.Minute), CreatedAt: now})
	if err != nil {
		t.Fatalf("GrantOverride: %v", err)
	}
	covered, err := fixture.store.AttemptSchedule(context.Background(), domain.Actor{UserID: fixture.coach.ID, Role: fixture.coach.Role, RequestID: "covered"},
		domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: fixture.incident.ID, SessionStartsAt: now.Add(time.Hour),
			IdempotencyKey: "covered-by-override", CreatedAt: now})
	if err != nil {
		t.Fatalf("covered AttemptSchedule: %v", err)
	}
	if !covered.Allowed || covered.DecisionCode != "manual_override" {
		t.Fatalf("covered session rejected: %+v", covered)
	}
	afterExpiry, err := fixture.store.AttemptSchedule(context.Background(), domain.Actor{UserID: fixture.coach.ID, Role: fixture.coach.Role, RequestID: "expired"},
		domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: fixture.incident.ID, SessionStartsAt: now.Add(2 * time.Hour),
			IdempotencyKey: "after-override-expiry", CreatedAt: now})
	if err != nil {
		t.Fatalf("expired AttemptSchedule: %v", err)
	}
	if afterExpiry.Allowed || afterExpiry.DecisionCode != "active_training_block" {
		t.Fatalf("expired override allowed session: %+v", afterExpiry)
	}
	if err := fixture.store.RevokeOverride(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "revoke"}, override.ID, "assessment canceled"); err != nil {
		t.Fatalf("RevokeOverride: %v", err)
	}
	revoked, err := fixture.store.AttemptSchedule(context.Background(), domain.Actor{UserID: fixture.coach.ID, Role: fixture.coach.Role, RequestID: "revoked"},
		domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: fixture.incident.ID, SessionStartsAt: now.Add(time.Hour),
			IdempotencyKey: "revoked-override", CreatedAt: now})
	if err != nil {
		t.Fatalf("revoked AttemptSchedule: %v", err)
	}
	if revoked.Allowed {
		t.Fatalf("revoked override allowed session: %+v", revoked)
	}
}

func TestFullCareWorkflowReleasesBlockAndAllowsSchedule(t *testing.T) {
	fixture := newWorkflowFixture(t)
	triaged, err := fixture.store.TriageIncident(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "triage"},
		fixture.incident.ID, repository.TriageInput{Severity: domain.SeverityModerate, StopTraining: true, PublicGuidance: "professional assessment",
			ClinicalNotes: "limited dorsiflexion", Expected: 1}, 4)
	if err != nil || triaged.Status != domain.IncidentTriaged {
		t.Fatalf("TriageIncident=%+v err=%v", triaged, err)
	}
	referral, err := fixture.store.CreateReferral(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "refer"},
		domain.Referral{IncidentID: fixture.incident.ID, Organization: "Youth Sport Medicine", Reason: "persistent functional limit",
			Status: domain.ReferralRequested, Version: 1, CreatedAt: testTime.Add(time.Minute), UpdatedAt: testTime.Add(time.Minute)})
	if err != nil {
		t.Fatalf("CreateReferral: %v", err)
	}
	accepted, err := fixture.store.AcceptReferral(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "accept"}, referral.ID, 1)
	if err != nil || accepted.Status != domain.ReferralAccepted {
		t.Fatalf("AcceptReferral=%+v err=%v", accepted, err)
	}
	plan, err := fixture.store.CreatePlan(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "plan"},
		domain.RehabPlan{ReferralID: referral.ID, ProfessionalID: fixture.professional.ID, Active: true,
			CreatedAt: testTime.Add(2 * time.Minute), UpdatedAt: testTime.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	version, err := fixture.store.PublishPlanVersion(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "publish"},
		plan.ID, 0, domain.RehabPlanVersion{Goals: "restore pain-free movement", Restrictions: "no impact activity", Exercises: "balance and range work",
			ReviewDueAt: testTime.Add(7 * 24 * time.Hour), PublishedBy: fixture.professional.ID, PublishedAt: testTime.Add(3 * time.Minute)})
	if err != nil || version.Version != 1 {
		t.Fatalf("PublishPlanVersion=%+v err=%v", version, err)
	}
	follow, err := fixture.store.RecordFollowUp(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "followup"},
		domain.FollowUp{PlanID: plan.ID, PlanVersion: 1, ProfessionalID: fixture.professional.ID, PainScore: 2, MobilityScore: 8,
			LoadTolerance: 8, Notes: "tolerated staged loading", AssessedAt: testTime.Add(8 * 24 * time.Hour),
			ValidUntil: testTime.Add(15 * 24 * time.Hour), CreatedAt: testTime.Add(8 * 24 * time.Hour)})
	if err != nil {
		t.Fatalf("RecordFollowUp: %v", err)
	}
	clearanceTime := testTime.Add(8*24*time.Hour + time.Minute)
	clearance, err := fixture.store.GrantClearance(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "clear"},
		domain.Clearance{IncidentID: fixture.incident.ID, FollowUpID: follow.ID, ProfessionalID: fixture.professional.ID, Kind: domain.ClearanceConditional,
			Conditions: "non-contact training only", Status: domain.ClearanceActive, ValidFrom: clearanceTime.Add(time.Minute),
			ValidUntil: clearanceTime.Add(7 * 24 * time.Hour), Version: 1, CreatedAt: clearanceTime, UpdatedAt: clearanceTime})
	if err != nil {
		t.Fatalf("GrantClearance: %v", err)
	}
	var blocks int
	if err := fixture.store.db.QueryRow(`SELECT COUNT(*) FROM training_blocks WHERE incident_id=? AND active=1`, fixture.incident.ID).Scan(&blocks); err != nil || blocks != 0 {
		t.Fatalf("active blocks=%d err=%v", blocks, err)
	}
	withoutAck, err := fixture.store.AttemptSchedule(context.Background(), domain.Actor{UserID: fixture.coach.ID, Role: fixture.coach.Role, RequestID: "without-ack"},
		domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: fixture.incident.ID, SessionStartsAt: clearance.ValidFrom.Add(time.Hour),
			ConditionsAcknowledged: false, IdempotencyKey: "conditions-missing", CreatedAt: clearanceTime})
	if err != nil {
		t.Fatalf("AttemptSchedule without acknowledgement: %v", err)
	}
	if withoutAck.Allowed || withoutAck.DecisionCode != "clearance_conditions_not_met" {
		t.Fatalf("conditions were not enforced: %+v", withoutAck)
	}
	allowed, err := fixture.store.AttemptSchedule(context.Background(), domain.Actor{UserID: fixture.coach.ID, Role: fixture.coach.Role, RequestID: "acknowledged"},
		domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: fixture.incident.ID, SessionStartsAt: clearance.ValidFrom.Add(time.Hour),
			ConditionsAcknowledged: true, IdempotencyKey: "conditions-acknowledged", CreatedAt: clearanceTime})
	if err != nil {
		t.Fatalf("AttemptSchedule acknowledged: %v", err)
	}
	if !allowed.Allowed || allowed.DecisionCode != "allowed" {
		t.Fatalf("valid conditional schedule rejected: %+v", allowed)
	}
}

func TestReturnedReferralReopensTriageWithoutDeletingHistory(t *testing.T) {
	fixture := newWorkflowFixture(t)
	_, err := fixture.store.TriageIncident(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "triage"},
		fixture.incident.ID, repository.TriageInput{Severity: domain.SeverityHigh, StopTraining: true, PublicGuidance: "refer", Expected: 1}, 3)
	if err != nil {
		t.Fatal(err)
	}
	referral, err := fixture.store.CreateReferral(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "refer"},
		domain.Referral{IncidentID: fixture.incident.ID, Organization: "Clinic", Reason: "assessment", Status: domain.ReferralRequested,
			Version: 1, CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	returned, err := fixture.store.ReturnReferral(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "return"},
		referral.ID, 1, "guardian consent document missing")
	if err != nil || returned.Status != domain.ReferralReturned {
		t.Fatalf("ReturnReferral=%+v err=%v", returned, err)
	}
	incident, err := fixture.store.GetIncident(context.Background(), fixture.incident.ID)
	if err != nil || incident.Status != domain.IncidentTriaged {
		t.Fatalf("incident after return=%+v err=%v", incident, err)
	}
	loaded, err := fixture.store.GetReferral(context.Background(), referral.ID)
	if err != nil || loaded.ReturnedReason == "" {
		t.Fatalf("returned referral history=%+v err=%v", loaded, err)
	}
	if _, err := fixture.store.AcceptReferral(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role}, referral.ID, returned.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("returned referral accepted directly: %v", err)
	}
}

// TestReturnReferralLeavesNoHalfReturnedStateOnFailure reproduces the reported
// symptom where a return request reported an error but the referral was later
// found returned while its incident stayed referred. The return must be atomic:
// if any post-update step fails, the referral must remain in its returnable
// state so the health professional can retry once the fault clears.
func TestReturnReferralLeavesNoHalfReturnedStateOnFailure(t *testing.T) {
	fixture := newWorkflowFixture(t)
	_, err := fixture.store.TriageIncident(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "triage"},
		fixture.incident.ID, repository.TriageInput{Severity: domain.SeverityModerate, StopTraining: true, PublicGuidance: "refer", Expected: 1}, 3)
	if err != nil {
		t.Fatal(err)
	}
	referral, err := fixture.store.CreateReferral(context.Background(), domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "refer"},
		domain.Referral{IncidentID: fixture.incident.ID, Organization: "Clinic", Reason: "assessment", Status: domain.ReferralRequested,
			Version: 1, CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := fixture.store.AcceptReferral(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "accept"},
		referral.ID, referral.Version)
	if err != nil {
		t.Fatalf("AcceptReferral: %v", err)
	}
	// Force the incident reopen step to fail by moving the incident into a state
	// that cannot transition back to triaged while the referral stays accepted.
	if _, err := fixture.store.db.Exec(`UPDATE incidents SET status=? WHERE id=?`, domain.IncidentClosed, fixture.incident.ID); err != nil {
		t.Fatalf("seed incident state: %v", err)
	}

	_, err = fixture.store.ReturnReferral(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "return"},
		referral.ID, accepted.Version, "guardian consent document missing")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("return against non-reopenable incident want conflict, got %v", err)
	}

	// The referral must NOT have been marked returned: no half-completed state.
	unchanged, err := fixture.store.GetReferral(context.Background(), referral.ID)
	if err != nil {
		t.Fatalf("GetReferral after failed return: %v", err)
	}
	if unchanged.Status != domain.ReferralAccepted || unchanged.ReturnedReason != "" || unchanged.Version != accepted.Version {
		t.Fatalf("referral mutated by failed return: %+v", unchanged)
	}

	// Once the fault clears (incident is restored to referred), the return must
	// succeed on the same version the professional originally held.
	if _, err := fixture.store.db.Exec(`UPDATE incidents SET status=? WHERE id=?`, domain.IncidentReferred, fixture.incident.ID); err != nil {
		t.Fatalf("restore incident state: %v", err)
	}
	retried, err := fixture.store.ReturnReferral(context.Background(), domain.Actor{UserID: fixture.professional.ID, Role: fixture.professional.Role, RequestID: "retry"},
		referral.ID, accepted.Version, "guardian consent document missing")
	if err != nil || retried.Status != domain.ReferralReturned {
		t.Fatalf("retry return after fault cleared=%+v err=%v", retried, err)
	}
	incident, err := fixture.store.GetIncident(context.Background(), fixture.incident.ID)
	if err != nil || incident.Status != domain.IncidentTriaged {
		t.Fatalf("incident after retried return=%+v err=%v", incident, err)
	}
}
