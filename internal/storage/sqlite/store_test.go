package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

var testTime = time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "rehab.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return store
}

func createUser(t *testing.T, store *Store, email string, role domain.Role) domain.User {
	t.Helper()
	user, err := store.CreateUser(context.Background(), domain.User{Email: email, DisplayName: email, PasswordHash: "test-hash",
		Role: role, Active: true, CreatedAt: testTime, UpdatedAt: testTime})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", role, err)
	}
	return user
}

func createParticipant(t *testing.T, store *Store, guardian domain.User) domain.Participant {
	t.Helper()
	participant, err := store.CreateParticipant(context.Background(), domain.Participant{PublicID: "participant_001", Name: "Casey Chen",
		BirthDate: testTime.AddDate(-14, 0, 0), GuardianUserID: guardian.ID, VenueID: "venue-north", Active: true, CreatedAt: testTime})
	if err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}
	return participant
}

func createIncident(t *testing.T, store *Store, reporter domain.User, participant domain.Participant) domain.Incident {
	t.Helper()
	incident, err := store.CreateIncident(context.Background(), domain.Actor{UserID: reporter.ID, Role: reporter.Role, RequestID: "req-report"},
		domain.Incident{PublicID: "inc_001", ParticipantID: participant.ID, ReporterUserID: reporter.ID, Kind: domain.InjurySprain,
			BodyArea: "ankle", OccurredAt: testTime.Add(-time.Hour), Description: "rolled ankle during landing", Status: domain.IncidentReported,
			Severity: domain.SeverityLow, StopTraining: true, Version: 1, CreatedAt: testTime, UpdatedAt: testTime}, "idem-report", "request-sha")
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	return incident
}

func TestOpenAppliesEveryMigrationAndEnablesForeignKeys(t *testing.T) {
	store := openTestStore(t)
	var versions int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if versions != 1 {
		t.Fatalf("migration count=%d want 1", versions)
	}
	var foreignKeys int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d", foreignKeys)
	}
	tables := []string{"users", "sessions", "participants", "incidents", "incident_revisions", "triage_assessments", "guardian_notifications",
		"referrals", "rehab_plans", "rehab_plan_versions", "followups", "clearances", "training_blocks", "schedule_attempts", "overrides",
		"notification_jobs", "audit_events", "idempotency_keys"}
	for _, table := range tables {
		var name string
		err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil || name != table {
			t.Errorf("table %s missing: name=%q err=%v", table, name, err)
		}
	}
}

func TestMigrationIsRepeatableAndStateSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open first: %v", err)
	}
	guardian := createUser(t, store, "guardian@example.test", domain.RoleGuardian)
	participant := createParticipant(t, store, guardian)
	if err := store.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}
	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open second: %v", err)
	}
	defer reopened.Close()
	loaded, err := reopened.GetParticipant(context.Background(), participant.ID)
	if err != nil {
		t.Fatalf("GetParticipant after restart: %v", err)
	}
	if loaded.PublicID != participant.PublicID || loaded.GuardianUserID != guardian.ID {
		t.Fatalf("state changed after restart: %+v", loaded)
	}
	var versions int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("repeat migration count=%d err=%v", versions, err)
	}
}

func TestForeignKeyViolationRollsBackInvalidParticipant(t *testing.T) {
	store := openTestStore(t)
	_, err := store.CreateParticipant(context.Background(), domain.Participant{PublicID: "orphan", Name: "Orphan",
		BirthDate: testTime.AddDate(-10, 0, 0), GuardianUserID: 9999, VenueID: "venue", Active: true, CreatedAt: testTime})
	if err == nil {
		t.Fatal("orphan participant was accepted")
	}
	var count int
	if queryErr := store.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE public_id='orphan'`).Scan(&count); queryErr != nil || count != 0 {
		t.Fatalf("orphan persisted: count=%d err=%v", count, queryErr)
	}
}

func TestAuthSessionLifecyclePersistsHashAndEnforcesRevocation(t *testing.T) {
	store := openTestStore(t)
	user := createUser(t, store, "coach@example.test", domain.RoleCoach)
	session, err := store.CreateSession(context.Background(), domain.Session{UserID: user.ID, TokenHash: "hashed-token", ExpiresAt: testTime.Add(time.Hour), CreatedAt: testTime})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID == 0 {
		t.Fatal("session id not assigned")
	}
	loadedSession, loadedUser, err := store.SessionUserByTokenHash(context.Background(), "hashed-token", testTime)
	if err != nil {
		t.Fatalf("SessionUserByTokenHash: %v", err)
	}
	if loadedSession.UserID != user.ID || loadedUser.Role != domain.RoleCoach {
		t.Fatalf("session identity mismatch: session=%+v user=%+v", loadedSession, loadedUser)
	}
	if err := store.RevokeSession(context.Background(), "hashed-token", testTime.Add(time.Minute)); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, _, err := store.SessionUserByTokenHash(context.Background(), "hashed-token", testTime.Add(2*time.Minute)); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("revoked session authenticated: %v", err)
	}
	if err := store.RevokeSession(context.Background(), "hashed-token", testTime); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("double revoke should be unauthenticated: %v", err)
	}
}

func TestExpiredSessionsAreRejectedAndPurged(t *testing.T) {
	store := openTestStore(t)
	user := createUser(t, store, "officer@example.test", domain.RoleSafetyOfficer)
	for index, expiry := range []time.Time{testTime.Add(-time.Second), testTime, testTime.Add(time.Hour)} {
		_, err := store.CreateSession(context.Background(), domain.Session{UserID: user.ID, TokenHash: fmt.Sprintf("hash-%d", index), ExpiresAt: expiry, CreatedAt: testTime.Add(-time.Hour)})
		if err != nil {
			t.Fatalf("CreateSession %d: %v", index, err)
		}
	}
	if _, _, err := store.SessionUserByTokenHash(context.Background(), "hash-0", testTime); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("expired session accepted: %v", err)
	}
	deleted, err := store.DeleteExpiredSessions(context.Background(), testTime)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d want 2", deleted)
	}
	if _, _, err := store.SessionUserByTokenHash(context.Background(), "hash-2", testTime); err != nil {
		t.Fatalf("unexpired session removed: %v", err)
	}
}

func TestIncidentCreationIsAtomicAcrossBlockAuditAndIdempotency(t *testing.T) {
	store := openTestStore(t)
	guardian := createUser(t, store, "guardian@example.test", domain.RoleGuardian)
	reporter := createUser(t, store, "coach@example.test", domain.RoleCoach)
	participant := createParticipant(t, store, guardian)
	incident := createIncident(t, store, reporter, participant)
	checks := map[string]string{
		"incidents":        `SELECT COUNT(*) FROM incidents WHERE id=?`,
		"training_blocks":  `SELECT COUNT(*) FROM training_blocks WHERE incident_id=? AND active=1`,
		"audit_events":     `SELECT COUNT(*) FROM audit_events WHERE object_type='incident' AND object_id=?`,
		"idempotency_keys": `SELECT COUNT(*) FROM idempotency_keys WHERE response_body=?`,
	}
	for table, query := range checks {
		argument := any(incident.ID)
		if table == "audit_events" || table == "idempotency_keys" {
			argument = incident.PublicID
		}
		var count int
		if err := store.db.QueryRow(query, argument).Scan(&count); err != nil || count != 1 {
			t.Errorf("%s count=%d err=%v", table, count, err)
		}
	}
	_, err := store.CreateIncident(context.Background(), domain.Actor{UserID: reporter.ID, Role: reporter.Role, RequestID: "duplicate"},
		domain.Incident{PublicID: "inc_duplicate", ParticipantID: participant.ID, ReporterUserID: reporter.ID, Kind: domain.InjuryStrain,
			BodyArea: "leg", OccurredAt: testTime, Description: "duplicate", Status: domain.IncidentReported, Severity: domain.SeverityLow,
			StopTraining: true, Version: 1, CreatedAt: testTime, UpdatedAt: testTime}, "idem-report", "different-hash")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate idempotency key should conflict, got %v", err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM incidents`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate created partial incident: count=%d err=%v", count, err)
	}
}

func TestIncidentCreationRollsBackWhenAuditForeignKeyFails(t *testing.T) {
	store := openTestStore(t)
	guardian := createUser(t, store, "guardian@example.test", domain.RoleGuardian)
	participant := createParticipant(t, store, guardian)
	incident := domain.Incident{PublicID: "inc_rollback", ParticipantID: participant.ID, ReporterUserID: 99999, Kind: domain.InjuryStrain,
		BodyArea: "hamstring", OccurredAt: testTime, Description: "sudden pain", Status: domain.IncidentReported, Severity: domain.SeverityLow,
		StopTraining: true, Version: 1, CreatedAt: testTime, UpdatedAt: testTime}
	_, err := store.CreateIncident(context.Background(), domain.Actor{UserID: 99999, Role: domain.RoleCoach, RequestID: "rollback"}, incident, "rollback-key", "sha")
	if err == nil {
		t.Fatal("foreign-key failure was accepted")
	}
	for _, table := range []string{"incidents", "training_blocks", "audit_events", "idempotency_keys"} {
		var count int
		if queryErr := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); queryErr != nil || count != 0 {
			t.Errorf("rollback left %s rows=%d err=%v", table, count, queryErr)
		}
	}
}

func TestTriageAtomicallyUpdatesIncidentEnqueuesNoticeAndAudits(t *testing.T) {
	store := openTestStore(t)
	guardian := createUser(t, store, "guardian@example.test", domain.RoleGuardian)
	reporter := createUser(t, store, "coach@example.test", domain.RoleCoach)
	officer := createUser(t, store, "safety@example.test", domain.RoleSafetyOfficer)
	participant := createParticipant(t, store, guardian)
	incident := createIncident(t, store, reporter, participant)
	updated, err := store.TriageIncident(context.Background(), domain.Actor{UserID: officer.ID, Role: officer.Role, RequestID: "triage"}, incident.ID,
		repository.TriageInput{Severity: domain.SeverityHigh, StopTraining: true, PublicGuidance: "stop and elevate", ClinicalNotes: "protected", Expected: 1}, 4)
	if err != nil {
		t.Fatalf("TriageIncident: %v", err)
	}
	if updated.Status != domain.IncidentTriaged || updated.Severity != domain.SeverityHigh || updated.Version != 2 {
		t.Fatalf("triage update mismatch: %+v", updated)
	}
	for table, query := range map[string]string{
		"triage":       `SELECT COUNT(*) FROM triage_assessments WHERE incident_id=?`,
		"notification": `SELECT COUNT(*) FROM guardian_notifications WHERE incident_id=? AND guardian_user_id=?`,
		"job":          `SELECT COUNT(*) FROM notification_jobs`,
		"audit":        `SELECT COUNT(*) FROM audit_events WHERE action='incident.triaged'`,
	} {
		var count int
		var queryErr error
		if table == "notification" {
			queryErr = store.db.QueryRow(query, incident.ID, guardian.ID).Scan(&count)
		} else if table == "triage" {
			queryErr = store.db.QueryRow(query, incident.ID).Scan(&count)
		} else {
			queryErr = store.db.QueryRow(query).Scan(&count)
		}
		if queryErr != nil || count != 1 {
			t.Errorf("%s count=%d err=%v", table, count, queryErr)
		}
	}
}

func TestStaleTriageDoesNotLeavePartialAssessment(t *testing.T) {
	store := openTestStore(t)
	guardian := createUser(t, store, "guardian@example.test", domain.RoleGuardian)
	reporter := createUser(t, store, "coach@example.test", domain.RoleCoach)
	officer := createUser(t, store, "safety@example.test", domain.RoleSafetyOfficer)
	participant := createParticipant(t, store, guardian)
	incident := createIncident(t, store, reporter, participant)
	_, err := store.TriageIncident(context.Background(), domain.Actor{UserID: officer.ID, Role: officer.Role, RequestID: "stale"}, incident.ID,
		repository.TriageInput{Severity: domain.SeverityModerate, PublicGuidance: "rest", Expected: 99}, 3)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale triage should conflict: %v", err)
	}
	for _, table := range []string{"triage_assessments", "guardian_notifications", "notification_jobs"} {
		var count int
		if queryErr := store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); queryErr != nil || count != 0 {
			t.Errorf("stale triage left %s rows=%d err=%v", table, count, queryErr)
		}
	}
	loaded, err := store.GetIncident(context.Background(), incident.ID)
	if err != nil || loaded.Status != domain.IncidentReported || loaded.Version != 1 {
		t.Fatalf("incident mutated by stale triage: %+v err=%v", loaded, err)
	}
}

func TestConcurrentCorrectionAllowsOneVersionWinner(t *testing.T) {
	store := openTestStore(t)
	guardian := createUser(t, store, "guardian@example.test", domain.RoleGuardian)
	reporter := createUser(t, store, "coach@example.test", domain.RoleCoach)
	participant := createParticipant(t, store, guardian)
	incident := createIncident(t, store, reporter, participant)
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for index := 0; index < 2; index++ {
		go func(index int) {
			ready.Done()
			<-start
			_, err := store.CorrectIncident(context.Background(), domain.Actor{UserID: reporter.ID, Role: reporter.Role, RequestID: fmt.Sprintf("correct-%d", index)},
				incident.ID, repository.IncidentCorrection{BodyArea: fmt.Sprintf("ankle-%d", index), OccurredAt: incident.OccurredAt,
					Description: fmt.Sprintf("corrected-%d", index), Reason: "report correction", Expected: 1})
			results <- err
		}(index)
	}
	ready.Wait()
	close(start)
	var successes, conflicts int
	for index := 0; index < 2; index++ {
		err := <-results
		if err == nil {
			successes++
		} else if errors.Is(err, domain.ErrConflict) || containsLocked(err) {
			conflicts++
		} else {
			t.Errorf("unexpected correction result: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	loaded, err := store.GetIncident(context.Background(), incident.ID)
	if err != nil || loaded.Version != 2 {
		t.Fatalf("final incident version=%d err=%v", loaded.Version, err)
	}
	var revisions int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM incident_revisions WHERE incident_id=?`, incident.ID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("revision count=%d err=%v", revisions, err)
	}
}

func TestFailedCorrectionLeavesNoRevisionAndRetriesProduceSingleRevision(t *testing.T) {
	store := openTestStore(t)
	guardian := createUser(t, store, "guardian@example.test", domain.RoleGuardian)
	reporter := createUser(t, store, "coach@example.test", domain.RoleCoach)
	participant := createParticipant(t, store, guardian)
	incident := createIncident(t, store, reporter, participant)

	stale := repository.IncidentCorrection{BodyArea: "left ankle", OccurredAt: incident.OccurredAt,
		Description: "corrected side", Reason: "witness correction", Expected: 99}
	_, err := store.CorrectIncident(context.Background(), domain.Actor{UserID: reporter.ID, Role: reporter.Role, RequestID: "stale"},
		incident.ID, stale)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale correction returned %v", err)
	}
	var staleRevisions, staleAudit int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM incident_revisions WHERE incident_id=?`, incident.ID).Scan(&staleRevisions); err != nil || staleRevisions != 0 {
		t.Fatalf("failed correction left revisions=%d err=%v", staleRevisions, err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='incident.corrected'`).Scan(&staleAudit); err != nil || staleAudit != 0 {
		t.Fatalf("failed correction left audit events=%d err=%v", staleAudit, err)
	}

	// A normal retry at the still-current version must produce exactly one effective revision.
	valid := repository.IncidentCorrection{BodyArea: "left ankle", OccurredAt: incident.OccurredAt,
		Description: "corrected side after witness confirmation", Reason: "witness correction", Expected: 1}
	updated, err := store.CorrectIncident(context.Background(), domain.Actor{UserID: reporter.ID, Role: reporter.Role, RequestID: "retry"},
		incident.ID, valid)
	if err != nil {
		t.Fatalf("retry correction: %v", err)
	}
	if updated.BodyArea != "left ankle" || updated.Version != 2 {
		t.Fatalf("retry result mismatch: %+v", updated)
	}
	var revisions, audit int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM incident_revisions WHERE incident_id=?`, incident.ID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("revision count=%d err=%v", revisions, err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='incident.corrected'`).Scan(&audit); err != nil || audit != 1 {
		t.Fatalf("audit count=%d err=%v", audit, err)
	}
}

func containsLocked(err error) bool {
	return err != nil && (errors.Is(err, context.DeadlineExceeded) || stringContains(err.Error(), "locked") || stringContains(err.Error(), "busy"))
}

func stringContains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}

func TestCanceledContextStopsRepositoryWork(t *testing.T) {
	store := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.FindUserByEmail(ctx, "nobody@example.test")
	if err == nil {
		t.Fatal("canceled repository call succeeded")
	}
}

func TestParticipantPagingUsesMatchingFilterForCountAndRows(t *testing.T) {
	store := openTestStore(t)
	first := createUser(t, store, "guardian1@example.test", domain.RoleGuardian)
	second := createUser(t, store, "guardian2@example.test", domain.RoleGuardian)
	for index := 0; index < 5; index++ {
		guardian := first
		venue := "venue-north"
		if index >= 3 {
			guardian = second
			venue = "venue-south"
		}
		_, err := store.CreateParticipant(context.Background(), domain.Participant{PublicID: fmt.Sprintf("participant_%d", index), Name: fmt.Sprintf("Youth %d", index),
			BirthDate: testTime.AddDate(-12, 0, 0), GuardianUserID: guardian.ID, VenueID: venue, Active: true, CreatedAt: testTime})
		if err != nil {
			t.Fatalf("CreateParticipant %d: %v", index, err)
		}
	}
	page, err := store.ListParticipants(context.Background(), first.ID, "venue-north", domain.Page{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("ListParticipants: %v", err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Limit != 2 || page.Offset != 1 {
		t.Fatalf("paging mismatch: %+v", page)
	}
	for _, participant := range page.Items {
		if participant.GuardianUserID != first.ID || participant.VenueID != "venue-north" {
			t.Fatalf("filter leaked participant: %+v", participant)
		}
	}
}

func TestDatabaseRejectsNewerUnknownSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL); INSERT INTO schema_migrations VALUES(999,'future')`); err != nil {
		t.Fatalf("seed future schema: %v", err)
	}
	db.Close()
	if store, err := Open(context.Background(), path); err == nil {
		store.Close()
		t.Fatal("future schema was silently accepted")
	}
}
