package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/security"
)

type AuthService struct {
	repo repository.Auth
	now  clock.Clock
	ttl  time.Duration
}

type LoginResult struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      domain.User `json:"user"`
}

func NewAuth(repo repository.Auth, now clock.Clock, ttl time.Duration) *AuthService {
	return &AuthService{repo: repo, now: now, ttl: ttl}
}

func (s *AuthService) BootstrapUser(ctx context.Context, email, displayName, password string, role domain.Role) (domain.User, error) {
	if !role.Valid() {
		return domain.User{}, &domain.FieldError{Field: "role", Problem: "is not supported"}
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return domain.User{}, &domain.FieldError{Field: "password", Problem: err.Error()}
	}
	now := s.now.Now()
	return s.repo.CreateUser(ctx, domain.User{Email: strings.TrimSpace(email), DisplayName: strings.TrimSpace(displayName),
		PasswordHash: hash, Role: role, Active: true, CreatedAt: now, UpdatedAt: now})
}

func (s *AuthService) EnsureBootstrapOfficer(ctx context.Context, email, password string) error {
	if email == "" {
		return nil
	}
	_, err := s.repo.FindUserByEmail(ctx, email)
	if err == nil {
		return nil
	}
	if err != domain.ErrNotFound {
		return fmt.Errorf("check bootstrap officer: %w", err)
	}
	_, err = s.BootstrapUser(ctx, email, "Bootstrap Safety Officer", password, domain.RoleSafetyOfficer)
	return err
}

func (s *AuthService) Login(ctx context.Context, email, password string) (LoginResult, error) {
	user, err := s.repo.FindUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil || !user.Active || !security.VerifyPassword(user.PasswordHash, password) {
		return LoginResult{}, domain.ErrUnauthenticated
	}
	token, hash, err := security.NewOpaqueToken()
	if err != nil {
		return LoginResult{}, err
	}
	now := s.now.Now()
	session, err := s.repo.CreateSession(ctx, domain.Session{UserID: user.ID, TokenHash: hash, ExpiresAt: now.Add(s.ttl), CreatedAt: now})
	if err != nil {
		return LoginResult{}, fmt.Errorf("create login session: %w", err)
	}
	user.PasswordHash = ""
	return LoginResult{Token: token, ExpiresAt: session.ExpiresAt, User: user}, nil
}

func (s *AuthService) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if strings.TrimSpace(token) == "" {
		return domain.User{}, domain.ErrUnauthenticated
	}
	_, user, err := s.repo.SessionUserByTokenHash(ctx, security.HashToken(token), s.now.Now())
	if err != nil {
		return domain.User{}, err
	}
	user.PasswordHash = ""
	return user, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return domain.ErrUnauthenticated
	}
	return s.repo.RevokeSession(ctx, security.HashToken(token), s.now.Now())
}

func (s *AuthService) PurgeExpired(ctx context.Context) (int64, error) {
	return s.repo.DeleteExpiredSessions(ctx, s.now.Now())
}
