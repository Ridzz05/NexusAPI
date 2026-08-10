package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Ridzz05/NexusAPI/internal/access"
	"github.com/Ridzz05/NexusAPI/internal/attendance"
	"github.com/Ridzz05/NexusAPI/internal/config"
	"github.com/Ridzz05/NexusAPI/internal/integration/loyalfitness"
	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
	"github.com/Ridzz05/NexusAPI/openapi"
)

type Dependencies struct {
	Logger        *slog.Logger
	Authenticator access.Authenticator
	LoyalFitness  loyalfitness.Reader
	Attendance    attendance.Service
	Readiness     func(context.Context) error
}

type Server struct {
	cfg  config.Config
	deps Dependencies
}

func New(cfg config.Config, deps Dependencies) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Server{cfg: cfg, deps: deps}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	protected := func(handler http.HandlerFunc) http.Handler {
		return access.Require(s.deps.Authenticator, http.HandlerFunc(handler))
	}
	register := func(method, path, allow string, handler http.Handler) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			s.methodNotAllowed(w, r, allow)
		})
		mux.Handle(method+" "+path, handler)
	}
	registerPrivate := func(method, path, allow string, handler http.HandlerFunc) {
		mux.Handle(path, protected(func(w http.ResponseWriter, r *http.Request) {
			s.methodNotAllowed(w, r, allow)
		}))
		mux.Handle(method+" "+path, protected(handler))
	}
	register(http.MethodGet, "/healthz", http.MethodGet, http.HandlerFunc(s.health))
	register(http.MethodGet, "/readyz", http.MethodGet, http.HandlerFunc(s.ready))
	register(http.MethodGet, "/openapi.yaml", http.MethodGet, http.HandlerFunc(s.openapi))
	registerPrivate(http.MethodGet, "/api/v1/users/me", http.MethodGet, s.currentUser)
	registerPrivate(http.MethodGet, "/api/v1/mobile/dashboard", http.MethodGet, s.mobileDashboard)
	registerPrivate(http.MethodGet, "/api/v1/members", http.MethodGet, s.members)
	registerPrivate(http.MethodGet, "/api/v1/pt-sessions", http.MethodGet, s.ptSessions)
	registerPrivate(http.MethodGet, "/api/v1/finance/summary", http.MethodGet, s.financeSummary)
	registerPrivate(http.MethodPost, "/api/v1/attendance/check-in", http.MethodPost, s.checkIn)
	registerPrivate(http.MethodPost, "/api/v1/attendance/check-out", http.MethodPost, s.checkOut)
	registerPrivate(http.MethodPost, "/api/v1/devices/heartbeat", http.MethodPost, s.heartbeat)
	mux.HandleFunc("/", s.notFound)

	return httpx.RequestIDMiddleware(
		httpx.SecurityHeadersMiddleware(
			httpx.RecoveryMiddleware(s.deps.Logger,
				httpx.LoggingMiddleware(s.deps.Logger,
					httpx.CORSMiddleware(s.cfg.CORSAllowedOrigins,
						httpx.NewRateLimitMiddleware(s.cfg.RateLimitRPS, s.cfg.RateLimitBurst,
							httpx.RequestTimeoutMiddleware(s.cfg.HTTPRequestTimeout,
								httpx.RecoveryMiddleware(s.deps.Logger, mux)),
						),
					),
				),
			),
		),
	)
}

func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Addr:         s.cfg.HTTPAddr,
		Handler:      s.Handler(),
		ReadTimeout:  s.cfg.HTTPReadTimeout,
		WriteTimeout: s.cfg.HTTPWriteTimeout,
		IdleTimeout:  s.cfg.HTTPIdleTimeout,
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, r, http.StatusOK, map[string]string{"status": "ok"}, nil)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.deps.Readiness == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "not_ready", "readiness checks are not configured")
		return
	}
	if err := s.deps.Readiness(r.Context()); err != nil {
		s.deps.Logger.Error("readiness check failed", "request_id", httpx.RequestID(r.Context()), "error", err)
		httpx.Error(w, r, http.StatusServiceUnavailable, "not_ready", "service dependencies are not ready")
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]string{"status": "ready"}, nil)
}

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapi.Spec)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotFound, "not_found", "the requested resource was not found")
}

func (s *Server) methodNotAllowed(w http.ResponseWriter, r *http.Request, allow string) {
	w.Header().Set("Allow", allow)
	httpx.Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "the HTTP method is not allowed for this resource")
}

func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := access.PrincipalFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}
	httpx.JSON(w, r, http.StatusOK, principal, nil)
}

func (s *Server) mobileDashboard(w http.ResponseWriter, r *http.Request) {
	principal := mustPrincipal(w, r)
	if principal.Subject == "" {
		return
	}
	if s.deps.LoyalFitness == nil {
		s.integrationUnavailable(w, r)
		return
	}
	dashboard, err := s.deps.LoyalFitness.MobileDashboard(r.Context(), actorFromPrincipal(principal))
	if err != nil {
		s.integrationError(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, dashboard, nil)
}

func (s *Server) members(w http.ResponseWriter, r *http.Request) {
	principal := mustPrincipal(w, r)
	if principal.Subject == "" {
		return
	}
	page, err := httpx.ParsePageRequest(r)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	query, err := httpx.BoundedQueryParam(r, "q", 100)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	status, err := httpx.BoundedQueryParam(r, "status", 40)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	if s.deps.LoyalFitness == nil {
		s.integrationUnavailable(w, r)
		return
	}
	result, err := s.deps.LoyalFitness.FindMembers(r.Context(), actorFromPrincipal(principal), loyalfitness.MemberFilter{Query: query, Status: status}, page)
	if err != nil {
		s.integrationError(w, r, err)
		return
	}
	if len(result.Items) > page.Limit {
		s.deps.Logger.Error("Loyal Fitness adapter returned an unbounded member page", "request_id", httpx.RequestID(r.Context()), "items", len(result.Items), "limit", page.Limit)
		s.integrationError(w, r, errors.New("member page exceeded requested limit"))
		return
	}
	httpx.JSON(w, r, http.StatusOK, result.Items, &httpx.Meta{NextCursor: result.NextCursor, HasMore: result.HasMore})
}

func (s *Server) ptSessions(w http.ResponseWriter, r *http.Request) {
	principal := mustPrincipal(w, r)
	if principal.Subject == "" {
		return
	}
	page, err := httpx.ParsePageRequest(r)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}
	status, err := httpx.BoundedQueryParam(r, "status", 40)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	from, err := httpx.BoundedQueryParam(r, "from", 40)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	to, err := httpx.BoundedQueryParam(r, "to", 40)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	if s.deps.LoyalFitness == nil {
		s.integrationUnavailable(w, r)
		return
	}
	result, err := s.deps.LoyalFitness.FindPTSessions(r.Context(), actorFromPrincipal(principal), loyalfitness.PTSessionFilter{Status: status, From: from, To: to}, page)
	if err != nil {
		s.integrationError(w, r, err)
		return
	}
	if len(result.Items) > page.Limit {
		s.deps.Logger.Error("Loyal Fitness adapter returned an unbounded PT session page", "request_id", httpx.RequestID(r.Context()), "items", len(result.Items), "limit", page.Limit)
		s.integrationError(w, r, errors.New("PT session page exceeded requested limit"))
		return
	}
	httpx.JSON(w, r, http.StatusOK, result.Items, &httpx.Meta{NextCursor: result.NextCursor, HasMore: result.HasMore})
}

func (s *Server) financeSummary(w http.ResponseWriter, r *http.Request) {
	principal := mustPrincipal(w, r)
	if principal.Subject == "" {
		return
	}
	if s.deps.LoyalFitness == nil {
		s.integrationUnavailable(w, r)
		return
	}
	result, err := s.deps.LoyalFitness.FinanceSummary(r.Context(), actorFromPrincipal(principal))
	if err != nil {
		s.integrationError(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, result, nil)
}

func (s *Server) checkIn(w http.ResponseWriter, r *http.Request) {
	var command attendance.CheckCommand
	if err := httpx.DecodeJSON(w, r, &command, 64<<10); err != nil || !validCheckCommand(command) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_attendance_command", "the attendance command is invalid")
		return
	}
	if s.deps.Attendance == nil {
		s.attendanceUnavailable(w, r)
		return
	}
	event, err := s.deps.Attendance.CheckIn(r.Context(), attendanceActor(r), command)
	if s.writeAttendanceError(w, r, err) {
		return
	}
	httpx.JSON(w, r, http.StatusCreated, event, nil)
}

func (s *Server) checkOut(w http.ResponseWriter, r *http.Request) {
	var command attendance.CheckCommand
	if err := httpx.DecodeJSON(w, r, &command, 64<<10); err != nil || !validCheckCommand(command) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_attendance_command", "the attendance command is invalid")
		return
	}
	if s.deps.Attendance == nil {
		s.attendanceUnavailable(w, r)
		return
	}
	event, err := s.deps.Attendance.CheckOut(r.Context(), attendanceActor(r), command)
	if s.writeAttendanceError(w, r, err) {
		return
	}
	httpx.JSON(w, r, http.StatusCreated, event, nil)
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var command attendance.HeartbeatCommand
	if err := httpx.DecodeJSON(w, r, &command, 16<<10); err != nil || !validHeartbeatCommand(command) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_heartbeat", "the device heartbeat is invalid")
		return
	}
	if s.deps.Attendance == nil {
		s.attendanceUnavailable(w, r)
		return
	}
	event, err := s.deps.Attendance.Heartbeat(r.Context(), attendanceActor(r), command)
	if s.writeAttendanceError(w, r, err) {
		return
	}
	httpx.JSON(w, r, http.StatusCreated, event, nil)
}

func validCheckCommand(command attendance.CheckCommand) bool {
	return attendance.ValidateCheckCommand(command) == nil
}

func validHeartbeatCommand(command attendance.HeartbeatCommand) bool {
	return attendance.ValidateHeartbeatCommand(command) == nil
}

func attendanceActor(r *http.Request) attendance.Actor {
	principal, _ := access.PrincipalFromContext(r.Context())
	return attendance.Actor{Subject: principal.Subject, Roles: append([]string(nil), principal.Roles...)}
}

func (s *Server) writeAttendanceError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, attendance.ErrInvalidCommand):
		httpx.Error(w, r, http.StatusBadRequest, "invalid_attendance_command", "the attendance command is invalid")
	case errors.Is(err, attendance.ErrNotAuthorized):
		httpx.Error(w, r, http.StatusForbidden, "forbidden", "you do not have permission to perform this action")
	case errors.Is(err, attendance.ErrConflict):
		httpx.Error(w, r, http.StatusConflict, "attendance_conflict", "the attendance state does not allow this action")
	default:
		s.deps.Logger.Error("attendance service failed", "request_id", httpx.RequestID(r.Context()), "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
	return true
}

func (s *Server) attendanceUnavailable(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotImplemented, "attendance_unavailable", "the attendance service is not configured")
}

func mustPrincipal(w http.ResponseWriter, r *http.Request) access.Principal {
	principal, ok := access.PrincipalFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
	return principal
}

func actorFromPrincipal(principal access.Principal) loyalfitness.Actor {
	return loyalfitness.Actor{Subject: principal.Subject, Roles: append([]string(nil), principal.Roles...)}
}

func (s *Server) integrationUnavailable(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotImplemented, "integration_unavailable", "the Loyal Fitness read adapter is not configured")
}

func (s *Server) integrationError(w http.ResponseWriter, r *http.Request, err error) {
	s.deps.Logger.Error("Loyal Fitness read adapter failed", "request_id", httpx.RequestID(r.Context()), "error", err)
	httpx.Error(w, r, http.StatusBadGateway, "integration_error", "the upstream read service is unavailable")
}
