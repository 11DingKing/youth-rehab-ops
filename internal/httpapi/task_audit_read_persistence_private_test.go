package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/audit"
	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/middleware"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

type controlledAuditStore struct {
	repository.Store
	appendErr error
	appended  chan audit.Record
}

func (s *controlledAuditStore) ListAudit(context.Context, string, string, domain.Page) (domain.PageResult[audit.Record], error) {
	return domain.PageResult[audit.Record]{Items: []audit.Record{{ID: 17, Action: "clearance.granted"}}, Total: 1}, nil
}

func (s *controlledAuditStore) AppendAudit(_ context.Context, record audit.Record) error {
	s.appended <- record
	return s.appendErr
}

type auditIncidentStore struct{ repository.IncidentStore }

func (auditIncidentStore) GetIncident(context.Context, int64) (domain.Incident, error) {
	return domain.Incident{ID: 1, PublicID: "inc-sensitive", ParticipantID: 2}, nil
}

type auditParticipantStore struct{ repository.Participants }

func (auditParticipantStore) GetParticipant(context.Context, int64) (domain.Participant, error) {
	return domain.Participant{ID: 2, GuardianUserID: 3, Active: true}, nil
}

func TestSensitiveAuditReadWaitsForAccessRecord(t *testing.T) {
	for _, test := range []struct {
		name       string
		appendErr  error
		wantStatus int
	}{
		{name: "audit persistence failure", appendErr: errors.New("audit disk unavailable"), wantStatus: http.StatusInternalServerError},
		{name: "audit persistence success", wantStatus: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &controlledAuditStore{appendErr: test.appendErr, appended: make(chan audit.Record, 1)}
			api := &API{
				Incidents: service.NewIncidents(auditIncidentStore{}, auditParticipantStore{}, clock.Fixed{Time: time.Now()}, 3),
				Store:     store,
				Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			request := httptest.NewRequest(http.MethodGet, "/api/incidents/1/audit", nil)
			request.SetPathValue("id", "1")
			ctx := middleware.WithIdentity(request.Context(), domain.User{ID: 9, Role: domain.RoleHealthProfessional}, "token")
			request = request.WithContext(middleware.WithRequestID(ctx, "audit-read-request"))
			response := httptest.NewRecorder()

			api.listAudit(response, request)
			if response.Code != test.wantStatus {
				t.Errorf("audit append error=%v status=%d body=%s", test.appendErr, response.Code, response.Body.String())
			}
			select {
			case record := <-store.appended:
				if record.Action != "audit.viewed" || record.ObjectID != "inc-sensitive" || record.RequestID != "audit-read-request" {
					t.Fatalf("access record=%+v", record)
				}
			case <-time.After(time.Second):
				t.Fatal("access audit was never attempted")
			}
		})
	}
}
