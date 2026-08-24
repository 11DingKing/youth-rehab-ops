package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func (s *Store) CreateParticipant(ctx context.Context, participant domain.Participant) (domain.Participant, error) {
	if participant.PublicID == "" || strings.TrimSpace(participant.Name) == "" || participant.GuardianUserID <= 0 || participant.VenueID == "" {
		return domain.Participant{}, &domain.FieldError{Field: "participant", Problem: "public id, name, guardian and venue are required"}
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO participants(public_id,name,birth_date,guardian_user_id,venue_id,active,created_at)
		VALUES(?,?,?,?,?,?,?)`, participant.PublicID, participant.Name, timeText(participant.BirthDate), participant.GuardianUserID,
		participant.VenueID, boolInt(participant.Active), timeText(participant.CreatedAt))
	if err != nil {
		return domain.Participant{}, fmt.Errorf("insert participant: %w", err)
	}
	participant.ID, err = result.LastInsertId()
	return participant, err
}

func (s *Store) GetParticipant(ctx context.Context, id int64) (domain.Participant, error) {
	return scanParticipant(s.db.QueryRowContext(ctx, `SELECT id,public_id,name,birth_date,guardian_user_id,venue_id,active,created_at FROM participants WHERE id=?`, id))
}

func (s *Store) ListParticipants(ctx context.Context, guardianID int64, venue string, page domain.Page) (domain.PageResult[domain.Participant], error) {
	page = page.Normalize()
	result := domain.PageResult[domain.Participant]{Limit: page.Limit, Offset: page.Offset}
	where := " WHERE active=1"
	args := []any{}
	if guardianID > 0 {
		where += " AND guardian_user_id=?"
		args = append(args, guardianID)
	}
	if venue != "" {
		where += " AND venue_id=?"
		args = append(args, venue)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM participants"+where, args...).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count participants: %w", err)
	}
	args = append(args, page.Limit, page.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,public_id,name,birth_date,guardian_user_id,venue_id,active,created_at FROM participants`+where+` ORDER BY name,id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return result, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		participant, err := scanParticipant(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, participant)
	}
	return result, rows.Err()
}

func scanParticipant(row rowScanner) (domain.Participant, error) {
	var participant domain.Participant
	var birth, created string
	var active int
	err := row.Scan(&participant.ID, &participant.PublicID, &participant.Name, &birth, &participant.GuardianUserID, &participant.VenueID, &active, &created)
	if err == sql.ErrNoRows {
		return domain.Participant{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Participant{}, fmt.Errorf("scan participant: %w", err)
	}
	participant.Active = active == 1
	if participant.BirthDate, err = parseTime(birth); err != nil {
		return domain.Participant{}, err
	}
	if participant.CreatedAt, err = parseTime(created); err != nil {
		return domain.Participant{}, err
	}
	return participant, nil
}
