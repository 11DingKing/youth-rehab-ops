package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/clock"
	"github.com/11DingKing/youth-rehab-ops/internal/domain"
	"github.com/11DingKing/youth-rehab-ops/internal/service"
	"github.com/11DingKing/youth-rehab-ops/internal/storage/sqlite"
)

type apiFixture struct {
	handler http.Handler
	store   *sqlite.Store
	auth    *service.AuthService
	coach   domain.User
}

func newAPIFixture(t *testing.T) apiFixture {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := clock.Fixed{Time: time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)}
	auth := service.NewAuth(store, now, time.Hour)
	coach, err := auth.BootstrapUser(context.Background(), "coach@example.test", "Coach", "safe password value", domain.RoleCoach)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := &API{Auth: auth, Incidents: service.NewIncidents(store, store, now, 3), Care: service.NewCare(store, now),
		Schedule: service.NewSchedule(store, now), Store: store, Logger: logger}
	return apiFixture{handler: api.Handler(), store: store, auth: auth, coach: coach}
}

func jsonRequest(t *testing.T, method, path, token string, body any) *http.Request {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestHealthAndReadinessExposeDependencyState(t *testing.T) {
	fixture := newAPIFixture(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		fixture.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "status") {
			t.Errorf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Request-ID") == "" {
			t.Errorf("%s omitted request id", path)
		}
	}
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "not_ready") {
		t.Fatalf("closed store readiness status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLoginAndLogoutHTTPContractRevokesBearer(t *testing.T) {
	fixture := newAPIFixture(t)
	login := httptest.NewRecorder()
	fixture.handler.ServeHTTP(login, jsonRequest(t, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": fixture.coach.Email, "password": "safe password value"}))
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	var result service.LoginResult
	if err := json.Unmarshal(login.Body.Bytes(), &result); err != nil || result.Token == "" {
		t.Fatalf("decode login: token=%q err=%v", result.Token, err)
	}
	logout := httptest.NewRecorder()
	fixture.handler.ServeHTTP(logout, jsonRequest(t, http.MethodPost, "/api/auth/logout", result.Token, nil))
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	after := httptest.NewRecorder()
	fixture.handler.ServeHTTP(after, jsonRequest(t, http.MethodGet, "/api/incidents/1", result.Token, nil))
	if after.Code != http.StatusUnauthorized || !strings.Contains(after.Body.String(), "unauthenticated") {
		t.Fatalf("revoked token status=%d body=%s", after.Code, after.Body.String())
	}
}

func TestProtectedRouteReturnsStableErrorWithRequestID(t *testing.T) {
	fixture := newAPIFixture(t)
	request := jsonRequest(t, http.MethodGet, "/api/incidents/1", "", nil)
	request.Header.Set("X-Request-ID", "client-request-42")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "unauthenticated" || body.Error.RequestID != "client-request-42" {
		t.Fatalf("error body=%+v", body)
	}
}

func TestLoginRejectsUnknownJSONAndWrongContentType(t *testing.T) {
	fixture := newAPIFixture(t)
	unknown := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"x","password":"y","extra":true}`))
	unknown.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, unknown)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "validation_failed") {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}
	plain := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"x"}`))
	plain.Header.Set("Content-Type", "text/plain")
	response = httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, plain)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("content type status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestInvalidPathIDMapsToValidationErrorBeforeRepository(t *testing.T) {
	fixture := newAPIFixture(t)
	login, err := fixture.auth.Login(context.Background(), fixture.coach.Email, "safe password value")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/incidents/zero", "/api/incidents/-1", "/api/incidents/0"} {
		response := httptest.NewRecorder()
		fixture.handler.ServeHTTP(response, jsonRequest(t, http.MethodGet, path, login.Token, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "validation_failed") {
			t.Errorf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
