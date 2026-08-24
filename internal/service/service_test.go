package service_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
	"github.com/11DingKing/youth-rehab-ops/internal/storage/sqlite"
)

var serviceTime = time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)

type serviceFixture struct {
	store        *sqlite.Store
	clock        *clock.Fixed
	auth         *service.AuthService
	incidents    *service.IncidentService
	care         *service.CareService
	schedule     *service.ScheduleService
	coach        domain.User
	officer      domain.User
	guardian     domain.User
	professional domain.User
	participant  domain.Participant
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixed := &clock.Fixed{Time: serviceTime}
	auth := service.NewAuth(store, fixed, time.Hour)
	fixture := &serviceFixture{store: store, clock: fixed, auth: auth,
		incidents: service.NewIncidents(store, store, fixed, 4), care: service.NewCare(store, fixed), schedule: service.NewSchedule(store, fixed)}
	fixture.coach = fixture.createUser(t, "coach@example.test", domain.RoleCoach)
	fixture.officer = fixture.createUser(t, "officer@example.test", domain.RoleSafetyOfficer)
	fixture.guardian = fixture.createUser(t, "guardian@example.test", domain.RoleGuardian)
	fixture.professional = fixture.createUser(t, "professional@example.test", domain.RoleHealthProfessional)
	participant, err := store.CreateParticipant(context.Background(), domain.Participant{PublicID: "participant_service", Name: "Morgan Li",
		BirthDate: serviceTime.AddDate(-15, 0, 0), GuardianUserID: fixture.guardian.ID, VenueID: "venue-service", Active: true, CreatedAt: serviceTime})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	fixture.participant = participant
	return fixture
}

func (f *serviceFixture) createUser(t *testing.T, email string, role domain.Role) domain.User {
	t.Helper()
	user, err := f.auth.BootstrapUser(context.Background(), email, email, "safe password value", role)
	if err != nil {
		t.Fatalf("BootstrapUser(%s): %v", role, err)
	}
	return user
}

func actor(user domain.User, request string) domain.Actor {
	return domain.Actor{UserID: user.ID, Role: user.Role, RequestID: request}
}

func reportInput(participantID int64, key string) service.ReportIncidentInput {
	return service.ReportIncidentInput{ParticipantID: participantID, Kind: domain.InjurySprain, BodyArea: "right ankle",
		OccurredAt: serviceTime.Add(-20 * time.Minute), Description: "pain after landing", IdempotencyKey: key}
}

func TestAuthenticationLifecycleLoginAuthenticateLogout(t *testing.T) {
	fixture := newServiceFixture(t)
	result, err := fixture.auth.Login(context.Background(), fixture.coach.Email, "safe password value")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.Token == "" || !result.ExpiresAt.Equal(serviceTime.Add(time.Hour)) || result.User.PasswordHash != "" {
		t.Fatalf("login result leaks or omits fields: %+v", result)
	}
	identity, err := fixture.auth.Authenticate(context.Background(), result.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.ID != fixture.coach.ID || identity.Role != domain.RoleCoach || identity.PasswordHash != "" {
		t.Fatalf("identity mismatch: %+v", identity)
	}
	if err := fixture.auth.Logout(context.Background(), result.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := fixture.auth.Authenticate(context.Background(), result.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("logged-out token authenticated: %v", err)
	}
}

func TestLoginHidesCredentialExistenceAndWrongPassword(t *testing.T) {
	fixture := newServiceFixture(t)
	for _, credentials := range []struct {
		email    string
		password string
	}{
		{fixture.coach.Email, "wrong password value"},
		{"missing@example.test", "safe password value"},
		{"", ""},
	} {
		if _, err := fixture.auth.Login(context.Background(), credentials.email, credentials.password); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("credentials %+v returned %v", credentials, err)
		}
	}
}

func TestSessionExpiryIsEnforcedByService(t *testing.T) {
	fixture := newServiceFixture(t)
	result, err := fixture.auth.Login(context.Background(), fixture.officer.Email, "safe password value")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	fixture.clock.Time = serviceTime.Add(time.Hour)
	if _, err := fixture.auth.Authenticate(context.Background(), result.Token); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("token at exact expiry authenticated: %v", err)
	}
	deleted, err := fixture.auth.PurgeExpired(context.Background())
	if err != nil || deleted != 1 {
		t.Fatalf("PurgeExpired deleted=%d err=%v", deleted, err)
	}
}

func TestBootstrapOfficerIsIdempotent(t *testing.T) {
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "bootstrap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	auth := service.NewAuth(store, clock.Fixed{Time: serviceTime}, time.Hour)
	for index := 0; index < 2; index++ {
		if err := auth.EnsureBootstrapOfficer(context.Background(), "boot@example.test", "bootstrap password"); err != nil {
			t.Fatalf("EnsureBootstrapOfficer %d: %v", index, err)
		}
	}
	user, err := store.FindUserByEmail(context.Background(), "boot@example.test")
	if err != nil || user.Role != domain.RoleSafetyOfficer {
		t.Fatalf("bootstrap user=%+v err=%v", user, err)
	}
}

func TestIncidentReportingEnforcesRoleAndCreatesOperationalView(t *testing.T) {
	fixture := newServiceFixture(t)
	for _, user := range []domain.User{fixture.coach, fixture.officer} {
		view, err := fixture.incidents.Report(context.Background(), actor(user, "report"), reportInput(fixture.participant.ID, "key-"+string(user.Role)))
		if err != nil {
			t.Fatalf("%s report: %v", user.Role, err)
		}
		if view.ID == 0 || view.PublicID == "" || view.Status != domain.IncidentReported || !view.StopTraining || view.Version != 1 {
			t.Fatalf("report view mismatch: %+v", view)
		}
	}
	for _, user := range []domain.User{fixture.guardian, fixture.professional} {
		if _, err := fixture.incidents.Report(context.Background(), actor(user, "forbidden"), reportInput(fixture.participant.ID, "forbidden-"+string(user.Role))); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s reported incident: %v", user.Role, err)
		}
	}
}

func TestIncidentReportingRequiresIdempotencyAndValidParticipant(t *testing.T) {
	fixture := newServiceFixture(t)
	input := reportInput(fixture.participant.ID, "")
	if _, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "missing-key"), input); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("missing idempotency key returned %v", err)
	}
	input = reportInput(999999, "unknown-participant")
	if _, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "unknown"), input); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown participant returned %v", err)
	}
}

func TestGuardianCanOnlyReadOwnParticipantIncident(t *testing.T) {
	fixture := newServiceFixture(t)
	view, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "report"), reportInput(fixture.participant.ID, "guardian-own"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.incidents.Get(context.Background(), actor(fixture.guardian, "own"), view.ID); err != nil {
		t.Fatalf("guardian could not read own incident: %v", err)
	}
	other := fixture.createUser(t, "other-guardian@example.test", domain.RoleGuardian)
	if _, err := fixture.incidents.Get(context.Background(), actor(other, "other"), view.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("unrelated guardian read incident: %v", err)
	}
}

func TestOnlySafetyOfficerCanTriage(t *testing.T) {
	fixture := newServiceFixture(t)
	view, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "report"), reportInput(fixture.participant.ID, "triage-role"))
	if err != nil {
		t.Fatal(err)
	}
	input := service.TriageIncidentInput{Severity: domain.SeverityModerate, StopTraining: true, PublicGuidance: "rest and observe", ClinicalNotes: "protected", ExpectedVersion: 1}
	for _, user := range []domain.User{fixture.coach, fixture.guardian, fixture.professional} {
		if _, err := fixture.incidents.Triage(context.Background(), actor(user, "bad-triage"), view.ID, input); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s triaged incident: %v", user.Role, err)
		}
	}
	updated, err := fixture.incidents.Triage(context.Background(), actor(fixture.officer, "triage"), view.ID, input)
	if err != nil {
		t.Fatalf("officer triage: %v", err)
	}
	if updated.Status != domain.IncidentTriaged || updated.Version != 2 || updated.Severity != domain.SeverityModerate {
		t.Fatalf("triage result mismatch: %+v", updated)
	}
}

func TestCorrectionPreservesOptimisticVersionBoundary(t *testing.T) {
	fixture := newServiceFixture(t)
	view, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "report"), reportInput(fixture.participant.ID, "correct"))
	if err != nil {
		t.Fatal(err)
	}
	correction := repository.IncidentCorrection{BodyArea: "left ankle", OccurredAt: serviceTime.Add(-25 * time.Minute),
		Description: "corrected side after witness confirmation", Reason: "witness correction", Expected: view.Version}
	updated, err := fixture.incidents.Correct(context.Background(), actor(fixture.coach, "correct"), view.ID, correction)
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if updated.BodyArea != "left ankle" || updated.Version != 2 {
		t.Fatalf("correction mismatch: %+v", updated)
	}
	if _, err := fixture.incidents.Correct(context.Background(), actor(fixture.officer, "stale"), view.ID, correction); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale correction returned %v", err)
	}
	if _, err := fixture.incidents.Correct(context.Background(), actor(fixture.guardian, "forbidden"), view.ID, correction); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("guardian correction returned %v", err)
	}
}

func TestCareServiceKeepsClinicalDecisionsWithProfessionals(t *testing.T) {
	fixture := newServiceFixture(t)
	view, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "report"), reportInput(fixture.participant.ID, "care"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.incidents.Triage(context.Background(), actor(fixture.officer, "triage"), view.ID,
		service.TriageIncidentInput{Severity: domain.SeverityHigh, StopTraining: true, PublicGuidance: "seek evaluation", ExpectedVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	referral, err := fixture.care.Refer(context.Background(), actor(fixture.officer, "refer"), view.ID, "Youth Clinic", "persistent pain")
	if err != nil {
		t.Fatalf("Refer: %v", err)
	}
	if _, err := fixture.care.AcceptReferral(context.Background(), actor(fixture.coach, "bad-accept"), referral.ID, referral.Version); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("coach accepted referral: %v", err)
	}
	accepted, err := fixture.care.AcceptReferral(context.Background(), actor(fixture.professional, "accept"), referral.ID, referral.Version)
	if err != nil || accepted.Status != domain.ReferralAccepted {
		t.Fatalf("professional accept=%+v err=%v", accepted, err)
	}
	plan, err := fixture.care.CreatePlan(context.Background(), actor(fixture.professional, "plan"), referral.ID)
	if err != nil || !plan.Active {
		t.Fatalf("CreatePlan=%+v err=%v", plan, err)
	}
	version, err := fixture.care.PublishPlan(context.Background(), actor(fixture.professional, "publish"), plan.ID, 0,
		"pain-free walking", "no running", "mobility and balance", serviceTime.Add(7*24*time.Hour))
	if err != nil || version.Version != 1 {
		t.Fatalf("PublishPlan=%+v err=%v", version, err)
	}
}

func TestScheduleInputValidationAndRoleBoundary(t *testing.T) {
	fixture := newServiceFixture(t)
	valid := domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: 1, SessionStartsAt: serviceTime.Add(time.Hour), IdempotencyKey: "schedule-key"}
	if _, err := fixture.schedule.Attempt(context.Background(), actor(fixture.guardian, "guardian"), valid); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("guardian schedule returned %v", err)
	}
	invalid := valid
	invalid.SessionStartsAt = serviceTime
	if _, err := fixture.schedule.Attempt(context.Background(), actor(fixture.coach, "past"), invalid); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("past schedule returned %v", err)
	}
	if _, err := fixture.schedule.Override(context.Background(), actor(fixture.coach, "override"), 1, "reason", time.Hour); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("coach override returned %v", err)
	}
	if _, err := fixture.schedule.Override(context.Background(), actor(fixture.officer, "long"), 1, "reason", 25*time.Hour); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("long override returned %v", err)
	}
}

func TestCanceledScheduleAttemptLeavesNoDecisionOrAudit(t *testing.T) {
	fixture := newServiceFixture(t)
	view, err := fixture.incidents.Report(context.Background(), actor(fixture.coach, "report"), reportInput(fixture.participant.ID, "canceled-schedule-report"))
	if err != nil {
		t.Fatalf("report incident: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, attemptErr := fixture.schedule.Attempt(ctx, actor(fixture.coach, "canceled-schedule"),
		domain.ScheduleAttempt{ParticipantID: fixture.participant.ID, IncidentID: view.ID,
			SessionStartsAt: serviceTime.Add(time.Hour), IdempotencyKey: "canceled-schedule"})
	if attemptErr == nil {
		t.Fatal("canceled schedule attempt succeeded")
	}
	audits, err := fixture.store.ListAudit(context.Background(), "incident", fmt.Sprint(view.ID), domain.Page{Limit: 100})
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	for _, record := range audits.Items {
		if record.Action == "schedule.attempted" {
			t.Fatalf("canceled schedule left audit record: %+v", record)
		}
	}
}
