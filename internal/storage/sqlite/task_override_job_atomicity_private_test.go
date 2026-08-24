package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func TestOverrideJobFailureRollsBackEntireGrant(t *testing.T) {
	fixture := newWorkflowFixture(t)
	actor := domain.Actor{UserID: fixture.officer.ID, Role: fixture.officer.Role, RequestID: "override-atomicity"}
	if _, err := fixture.store.db.Exec(`CREATE TRIGGER reject_override_job BEFORE INSERT ON notification_jobs BEGIN SELECT RAISE(ABORT, 'job queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	override := domain.Override{IncidentID: fixture.incident.ID, GrantedBy: fixture.officer.ID, Reason: "supervised range assessment",
		ExpiresAt: testTime.Add(2 * time.Hour), CreatedAt: testTime}
	if _, err := fixture.store.GrantOverride(context.Background(), actor, override); err == nil {
		t.Fatal("job queue failure was reported as success")
	}
	for name, query := range map[string]string{
		"override":     `SELECT COUNT(*) FROM overrides WHERE incident_id=?`,
		"notification": `SELECT COUNT(*) FROM guardian_notifications WHERE incident_id=? AND message_class='manual_override'`,
		"job":          `SELECT COUNT(*) FROM notification_jobs`,
		"audit":        `SELECT COUNT(*) FROM audit_events WHERE action='override.granted'`,
	} {
		var count int
		if err := fixture.store.db.QueryRow(query, fixture.incident.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("failed override left %s rows=%d", name, count)
		}
	}
	if _, err := fixture.store.db.Exec(`DROP TRIGGER reject_override_job`); err != nil {
		t.Fatal(err)
	}
	granted, err := fixture.store.GrantOverride(context.Background(), actor, override)
	if err != nil || granted.ID == 0 || granted.NotificationID == nil {
		t.Fatalf("retry after queue recovery failed: override=%+v err=%v", granted, err)
	}
}
