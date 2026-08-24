package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

func (s *Store) CreateIncident(ctx context.Context, actor domain.Actor, incident domain.Incident, idempotencyKey, requestHash string) (domain.Incident, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var existingBody string
		err := tx.QueryRowContext(ctx, `SELECT response_body FROM idempotency_keys WHERE actor_id=? AND method='POST' AND path='/api/incidents' AND key=?`, actor.UserID, idempotencyKey).Scan(&existingBody)
		if err == nil {
			return fmt.Errorf("idempotency key already committed: %w", domain.ErrConflict)
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check incident idempotency: %w", err)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO incidents(public_id,participant_id,reporter_user_id,kind,body_area,occurred_at,description,status,severity,stop_training,version,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, incident.PublicID, incident.ParticipantID, incident.ReporterUserID, incident.Kind, incident.BodyArea,
			timeText(incident.OccurredAt), incident.Description, incident.Status, incident.Severity, boolInt(incident.StopTraining), incident.Version,
			timeText(incident.CreatedAt), timeText(incident.UpdatedAt))
		if err != nil {
			return fmt.Errorf("insert incident: %w", err)
		}
		incident.ID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read incident id: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO training_blocks(participant_id,incident_id,reason,active,created_at) VALUES(?,?,?,?,?)`,
			incident.ParticipantID, incident.ID, "incident_reported", 1, timeText(incident.CreatedAt)); err != nil {
			return fmt.Errorf("create initial training block: %w", err)
		}
		if err := appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "incident.reported",
			ObjectType: "incident", ObjectID: incident.PublicID, Result: audit.Succeeded, RequestID: actor.RequestID, CreatedAt: incident.CreatedAt}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_keys(actor_id,method,path,key,request_hash,response_code,response_body,created_at,expires_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, actor.UserID, "POST", "/api/incidents", idempotencyKey, requestHash, 201,
			incident.PublicID, timeText(incident.CreatedAt), timeText(incident.CreatedAt.Add(24*time.Hour))); err != nil {
			return fmt.Errorf("record incident idempotency: %w", err)
		}
		return nil
	})
	return incident, err
}

func (s *Store) GetIncident(ctx context.Context, id int64) (domain.Incident, error) {
	return scanIncident(s.db.QueryRowContext(ctx, incidentSelect+" WHERE i.id=?", id))
}

func (s *Store) ListIncidents(ctx context.Context, filter repository.IncidentFilter) (domain.PageResult[domain.Incident], error) {
	page := filter.Page.Normalize()
	result := domain.PageResult[domain.Incident]{Limit: page.Limit, Offset: page.Offset}
	where := " WHERE 1=1"
	args := []any{}
	if filter.ParticipantID > 0 {
		where += " AND i.participant_id=?"
		args = append(args, filter.ParticipantID)
	}
	if filter.Status != "" {
		where += " AND i.status=?"
		args = append(args, filter.Status)
	}
	if filter.VenueID != "" {
		where += " AND p.venue_id=?"
		args = append(args, filter.VenueID)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents i JOIN participants p ON p.id=i.participant_id`+where, args...).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count incidents: %w", err)
	}
	args = append(args, page.Limit, page.Offset)
	rows, err := s.db.QueryContext(ctx, incidentSelect+` JOIN participants p ON p.id=i.participant_id`+where+` ORDER BY i.updated_at DESC,i.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return result, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, incident)
	}
	return result, rows.Err()
}

func (s *Store) CorrectIncident(ctx context.Context, actor domain.Actor, id int64, correction repository.IncidentCorrection) (domain.Incident, error) {
	var updated domain.Incident
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		current, err := scanIncident(tx.QueryRowContext(ctx, incidentSelect+" WHERE i.id=?", id))
		if err != nil {
			return err
		}
		if current.Version != correction.Expected {
			return &domain.ConflictError{Entity: "incident", Expected: correction.Expected, Actual: current.Version}
		}
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM incident_revisions WHERE incident_id=?`, id).Scan(&revision); err != nil {
			return fmt.Errorf("next incident revision: %w", err)
		}
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `INSERT INTO incident_revisions(incident_id,revision,body_area,occurred_at,description,reason,corrected_by,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			id, revision, current.BodyArea, timeText(current.OccurredAt), current.Description, correction.Reason, actor.UserID, timeText(now)); err != nil {
			return fmt.Errorf("preserve incident revision: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE incidents SET body_area=?,occurred_at=?,description=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
			correction.BodyArea, timeText(correction.OccurredAt), correction.Description, timeText(now), id, correction.Expected)
		if err != nil {
			return fmt.Errorf("correct incident: %w", err)
		}
		if err := requireAffected(result, "incident"); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "incident.corrected",
			ObjectType: "incident", ObjectID: current.PublicID, Result: audit.Succeeded, Reason: correction.Reason, RequestID: actor.RequestID, CreatedAt: now}); err != nil {
			return err
		}
		updated, err = scanIncident(tx.QueryRowContext(ctx, incidentSelect+" WHERE i.id=?", id))
		return err
	})
	return updated, err
}

func (s *Store) TriageIncident(ctx context.Context, actor domain.Actor, id int64, input repository.TriageInput, maxAttempts int) (domain.Incident, error) {
	var updated domain.Incident
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		current, err := scanIncident(tx.QueryRowContext(ctx, incidentSelect+" WHERE i.id=?", id))
		if err != nil {
			return err
		}
		if current.Version != input.Expected || current.Status != domain.IncidentReported {
			return fmt.Errorf("incident cannot be triaged from current version/state: %w", domain.ErrConflict)
		}
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx, `INSERT INTO triage_assessments(incident_id,safety_officer_id,severity,stop_training,public_guidance,clinical_notes,assessed_at)
			VALUES(?,?,?,?,?,?,?)`, id, actor.UserID, input.Severity, boolInt(input.StopTraining), input.PublicGuidance, input.ClinicalNotes, timeText(now)); err != nil {
			return fmt.Errorf("insert triage assessment: %w", err)
		}
		result, err := tx.ExecContext(ctx, `UPDATE incidents SET status=?,severity=?,stop_training=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
			domain.IncidentTriaged, input.Severity, boolInt(input.StopTraining), timeText(now), id, input.Expected)
		if err != nil {
			return fmt.Errorf("update incident triage: %w", err)
		}
		if err := requireAffected(result, "incident"); err != nil {
			return err
		}
		var guardianID int64
		if err := tx.QueryRowContext(ctx, `SELECT guardian_user_id FROM participants WHERE id=?`, current.ParticipantID).Scan(&guardianID); err != nil {
			return fmt.Errorf("read participant guardian: %w", err)
		}
		notification, err := tx.ExecContext(ctx, `INSERT INTO guardian_notifications(incident_id,guardian_user_id,channel,message_class,status,created_at) VALUES(?,?,?,?,?,?)`,
			id, guardianID, "email", "triage_completed", "pending", timeText(now))
		if err != nil {
			return fmt.Errorf("insert guardian notification: %w", err)
		}
		notificationID, _ := notification.LastInsertId()
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_jobs(notification_id,status,max_attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			notificationID, "pending", maxAttempts, timeText(now), timeText(now), timeText(now)); err != nil {
			return fmt.Errorf("enqueue guardian notification: %w", err)
		}
		auditCtx := context.WithoutCancel(ctx)
		if err := appendAudit(auditCtx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "incident.triaged",
			ObjectType: "incident", ObjectID: current.PublicID, Result: audit.Succeeded, RequestID: actor.RequestID, CreatedAt: now}); err != nil {
			return err
		}
		updated, err = scanIncident(tx.QueryRowContext(ctx, incidentSelect+" WHERE i.id=?", id))
		return err
	})
	return updated, err
}

func (s *Store) ReleaseTrainingBlock(ctx context.Context, actor domain.Actor, incidentID int64, reason string) error {
	now := time.Now().UTC()
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE training_blocks SET active=0,released_at=? WHERE incident_id=? AND active=1`, timeText(now), incidentID)
		if err != nil {
			return fmt.Errorf("release training block: %w", err)
		}
		if err := requireAffected(result, "training block"); err != nil {
			return err
		}
		return appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "training_block.released",
			ObjectType: "incident", ObjectID: fmt.Sprint(incidentID), Result: audit.Succeeded, Reason: reason, RequestID: actor.RequestID, CreatedAt: now})
	})
}

const incidentSelect = `SELECT i.id,i.public_id,i.participant_id,i.reporter_user_id,i.kind,i.body_area,i.occurred_at,i.description,i.status,i.severity,i.stop_training,i.version,i.created_at,i.updated_at FROM incidents i`

func scanIncident(row rowScanner) (domain.Incident, error) {
	var incident domain.Incident
	var occurred, created, updated string
	var stop int
	err := row.Scan(&incident.ID, &incident.PublicID, &incident.ParticipantID, &incident.ReporterUserID, &incident.Kind, &incident.BodyArea,
		&occurred, &incident.Description, &incident.Status, &incident.Severity, &stop, &incident.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return domain.Incident{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Incident{}, fmt.Errorf("scan incident: %w", err)
	}
	incident.StopTraining = stop == 1
	for target, source := range map[*time.Time]string{&incident.OccurredAt: occurred, &incident.CreatedAt: created, &incident.UpdatedAt: updated} {
		parsed, err := parseTime(source)
		if err != nil {
			return domain.Incident{}, err
		}
		*target = parsed
	}
	return incident, nil
}

var _ = strings.TrimSpace
