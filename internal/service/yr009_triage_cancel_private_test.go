package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

type cancelledTriageRepo struct{ repository.IncidentStore }

func (cancelledTriageRepo) TriageIncident(ctx context.Context, _ domain.Actor, _ int64, _ repository.TriageInput, _ int) (domain.Incident, error) {
	if err := ctx.Err(); err != nil {
		return domain.Incident{}, err
	}
	return domain.Incident{ID: 9, Status: domain.IncidentTriaged}, nil
}

type triageParticipants struct{ repository.Participants }

func TestCancelledTriageDoesNotAdvanceIncident(t *testing.T) {
	fixed := clock.Fixed{Time: time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)}
	s := service.NewIncidents(cancelledTriageRepo{}, triageParticipants{}, fixed, 4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	incident, err := s.Triage(ctx, domain.Actor{UserID: 8, Role: domain.RoleSafetyOfficer}, 9, service.TriageIncidentInput{
		Severity: domain.SeverityModerate, StopTraining: true, PublicGuidance: "clinical review", ExpectedVersion: 1,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled triage returned incident=%+v err=%v", incident, err)
	}
	if incident.ID != 0 {
		t.Fatalf("canceled triage advanced incident: %+v", incident)
	}
}
