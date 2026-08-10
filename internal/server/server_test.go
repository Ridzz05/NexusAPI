package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ridzz05/NexusAPI/internal/access"
	"github.com/Ridzz05/NexusAPI/internal/attendance"
	"github.com/Ridzz05/NexusAPI/internal/config"
	"github.com/Ridzz05/NexusAPI/internal/integration/loyalfitness"
	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
)

func testConfig() config.Config {
	return config.Config{
		AppEnv:              "test",
		HTTPAddr:            ":8080",
		JWTSecret:           "01234567890123456789012345678901",
		DatabaseURL:         "postgres://localhost/nexus",
		RedisURL:            "redis://localhost:6379",
		DBMaxConns:          10,
		DBMinConns:          2,
		RateLimitRPS:        100,
		RateLimitBurst:      100,
		HTTPReadTimeout:     10 * time.Second,
		HTTPWriteTimeout:    10 * time.Second,
		HTTPIdleTimeout:     10 * time.Second,
		HTTPRequestTimeout:  10 * time.Second,
		ShutdownTimeout:     10 * time.Second,
		LoyalFitnessTimeout: 5 * time.Second,
		ReadCacheTTL:        30 * time.Second,
	}
}

func TestHealthIncludesRequestIDAndStableEnvelope(t *testing.T) {
	api := New(testConfig(), Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request ID header")
	}
	var body struct {
		Data      map[string]string `json:"data"`
		RequestID string            `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["status"] != "ok" || body.RequestID == "" {
		t.Fatalf("unexpected health response: %s", recorder.Body.String())
	}
}

func TestPrivateRoutesRejectMissingCredentials(t *testing.T) {
	api := New(testConfig(), Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unexpected error response: %s", recorder.Body.String())
	}
}

func TestUnknownRoutesUseStandardErrorEnvelope(t *testing.T) {
	api := New(testConfig(), Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"not_found"`) {
		t.Fatalf("unexpected not-found response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestUnconfiguredIntegrationReturnsExplicitError(t *testing.T) {
	api := New(testConfig(), Dependencies{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authenticator: testAuthenticator{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/members?limit=10", nil)
	req.Header.Set("Authorization", "Bearer accepted-in-test")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"integration_unavailable"`) {
		t.Fatalf("unexpected error response: %s", recorder.Body.String())
	}
}

func TestMemberReadReceivesExplicitActorScope(t *testing.T) {
	reader := &recordingReader{}
	api := New(testConfig(), Dependencies{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authenticator: scopedAuthenticator{},
		LoyalFitness:  reader,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/members?limit=2&q=alice&status=active", nil)
	req.Header.Set("Authorization", "Bearer accepted-in-test")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || reader.actor.Subject != "user-1" || len(reader.actor.Roles) != 1 || reader.actor.Roles[0] != "member" {
		t.Fatalf("unexpected scoped read: status=%d actor=%#v body=%s", recorder.Code, reader.actor, recorder.Body.String())
	}
	if reader.filter.Query != "alice" || reader.filter.Status != "active" || reader.page.Limit != 2 {
		t.Fatalf("unexpected read arguments: filter=%#v page=%#v", reader.filter, reader.page)
	}
}

func TestMemberFilterIsBoundedBeforeAdapter(t *testing.T) {
	api := New(testConfig(), Dependencies{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authenticator: scopedAuthenticator{},
		LoyalFitness:  &recordingReader{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/members?q="+strings.Repeat("x", 101), nil)
	req.Header.Set("Authorization", "Bearer accepted-in-test")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_filter"`) {
		t.Fatalf("unexpected filter response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAttendanceCommandIsStrictAndDoesNotFabricateWrites(t *testing.T) {
	api := New(testConfig(), Dependencies{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authenticator: scopedAuthenticator{},
	})
	validBody := `{"qr_token":"qr-1","occurred_at":"2026-08-10T10:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attendance/check-in", strings.NewReader(validBody))
	req.Header.Set("Authorization", "Bearer accepted-in-test")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotImplemented || !strings.Contains(recorder.Body.String(), `"code":"attendance_unavailable"`) {
		t.Fatalf("unexpected unconfigured attendance response: %d %s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/attendance/check-in", strings.NewReader(`{"qr_token":"qr-1","occurred_at":"2026-08-10T10:00:00Z","extra":true}`))
	req.Header.Set("Authorization", "Bearer accepted-in-test")
	recorder = httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"invalid_attendance_command"`) {
		t.Fatalf("unexpected invalid attendance response: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAttendanceServiceReceivesAuthenticatedActor(t *testing.T) {
	service := &recordingAttendance{}
	api := New(testConfig(), Dependencies{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Authenticator: scopedAuthenticator{},
		Attendance:    service,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/attendance/check-in", strings.NewReader(`{"qr_token":"qr-1","occurred_at":"2026-08-10T10:00:00Z"}`))
	req.Header.Set("Authorization", "Bearer accepted-in-test")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated || service.actor.Subject != "user-1" || service.command.QRToken != "qr-1" {
		t.Fatalf("unexpected attendance call: status=%d actor=%#v command=%#v body=%s", recorder.Code, service.actor, service.command, recorder.Body.String())
	}
}

type testAuthenticator struct{}

func (testAuthenticator) Authenticate(string, time.Time) (access.Principal, error) {
	return access.Principal{Subject: "user-1"}, nil
}

type scopedAuthenticator struct{}

func (scopedAuthenticator) Authenticate(string, time.Time) (access.Principal, error) {
	return access.Principal{Subject: "user-1", Roles: []string{"member"}}, nil
}

type recordingReader struct {
	actor  loyalfitness.Actor
	filter loyalfitness.MemberFilter
	page   httpx.PageRequest
}

type recordingAttendance struct {
	actor   attendance.Actor
	command attendance.CheckCommand
}

func (s *recordingAttendance) CheckIn(_ context.Context, actor attendance.Actor, command attendance.CheckCommand) (attendance.Event, error) {
	s.actor, s.command = actor, command
	return attendance.Event{ID: "event-1", Kind: "check_in", DeviceID: actor.Subject, OccurredAt: command.OccurredAt}, nil
}

func (*recordingAttendance) CheckOut(context.Context, attendance.Actor, attendance.CheckCommand) (attendance.Event, error) {
	return attendance.Event{}, nil
}

func (*recordingAttendance) Heartbeat(context.Context, attendance.Actor, attendance.HeartbeatCommand) (attendance.Event, error) {
	return attendance.Event{}, nil
}

func (r *recordingReader) FindMembers(_ context.Context, actor loyalfitness.Actor, filter loyalfitness.MemberFilter, page httpx.PageRequest) (loyalfitness.MembersPage, error) {
	r.actor, r.filter, r.page = actor, filter, page
	return loyalfitness.MembersPage{Items: []loyalfitness.Member{{ID: "m-1"}}}, nil
}

func (*recordingReader) FindPTSessions(context.Context, loyalfitness.Actor, loyalfitness.PTSessionFilter, httpx.PageRequest) (loyalfitness.PTSessionsPage, error) {
	return loyalfitness.PTSessionsPage{}, nil
}

func (*recordingReader) FinanceSummary(context.Context, loyalfitness.Actor) (loyalfitness.FinanceSummary, error) {
	return loyalfitness.FinanceSummary{}, nil
}

func (*recordingReader) MobileDashboard(context.Context, loyalfitness.Actor) (loyalfitness.MobileDashboard, error) {
	return loyalfitness.MobileDashboard{}, nil
}
