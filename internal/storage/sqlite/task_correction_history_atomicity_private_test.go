package sqlite

import (
	"context"
	"testing"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

func TestFailedCorrectionLeavesCurrentAndHistoryUnchanged(t *testing.T) {
	store := openTestStore(t)
	guardian := createUser(t, store, "correction-guardian@example.test", domain.RoleGuardian)
	coach := createUser(t, store, "correction-coach@example.test", domain.RoleCoach)
	participant := createParticipant(t, store, guardian)
	incident := createIncident(t, store, coach, participant)
	actor := domain.Actor{UserID: coach.ID, Role: coach.Role, RequestID: "correction-atomicity"}
	correction := repository.IncidentCorrection{BodyArea: "left ankle", OccurredAt: incident.OccurredAt,
		Description: "corrected landing detail", Reason: "coach confirmed side", Expected: incident.Version}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_incident_correction BEFORE UPDATE ON incidents BEGIN SELECT RAISE(ABORT, 'incident update unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CorrectIncident(context.Background(), actor, incident.ID, correction); err == nil {
		t.Fatal("incident update failure was reported as success")
	}
	loaded, err := store.GetIncident(context.Background(), incident.ID)
	if err != nil {
		t.Fatal(err)
	}
	var revisions int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM incident_revisions WHERE incident_id=?`, incident.ID).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if loaded.Version != incident.Version || loaded.BodyArea != incident.BodyArea || revisions != 0 {
		t.Fatalf("failed correction changed state: version=%d body_area=%q revisions=%d", loaded.Version, loaded.BodyArea, revisions)
	}
	if _, err := store.db.Exec(`DROP TRIGGER reject_incident_correction`); err != nil {
		t.Fatal(err)
	}
	updated, err := store.CorrectIncident(context.Background(), actor, incident.ID, correction)
	if err != nil || updated.Version != incident.Version+1 || updated.BodyArea != "left ankle" {
		t.Fatalf("retry after recovery failed: incident=%+v err=%v", updated, err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM incident_revisions WHERE incident_id=?`, incident.ID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("revision count after retry=%d err=%v", revisions, err)
	}
}
