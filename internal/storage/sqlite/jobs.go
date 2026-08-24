package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/repository"
)

func (s *Store) ClaimNotificationJobs(ctx context.Context, owner string, limit int, now time.Time, lease time.Duration) ([]repository.NotificationJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var jobs []repository.NotificationJob
	err := withTx(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,notification_id,status,attempts,max_attempts,next_attempt_at,lease_owner,lease_until,last_error,created_at,updated_at
			FROM notification_jobs WHERE status IN ('pending','retry') AND next_attempt_at<=? AND (lease_until IS NULL OR lease_until<=?)
			ORDER BY next_attempt_at,id LIMIT ?`, timeText(now), timeText(now), limit)
		if err != nil {
			return fmt.Errorf("select due notification jobs: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			job, err := scanJob(rows)
			if err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		claimed := jobs[:0]
		for _, job := range jobs {
			result, err := tx.ExecContext(ctx, `UPDATE notification_jobs SET status='processing',lease_owner=?,lease_until=?,updated_at=?
				WHERE id=? AND status IN ('pending','retry') AND (lease_until IS NULL OR lease_until<=?)`, owner, timeText(now.Add(lease)), timeText(now), job.ID, timeText(now))
			if err != nil {
				return fmt.Errorf("claim notification job %d: %w", job.ID, err)
			}
			count, _ := result.RowsAffected()
			if count == 1 {
				job.Status = "processing"
				job.LeaseOwner = owner
				until := now.Add(lease)
				job.LeaseUntil = &until
				claimed = append(claimed, job)
			}
		}
		jobs = claimed
		return nil
	})
	return jobs, err
}

func (s *Store) CompleteNotificationJob(ctx context.Context, id int64, owner string, now time.Time) error {
	return s.finishJob(ctx, id, owner, now, "completed", "", 0)
}

func (s *Store) RetryNotificationJob(ctx context.Context, id int64, owner, message string, now time.Time, delay time.Duration) error {
	return s.finishJob(ctx, id, owner, now, "retry", message, delay)
}

func (s *Store) FailNotificationJob(ctx context.Context, id int64, owner, message string, now time.Time) error {
	return s.finishJob(ctx, id, owner, now, "failed", message, 0)
}

func (s *Store) finishJob(ctx context.Context, id int64, owner string, now time.Time, status, message string, delay time.Duration) error {
	next := now
	if delay > 0 {
		next = now.Add(delay)
	}
	return withTx(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE notification_jobs SET status=?,attempts=attempts+1,next_attempt_at=?,lease_owner='',lease_until=NULL,last_error=?,updated_at=?
			WHERE id=? AND status='processing' AND lease_owner=?`, status, timeText(next), message, timeText(now), id, owner)
		if err != nil {
			return fmt.Errorf("finish notification job: %w", err)
		}
		if err := requireAffected(result, "notification job"); err != nil {
			return err
		}
		if status == "completed" {
			if _, err := tx.ExecContext(ctx, `UPDATE guardian_notifications SET status='delivered' WHERE id=(SELECT notification_id FROM notification_jobs WHERE id=?)`, id); err != nil {
				return fmt.Errorf("mark notification delivered: %w", err)
			}
		} else if status == "failed" {
			if _, err := tx.ExecContext(ctx, `UPDATE guardian_notifications SET status='failed' WHERE id=(SELECT notification_id FROM notification_jobs WHERE id=?)`, id); err != nil {
				return fmt.Errorf("mark notification failed: %w", err)
			}
		}
		return nil
	})
}

func scanJob(row rowScanner) (repository.NotificationJob, error) {
	var job repository.NotificationJob
	var next, created, updated string
	var lease sql.NullString
	err := row.Scan(&job.ID, &job.NotificationID, &job.Status, &job.Attempts, &job.MaxAttempts, &next, &job.LeaseOwner, &lease, &job.LastError, &created, &updated)
	if err != nil {
		return job, fmt.Errorf("scan notification job: %w", err)
	}
	var parseErr error
	if job.NextAttemptAt, parseErr = parseTime(next); parseErr != nil {
		return job, parseErr
	}
	if job.LeaseUntil, parseErr = nullableTime(lease); parseErr != nil {
		return job, parseErr
	}
	if job.CreatedAt, parseErr = parseTime(created); parseErr != nil {
		return job, parseErr
	}
	if job.UpdatedAt, parseErr = parseTime(updated); parseErr != nil {
		return job, parseErr
	}
	return job, nil
}
