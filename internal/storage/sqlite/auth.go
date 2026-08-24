package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

func (s *Store) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	if user.Email == "" || user.DisplayName == "" || user.PasswordHash == "" || !user.Role.Valid() {
		return domain.User{}, &domain.FieldError{Field: "user", Problem: "email, display name, password hash and role are required"}
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO users(email,display_name,password_hash,role,active,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)`, user.Email, user.DisplayName, user.PasswordHash, user.Role, boolInt(user.Active), timeText(user.CreatedAt), timeText(user.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.User{}, fmt.Errorf("email already registered: %w", domain.ErrConflict)
		}
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}
	user.ID, err = result.LastInsertId()
	return user, err
}

func (s *Store) FindUserByEmail(ctx context.Context, email string) (domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,email,display_name,password_hash,role,active,created_at,updated_at
		FROM users WHERE email = ? COLLATE NOCASE`, strings.TrimSpace(email))
	return scanUser(row)
}

func (s *Store) CreateSession(ctx context.Context, session domain.Session) (domain.Session, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at,revoked_at,created_at)
		VALUES(?,?,?,?,?)`, session.UserID, session.TokenHash, timeText(session.ExpiresAt), nil, timeText(session.CreatedAt))
	if err != nil {
		return domain.Session{}, fmt.Errorf("insert session: %w", err)
	}
	session.ID, err = result.LastInsertId()
	return session, err
}

func (s *Store) SessionUserByTokenHash(ctx context.Context, hash string, now time.Time) (domain.Session, domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.token_hash,s.expires_at,s.revoked_at,s.created_at,
		u.id,u.email,u.display_name,u.password_hash,u.role,u.active,u.created_at,u.updated_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=?`, hash)
	var session domain.Session
	var user domain.User
	var expires, created, userCreated, userUpdated string
	var revoked sql.NullString
	var active int
	err := row.Scan(&session.ID, &session.UserID, &session.TokenHash, &expires, &revoked, &created,
		&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Role, &active, &userCreated, &userUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, domain.User{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return domain.Session{}, domain.User{}, fmt.Errorf("read session: %w", err)
	}
	if session.ExpiresAt, err = parseTime(expires); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if session.CreatedAt, err = parseTime(created); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if session.RevokedAt, err = nullableTime(revoked); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	user.Active = active == 1
	if user.CreatedAt, err = parseTime(userCreated); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if user.UpdatedAt, err = parseTime(userUpdated); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	if !user.Active || !session.Usable(now) {
		return domain.Session{}, domain.User{}, domain.ErrUnauthenticated
	}
	return session, user, nil
}

func (s *Store) RevokeSession(ctx context.Context, hash string, now time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, timeText(now), hash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return domain.ErrUnauthenticated
	}
	return nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ? OR revoked_at IS NOT NULL`, timeText(now))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return result.RowsAffected()
}

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (domain.User, error) {
	var user domain.User
	var active int
	var created, updated string
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Role, &active, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}
	user.Active = active == 1
	if user.CreatedAt, err = parseTime(created); err != nil {
		return domain.User{}, err
	}
	if user.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.User{}, err
	}
	return user, nil
}
