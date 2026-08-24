package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func (s *Store) AppendAudit(ctx context.Context, record audit.Record) error {
	return appendAudit(ctx, s.db, record)
}

func appendAudit(ctx context.Context, query querier, record audit.Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	_, err := query.ExecContext(ctx, `INSERT INTO audit_events(actor_id,actor_role,action,object_type,object_id,result,reason,request_id,metadata_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, record.ActorID, record.ActorRole, record.Action, record.ObjectType, record.ObjectID,
		record.Result, record.Reason, record.RequestID, record.MetadataJSON(), timeText(record.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s *Store) ListAudit(ctx context.Context, objectType, objectID string, page domain.Page) (domain.PageResult[audit.Record], error) {
	page = page.Normalize()
	result := domain.PageResult[audit.Record]{Limit: page.Limit, Offset: page.Offset}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE object_type=? AND object_id=?`, objectType, objectID).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count audit events: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor_id,actor_role,action,object_type,object_id,result,reason,request_id,metadata_json,created_at
		FROM audit_events WHERE object_type=? AND object_id=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`,
		objectType, objectID, page.Limit, page.Offset)
	if err != nil {
		return result, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record audit.Record
		var metadata, created string
		if err := rows.Scan(&record.ID, &record.ActorID, &record.ActorRole, &record.Action, &record.ObjectType,
			&record.ObjectID, &record.Result, &record.Reason, &record.RequestID, &metadata, &created); err != nil {
			return result, fmt.Errorf("scan audit event: %w", err)
		}
		parsed, err := parseTime(created)
		if err != nil {
			return result, err
		}
		record.CreatedAt = parsed
		record.Metadata = map[string]string{"raw": metadata}
		result.Items = append(result.Items, record)
	}
	return result, rows.Err()
}

func requireAffected(result sql.Result, entity string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read %s affected rows: %w", entity, err)
	}
	if count == 0 {
		return fmt.Errorf("%s not updated: %w", entity, domain.ErrConflict)
	}
	return nil
}

func mapNotFound(err error, operation string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", operation, domain.ErrNotFound)
	}
	return err
}
