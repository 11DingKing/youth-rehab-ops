package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func (s *Store) AttemptSchedule(ctx context.Context, actor domain.Actor, attempt domain.ScheduleAttempt) (domain.ScheduleAttempt, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var existing domain.ScheduleAttempt
		var starts, created string
		var acknowledged, allowed int
		err := tx.QueryRowContext(ctx, `SELECT id,participant_id,incident_id,requested_by,session_starts_at,conditions_acknowledged,allowed,decision_code,idempotency_key,created_at
			FROM schedule_attempts WHERE idempotency_key=?`, attempt.IdempotencyKey).
			Scan(&existing.ID, &existing.ParticipantID, &existing.IncidentID, &existing.RequestedBy, &starts, &acknowledged, &allowed,
				&existing.DecisionCode, &existing.IdempotencyKey, &created)
		if err == nil {
			existing.ConditionsAcknowledged = acknowledged == 1
			existing.Allowed = allowed == 1
			existing.SessionStartsAt, _ = parseTime(starts)
			existing.CreatedAt, _ = parseTime(created)
			attempt = existing
			return nil
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("check schedule idempotency: %w", err)
		}
		if strings.TrimSpace(attempt.IdempotencyKey) == "" {
			return &domain.FieldError{Field: "idempotency_key", Problem: "is required"}
		}
		var activeBlocks int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM training_blocks WHERE participant_id=? AND active=1`, attempt.ParticipantID).Scan(&activeBlocks); err != nil {
			return fmt.Errorf("check training blocks: %w", err)
		}
		decision := "allowed"
		attempt.Allowed = false
		if activeBlocks > 0 {
			var activeOverrides int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM overrides WHERE incident_id=? AND revoked_at IS NULL AND expires_at>?`,
				attempt.IncidentID, timeText(attempt.SessionStartsAt)).Scan(&activeOverrides); err != nil {
				return fmt.Errorf("check active overrides: %w", err)
			}
			if activeOverrides > 0 {
				attempt.Allowed = true
				decision = "manual_override"
			} else {
				decision = "active_training_block"
			}
		} else {
			clearance, err := scanClearance(tx.QueryRowContext(ctx, clearanceSelect+` WHERE incident_id=? AND status=? AND valid_from<=? AND valid_until>? ORDER BY created_at DESC,id DESC LIMIT 1`,
				attempt.IncidentID, domain.ClearanceActive, timeText(attempt.SessionStartsAt), timeText(attempt.SessionStartsAt)))
			if err != nil {
				if err == domain.ErrNotFound {
					decision = "clearance_missing_or_expired"
				} else {
					return err
				}
			} else if err := clearance.AllowsTraining(attempt.SessionStartsAt, attempt.ConditionsAcknowledged); err != nil {
				decision = "clearance_conditions_not_met"
			} else {
				attempt.Allowed = true
			}
		}
		attempt.DecisionCode = decision
		result, err := tx.ExecContext(ctx, `INSERT INTO schedule_attempts(participant_id,incident_id,requested_by,session_starts_at,conditions_acknowledged,allowed,decision_code,idempotency_key,created_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, attempt.ParticipantID, attempt.IncidentID, actor.UserID, timeText(attempt.SessionStartsAt), boolInt(attempt.ConditionsAcknowledged),
			boolInt(attempt.Allowed), attempt.DecisionCode, attempt.IdempotencyKey, timeText(attempt.CreatedAt))
		if err != nil {
			return fmt.Errorf("insert schedule attempt: %w", err)
		}
		attempt.ID, _ = result.LastInsertId()
		resultState := audit.Denied
		if attempt.Allowed {
			resultState = audit.Succeeded
		}
		return appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "schedule.attempted",
			ObjectType: "incident", ObjectID: fmt.Sprint(attempt.IncidentID), Result: resultState, Reason: attempt.DecisionCode,
			RequestID: actor.RequestID, CreatedAt: attempt.CreatedAt})
	})
	return attempt, err
}

func (s *Store) GrantOverride(ctx context.Context, actor domain.Actor, override domain.Override) (domain.Override, error) {
	grant, err := domain.PrepareOverrideGrant(override)
	if err != nil {
		return domain.Override{}, err
	}
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		var guardianID int64
		if err := tx.QueryRowContext(ctx, `SELECT p.guardian_user_id FROM incidents i JOIN participants p ON p.id=i.participant_id WHERE i.id=?`, override.IncidentID).Scan(&guardianID); err != nil {
			return mapNotFound(err, "read incident guardian")
		}
		notification, err := tx.ExecContext(ctx, `INSERT INTO guardian_notifications(incident_id,guardian_user_id,channel,message_class,status,created_at) VALUES(?,?,?,?,?,?)`,
			override.IncidentID, guardianID, "email", grant.MessageClass, "pending", timeText(override.CreatedAt))
		if err != nil {
			return fmt.Errorf("create override notification: %w", err)
		}
		notificationID, _ := notification.LastInsertId()
		override.NotificationID = &notificationID
		result, err := tx.ExecContext(ctx, `INSERT INTO overrides(incident_id,granted_by,reason,expires_at,notification_id,created_at) VALUES(?,?,?,?,?,?)`,
			override.IncidentID, actor.UserID, override.Reason, timeText(override.ExpiresAt), notificationID, timeText(override.CreatedAt))
		if err != nil {
			return fmt.Errorf("insert override: %w", err)
		}
		override.ID, _ = result.LastInsertId()
		return nil
	})
	if err != nil {
		return domain.Override{}, err
	}
	err = withTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_jobs(notification_id,status,max_attempts,next_attempt_at,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			*override.NotificationID, "pending", grant.JobMaxAttempts, timeText(override.CreatedAt), timeText(override.CreatedAt), timeText(override.CreatedAt)); err != nil {
			return fmt.Errorf("enqueue override notification: %w", err)
		}
		return appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "override.granted",
			ObjectType: "override", ObjectID: fmt.Sprint(override.ID), Result: audit.Succeeded, Reason: override.Reason,
			RequestID: actor.RequestID, CreatedAt: override.CreatedAt})
	})
	return override, err
}

func (s *Store) RevokeOverride(ctx context.Context, actor domain.Actor, overrideID int64, reason string) error {
	now := time.Now().UTC()
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE overrides SET revoked_at=? WHERE id=? AND revoked_at IS NULL AND expires_at>?`, timeText(now), overrideID, timeText(now))
		if err != nil {
			return fmt.Errorf("revoke override: %w", err)
		}
		if err := requireAffected(result, "override"); err != nil {
			return err
		}
		return appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "override.revoked",
			ObjectType: "override", ObjectID: fmt.Sprint(overrideID), Result: audit.Succeeded, Reason: reason,
			RequestID: actor.RequestID, CreatedAt: now})
	})
}
