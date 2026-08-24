package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func (s *Store) CreateReferral(ctx context.Context, actor domain.Actor, referral domain.Referral) (domain.Referral, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		incident, err := scanIncident(tx.QueryRowContext(ctx, incidentSelect+" WHERE i.id=?", referral.IncidentID))
		if err != nil {
			return err
		}
		if incident.Status != domain.IncidentTriaged {
			return fmt.Errorf("incident is not ready for referral: %w", domain.ErrConflict)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO referrals(incident_id,organization,reason,status,returned_reason,professional_id,version,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, referral.IncidentID, referral.Organization, referral.Reason, referral.Status, "", nil,
			referral.Version, timeText(referral.CreatedAt), timeText(referral.UpdatedAt))
		if err != nil {
			return fmt.Errorf("insert referral: %w", err)
		}
		referral.ID, _ = result.LastInsertId()
		update, err := tx.ExecContext(ctx, `UPDATE incidents SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
			domain.IncidentReferred, timeText(referral.CreatedAt), incident.ID, incident.Version)
		if err != nil {
			return fmt.Errorf("mark incident referred: %w", err)
		}
		if err := requireAffected(update, "incident"); err != nil {
			return err
		}
		return appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "referral.requested",
			ObjectType: "referral", ObjectID: fmt.Sprint(referral.ID), Result: audit.Succeeded, RequestID: actor.RequestID, CreatedAt: referral.CreatedAt})
	})
	return referral, err
}

func (s *Store) GetReferral(ctx context.Context, id int64) (domain.Referral, error) {
	return scanReferral(s.db.QueryRowContext(ctx, referralSelect+" WHERE id=?", id))
}

func (s *Store) AcceptReferral(ctx context.Context, actor domain.Actor, id, expected int64) (domain.Referral, error) {
	var referral domain.Referral
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		current, err := scanReferral(tx.QueryRowContext(ctx, referralSelect+" WHERE id=?", id))
		if err != nil {
			return err
		}
		if current.Version != expected {
			return &domain.ConflictError{Entity: "referral", Expected: expected, Actual: current.Version}
		}
		now := time.Now().UTC()
		if err := current.Accept(actor.UserID, now); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE referrals SET status=?,professional_id=?,version=?,updated_at=? WHERE id=? AND version=?`,
			current.Status, current.ProfessionalID, current.Version, timeText(now), id, expected)
		if err != nil {
			return fmt.Errorf("accept referral: %w", err)
		}
		if err := requireAffected(result, "referral"); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "referral.accepted",
			ObjectType: "referral", ObjectID: fmt.Sprint(id), Result: audit.Succeeded, RequestID: actor.RequestID, CreatedAt: now}); err != nil {
			return err
		}
		referral = current
		return nil
	})
	return referral, err
}

func (s *Store) ReturnReferral(ctx context.Context, actor domain.Actor, id, expected int64, reason string) (domain.Referral, error) {
	var referral domain.Referral
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		current, err := scanReferral(tx.QueryRowContext(ctx, referralSelect+" WHERE id=?", id))
		if err != nil {
			return err
		}
		if current.Version != expected {
			return &domain.ConflictError{Entity: "referral", Expected: expected, Actual: current.Version}
		}
		now := time.Now().UTC()
		if err := current.Return(reason, now); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE referrals SET status=?,returned_reason=?,version=?,updated_at=? WHERE id=? AND version=?`,
			current.Status, current.ReturnedReason, current.Version, timeText(now), id, expected)
		if err != nil {
			return fmt.Errorf("return referral: %w", err)
		}
		if err := requireAffected(result, "referral"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE incidents SET status=?,version=version+1,updated_at=? WHERE id=?`,
			domain.IncidentTriaged, timeText(now), current.IncidentID); err != nil {
			return fmt.Errorf("reopen incident after referral return: %w", err)
		}
		if err := appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "referral.returned",
			ObjectType: "referral", ObjectID: fmt.Sprint(id), Result: audit.Succeeded, Reason: reason, RequestID: actor.RequestID, CreatedAt: now}); err != nil {
			return err
		}
		referral = current
		return nil
	})
	return referral, err
}

func (s *Store) CreatePlan(ctx context.Context, actor domain.Actor, plan domain.RehabPlan) (domain.RehabPlan, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		referral, err := scanReferral(tx.QueryRowContext(ctx, referralSelect+" WHERE id=?", plan.ReferralID))
		if err != nil {
			return err
		}
		if referral.Status != domain.ReferralAccepted || referral.ProfessionalID == nil || *referral.ProfessionalID != actor.UserID {
			return fmt.Errorf("referral is not owned and accepted: %w", domain.ErrForbidden)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO rehab_plans(referral_id,professional_id,current_version,active,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
			plan.ReferralID, plan.ProfessionalID, plan.CurrentVersion, boolInt(plan.Active), timeText(plan.CreatedAt), timeText(plan.UpdatedAt))
		if err != nil {
			return fmt.Errorf("insert rehab plan: %w", err)
		}
		plan.ID, _ = result.LastInsertId()
		if _, err := tx.ExecContext(ctx, `UPDATE incidents SET status=?,version=version+1,updated_at=? WHERE id=?`,
			domain.IncidentRehabActive, timeText(plan.CreatedAt), referral.IncidentID); err != nil {
			return fmt.Errorf("activate incident rehabilitation: %w", err)
		}
		return appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "rehab_plan.created",
			ObjectType: "rehab_plan", ObjectID: fmt.Sprint(plan.ID), Result: audit.Succeeded, RequestID: actor.RequestID, CreatedAt: plan.CreatedAt})
	})
	return plan, err
}

func (s *Store) PublishPlanVersion(ctx context.Context, actor domain.Actor, planID, expected int64, version domain.RehabPlanVersion) (domain.RehabPlanVersion, error) {
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		var plan domain.RehabPlan
		var active int
		var created, updated string
		err := tx.QueryRowContext(ctx, `SELECT id,referral_id,professional_id,current_version,active,created_at,updated_at FROM rehab_plans WHERE id=?`, planID).
			Scan(&plan.ID, &plan.ReferralID, &plan.ProfessionalID, &plan.CurrentVersion, &active, &created, &updated)
		if err == sql.ErrNoRows {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read rehab plan: %w", err)
		}
		plan.Active = active == 1
		if plan.ProfessionalID != actor.UserID {
			return domain.ErrForbidden
		}
		now := version.PublishedAt.UTC()
		if now.IsZero() {
			now = time.Now().UTC()
		}
		if err := plan.Publish(&version, expected, now); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE rehab_plans SET current_version=?,updated_at=? WHERE id=? AND current_version=? AND active=1`,
			plan.CurrentVersion, timeText(now), planID, expected)
		if err != nil {
			return fmt.Errorf("advance plan version: %w", err)
		}
		if err := requireAffected(result, "rehab plan"); err != nil {
			return err
		}
		inserted, err := tx.ExecContext(ctx, `INSERT INTO rehab_plan_versions(plan_id,version,goals,restrictions,exercises,review_due_at,published_by,published_at)
			VALUES(?,?,?,?,?,?,?,?)`, planID, version.Version, version.Goals, version.Restrictions, version.Exercises, timeText(version.ReviewDueAt), actor.UserID, timeText(now))
		if err != nil {
			return fmt.Errorf("insert rehab plan version: %w", err)
		}
		version.ID, _ = inserted.LastInsertId()
		return appendAudit(ctx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "rehab_plan.version_published",
			ObjectType: "rehab_plan", ObjectID: fmt.Sprint(planID), Result: audit.Succeeded, RequestID: actor.RequestID,
			Metadata: map[string]string{"version": fmt.Sprint(version.Version)}, CreatedAt: now})
	})
	return version, err
}

func (s *Store) RecordFollowUp(ctx context.Context, actor domain.Actor, follow domain.FollowUp) (domain.FollowUp, error) {
	if err := follow.Validate(); err != nil {
		return domain.FollowUp{}, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO followups(plan_id,plan_version,professional_id,pain_score,mobility_score,load_tolerance,notes,assessed_at,valid_until,created_at)
		SELECT ?,?,?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM rehab_plans WHERE id=? AND professional_id=? AND active=1)`, follow.PlanID, follow.PlanVersion,
		actor.UserID, follow.PainScore, follow.MobilityScore, follow.LoadTolerance, follow.Notes, timeText(follow.AssessedAt), timeText(follow.ValidUntil), timeText(follow.CreatedAt), follow.PlanID, actor.UserID)
	if err != nil {
		return domain.FollowUp{}, fmt.Errorf("insert follow-up: %w", err)
	}
	if err := requireAffected(result, "follow-up"); err != nil {
		return domain.FollowUp{}, domain.ErrForbidden
	}
	follow.ID, _ = result.LastInsertId()
	return follow, nil
}

func (s *Store) GetFollowUp(ctx context.Context, id int64) (domain.FollowUp, error) {
	var follow domain.FollowUp
	var assessed, valid, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,plan_id,plan_version,professional_id,pain_score,mobility_score,load_tolerance,notes,assessed_at,valid_until,created_at FROM followups WHERE id=?`, id).
		Scan(&follow.ID, &follow.PlanID, &follow.PlanVersion, &follow.ProfessionalID, &follow.PainScore, &follow.MobilityScore, &follow.LoadTolerance, &follow.Notes, &assessed, &valid, &created)
	if err == sql.ErrNoRows {
		return domain.FollowUp{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.FollowUp{}, fmt.Errorf("read follow-up: %w", err)
	}
	for target, raw := range map[*time.Time]string{&follow.AssessedAt: assessed, &follow.ValidUntil: valid, &follow.CreatedAt: created} {
		parsed, err := parseTime(raw)
		if err != nil {
			return domain.FollowUp{}, err
		}
		*target = parsed
	}
	return follow, nil
}

func (s *Store) GrantClearance(ctx context.Context, actor domain.Actor, clearance domain.Clearance) (domain.Clearance, error) {
	txCtx := context.WithoutCancel(ctx)
	err := withTx(txCtx, s.db, func(tx *sql.Tx) error {
		var follow domain.FollowUp
		var assessed, validUntil, created string
		err := tx.QueryRowContext(txCtx, `SELECT id,plan_id,plan_version,professional_id,pain_score,mobility_score,load_tolerance,notes,assessed_at,valid_until,created_at FROM followups WHERE id=?`, clearance.FollowUpID).
			Scan(&follow.ID, &follow.PlanID, &follow.PlanVersion, &follow.ProfessionalID, &follow.PainScore, &follow.MobilityScore,
				&follow.LoadTolerance, &follow.Notes, &assessed, &validUntil, &created)
		if err == sql.ErrNoRows {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		if follow.AssessedAt, err = parseTime(assessed); err != nil {
			return err
		}
		if follow.ValidUntil, err = parseTime(validUntil); err != nil {
			return err
		}
		if follow.CreatedAt, err = parseTime(created); err != nil {
			return err
		}
		if follow.ProfessionalID != actor.UserID {
			return domain.ErrForbidden
		}
		if err := follow.EligibleForClearance(clearance.CreatedAt); err != nil {
			return err
		}
		result, err := tx.ExecContext(txCtx, `INSERT INTO clearances(incident_id,followup_id,professional_id,kind,conditions,status,valid_from,valid_until,version,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, clearance.IncidentID, clearance.FollowUpID, actor.UserID, clearance.Kind, clearance.Conditions, clearance.Status,
			timeText(clearance.ValidFrom), timeText(clearance.ValidUntil), clearance.Version, timeText(clearance.CreatedAt), timeText(clearance.UpdatedAt))
		if err != nil {
			return fmt.Errorf("insert clearance: %w", err)
		}
		clearance.ID, _ = result.LastInsertId()
		if _, err := tx.ExecContext(txCtx, `UPDATE incidents SET status=?,version=version+1,updated_at=? WHERE id=?`,
			domain.IncidentCleared, timeText(clearance.UpdatedAt), clearance.IncidentID); err != nil {
			return fmt.Errorf("mark incident cleared: %w", err)
		}
		if _, err := tx.ExecContext(txCtx, `UPDATE training_blocks SET active=0,released_at=? WHERE incident_id=? AND active=1`,
			timeText(clearance.UpdatedAt), clearance.IncidentID); err != nil {
			return fmt.Errorf("release training block after clearance: %w", err)
		}
		return appendAudit(txCtx, tx, audit.Record{ActorID: actor.UserID, ActorRole: string(actor.Role), Action: "clearance.granted",
			ObjectType: "clearance", ObjectID: fmt.Sprint(clearance.ID), Result: audit.Succeeded, RequestID: actor.RequestID, CreatedAt: clearance.CreatedAt})
	})
	return clearance, err
}

func (s *Store) LatestClearance(ctx context.Context, incidentID int64, at time.Time) (domain.Clearance, error) {
	return scanClearance(s.db.QueryRowContext(ctx, clearanceSelect+` WHERE incident_id=? AND status=? AND valid_from<=? AND valid_until>? ORDER BY created_at DESC,id DESC LIMIT 1`,
		incidentID, domain.ClearanceActive, timeText(at), timeText(at)))
}

const referralSelect = `SELECT id,incident_id,organization,reason,status,returned_reason,professional_id,version,created_at,updated_at FROM referrals`

func scanReferral(row rowScanner) (domain.Referral, error) {
	var referral domain.Referral
	var professional sql.NullInt64
	var created, updated string
	err := row.Scan(&referral.ID, &referral.IncidentID, &referral.Organization, &referral.Reason, &referral.Status, &referral.ReturnedReason,
		&professional, &referral.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return domain.Referral{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Referral{}, fmt.Errorf("scan referral: %w", err)
	}
	if professional.Valid {
		referral.ProfessionalID = &professional.Int64
	}
	var parseErr error
	if referral.CreatedAt, parseErr = parseTime(created); parseErr != nil {
		return domain.Referral{}, parseErr
	}
	if referral.UpdatedAt, parseErr = parseTime(updated); parseErr != nil {
		return domain.Referral{}, parseErr
	}
	return referral, nil
}

const clearanceSelect = `SELECT id,incident_id,followup_id,professional_id,kind,conditions,status,valid_from,valid_until,version,created_at,updated_at FROM clearances`

func scanClearance(row rowScanner) (domain.Clearance, error) {
	var clearance domain.Clearance
	var validFrom, validUntil, created, updated string
	err := row.Scan(&clearance.ID, &clearance.IncidentID, &clearance.FollowUpID, &clearance.ProfessionalID, &clearance.Kind,
		&clearance.Conditions, &clearance.Status, &validFrom, &validUntil, &clearance.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return domain.Clearance{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Clearance{}, fmt.Errorf("scan clearance: %w", err)
	}
	for target, raw := range map[*time.Time]string{&clearance.ValidFrom: validFrom, &clearance.ValidUntil: validUntil, &clearance.CreatedAt: created, &clearance.UpdatedAt: updated} {
		parsed, err := parseTime(raw)
		if err != nil {
			return domain.Clearance{}, err
		}
		*target = parsed
	}
	return clearance, nil
}
