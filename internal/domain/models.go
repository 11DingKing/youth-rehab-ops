package domain

import "time"

type User struct {
	ID           int64
	Email        string
	DisplayName  string
	PasswordHash string
	Role         Role
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Session struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (s Session) Usable(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

type Participant struct {
	ID             int64
	PublicID       string
	Name           string
	BirthDate      time.Time
	GuardianUserID int64
	VenueID        string
	Active         bool
	CreatedAt      time.Time
}

type Actor struct {
	UserID    int64
	Role      Role
	RequestID string
}

type Page struct {
	Limit  int
	Offset int
}

func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = 25
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

type PageResult[T any] struct {
	Items  []T
	Total  int
	Limit  int
	Offset int
}
