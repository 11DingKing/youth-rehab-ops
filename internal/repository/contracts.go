package repository

import (
	"context"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

type Auth interface {
	CreateUser(context.Context, domain.User) (domain.User, error)
	FindUserByEmail(context.Context, string) (domain.User, error)
	CreateSession(context.Context, domain.Session) (domain.Session, error)
	SessionUserByTokenHash(context.Context, string, time.Time) (domain.Session, domain.User, error)
	RevokeSession(context.Context, string, time.Time) error
	DeleteExpiredSessions(context.Context, time.Time) (int64, error)
}

type Participants interface {
	CreateParticipant(context.Context, domain.Participant) (domain.Participant, error)
	GetParticipant(context.Context, int64) (domain.Participant, error)
	ListParticipants(context.Context, int64, string, domain.Page) (domain.PageResult[domain.Participant], error)
}

type IncidentFilter struct {
	ParticipantID int64
	Status        domain.IncidentStatus
	VenueID       string
	Page          domain.Page
}

type IncidentCorrection struct {
	BodyArea    string
	OccurredAt  time.Time
	Description string
	Reason      string
	Expected    int64
}

type TriageInput struct {
	Severity       domain.Severity
	StopTraining   bool
	PublicGuidance string
	ClinicalNotes  string
	Expected       int64
}

type IncidentStore interface {
	CreateIncident(context.Context, domain.Actor, domain.Incident, string, string) (domain.Incident, error)
	GetIncident(context.Context, int64) (domain.Incident, error)
	ListIncidents(context.Context, IncidentFilter) (domain.PageResult[domain.Incident], error)
	CorrectIncident(context.Context, domain.Actor, int64, IncidentCorrection) (domain.Incident, error)
	TriageIncident(context.Context, domain.Actor, int64, TriageInput, int) (domain.Incident, error)
	ReleaseTrainingBlock(context.Context, domain.Actor, int64, string) error
}

type CareStore interface {
	CreateReferral(context.Context, domain.Actor, domain.Referral) (domain.Referral, error)
	GetReferral(context.Context, int64) (domain.Referral, error)
	AcceptReferral(context.Context, domain.Actor, int64, int64) (domain.Referral, error)
	ReturnReferral(context.Context, domain.Actor, int64, int64, string) (domain.Referral, error)
	CreatePlan(context.Context, domain.Actor, domain.RehabPlan) (domain.RehabPlan, error)
	PublishPlanVersion(context.Context, domain.Actor, int64, int64, domain.RehabPlanVersion) (domain.RehabPlanVersion, error)
	RecordFollowUp(context.Context, domain.Actor, domain.FollowUp) (domain.FollowUp, error)
	GetFollowUp(context.Context, int64) (domain.FollowUp, error)
	GrantClearance(context.Context, domain.Actor, domain.Clearance) (domain.Clearance, error)
	CreateClearanceRecord(context.Context, domain.Actor, domain.Clearance) (domain.Clearance, error)
	ReleaseClearanceTrainingBlock(context.Context, domain.Actor, int64, time.Time) error
	LatestClearance(context.Context, int64, time.Time) (domain.Clearance, error)
}

type ScheduleStore interface {
	AttemptSchedule(context.Context, domain.Actor, domain.ScheduleAttempt) (domain.ScheduleAttempt, error)
	GrantOverride(context.Context, domain.Actor, domain.Override) (domain.Override, error)
	RevokeOverride(context.Context, domain.Actor, int64, string) error
}

type NotificationJob struct {
	ID             int64
	NotificationID int64
	Status         string
	Attempts       int
	MaxAttempts    int
	NextAttemptAt  time.Time
	LeaseOwner     string
	LeaseUntil     *time.Time
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Notifications interface {
	ClaimNotificationJobs(context.Context, string, int, time.Time, time.Duration) ([]NotificationJob, error)
	CompleteNotificationJob(context.Context, int64, string, time.Time) error
	RetryNotificationJob(context.Context, int64, string, string, time.Time, time.Duration) error
	FailNotificationJob(context.Context, int64, string, string, time.Time) error
}

type Audits interface {
	AppendAudit(context.Context, audit.Record) error
	ListAudit(context.Context, string, string, domain.Page) (domain.PageResult[audit.Record], error)
}

type Health interface {
	Ping(context.Context) error
	Close() error
}

type Store interface {
	Auth
	Participants
	IncidentStore
	CareStore
	ScheduleStore
	Notifications
	Audits
	Health
}
