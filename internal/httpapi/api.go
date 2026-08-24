package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/middleware"
	"github.com/11DingKing/youth-rehab-ops/internal/repository"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
)

type API struct {
	Auth      *service.AuthService
	Incidents *service.IncidentService
	Care      *service.CareService
	Schedule  *service.ScheduleService
	Store     repository.Store
	Logger    *slog.Logger
}

type errorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func (a *API) Handler() http.Handler {
	public := http.NewServeMux()
	public.HandleFunc("GET /healthz", a.health)
	public.HandleFunc("GET /readyz", a.ready)
	public.HandleFunc("POST /api/auth/login", a.login)

	protected := http.NewServeMux()
	protected.HandleFunc("POST /api/auth/logout", a.logout)
	protected.HandleFunc("GET /api/incidents/{id}", a.getIncident)
	protected.HandleFunc("POST /api/incidents", a.reportIncident)
	protected.HandleFunc("POST /api/incidents/{id}/corrections", a.correctIncident)
	protected.HandleFunc("POST /api/incidents/{id}/triage", a.triageIncident)
	protected.HandleFunc("POST /api/incidents/{id}/referrals", a.createReferral)
	protected.HandleFunc("POST /api/referrals/{id}/accept", a.acceptReferral)
	protected.HandleFunc("POST /api/referrals/{id}/return", a.returnReferral)
	protected.HandleFunc("POST /api/referrals/{id}/plans", a.createPlan)
	protected.HandleFunc("POST /api/plans/{id}/versions", a.publishPlan)
	protected.HandleFunc("POST /api/plans/{id}/followups", a.recordFollowUp)
	protected.HandleFunc("POST /api/incidents/{id}/clearances", a.grantClearance)
	protected.HandleFunc("POST /api/schedule-attempts", a.attemptSchedule)
	protected.HandleFunc("POST /api/incidents/{id}/overrides", a.grantOverride)
	protected.HandleFunc("GET /api/incidents/{id}/audit", a.listAudit)

	authenticated := middleware.Authenticate(func(r *http.Request, token string) (domain.User, error) {
		return a.Auth.Authenticate(r.Context(), token)
	}, a.writeError, protected)
	public.Handle("/api/", authenticated)
	return middleware.Recover(a.Logger, middleware.RequestContext(a.Logger, public))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return &domain.FieldError{Field: "Content-Type", Problem: "must be application/json"}
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return &domain.FieldError{Field: "body", Problem: err.Error()}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return &domain.FieldError{Field: "body", Problem: "must contain one JSON value"}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := errorMapping(err)
	body := errorBody{}
	body.Error.Code = code
	body.Error.Message = message
	body.Error.RequestID = middleware.RequestID(r.Context())
	writeJSON(w, status, body)
}

func errorMapping(err error) (int, string, string) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, "validation_failed", err.Error()
	case errors.Is(err, domain.ErrUnauthenticated):
		return http.StatusUnauthorized, "unauthenticated", "authentication is required or has expired"
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, "forbidden", "the current role cannot perform this operation"
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, "not_found", "the requested record was not found"
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, domain.ErrExpired):
		return http.StatusConflict, "expired", err.Error()
	case errors.Is(err, domain.ErrUnavailable):
		return http.StatusServiceUnavailable, "unavailable", "a required dependency is unavailable"
	default:
		return http.StatusInternalServerError, "internal", "internal server error"
	}
}

func actorFrom(r *http.Request) (domain.Actor, error) {
	user, ok := middleware.User(r.Context())
	if !ok {
		return domain.Actor{}, domain.ErrUnauthenticated
	}
	return domain.Actor{UserID: user.ID, Role: user.Role, RequestID: middleware.RequestID(r.Context())}, nil
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, &domain.FieldError{Field: name, Problem: "must be a positive integer"}
	}
	return id, nil
}
