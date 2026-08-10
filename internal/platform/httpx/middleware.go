package httpx

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

func RecoveryMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("request panic recovered", "request_id", RequestID(r.Context()), "panic", recovered, "stack", string(debug.Stack()))
				Error(w, r, http.StatusInternalServerError, "internal_error", "an internal error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

func LoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		capture := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)
		logger.Info("http request",
			"request_id", RequestID(r.Context()),
			"method", r.Method,
			"route", r.Pattern,
			"status", capture.status,
			"latency_ms", time.Since(started).Milliseconds(),
		)
	})
}

func CORSMiddleware(allowed []string, next http.Handler) http.Handler {
	allowedOrigins := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		allowedOrigins[origin] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowedOrigins[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		if r.Method == http.MethodOptions {
			if origin == "" {
				Error(w, r, http.StatusForbidden, "cors_forbidden", "origin is required")
				return
			}
			if _, ok := allowedOrigins[origin]; !ok {
				Error(w, r, http.StatusForbidden, "cors_forbidden", "origin is not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type ipRateLimiter struct {
	mu        sync.Mutex
	clients   map[string]*clientLimit
	rps       float64
	burst     float64
	lastSweep time.Time
}

const maxTrackedRateLimitClients = 10_000

type clientLimit struct {
	tokens float64
	seen   time.Time
}

func NewRateLimitMiddleware(requestsPerSecond, burst int, next http.Handler) http.Handler {
	limiter := &ipRateLimiter{
		clients: make(map[string]*clientLimit),
		rps:     float64(requestsPerSecond),
		burst:   float64(burst),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r)
		if !limiter.allow(key, time.Now()) {
			w.Header().Set("Retry-After", "1")
			Error(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *ipRateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > time.Minute {
		for client, state := range l.clients {
			if now.Sub(state.seen) > 2*time.Minute {
				delete(l.clients, client)
			}
		}
		l.lastSweep = now
	}
	state, ok := l.clients[key]
	if !ok {
		if len(l.clients) >= maxTrackedRateLimitClients {
			l.evictOldest()
		}
		l.clients[key] = &clientLimit{tokens: l.burst - 1, seen: now}
		return true
	}
	state.tokens += now.Sub(state.seen).Seconds() * l.rps
	if state.tokens > l.burst {
		state.tokens = l.burst
	}
	state.seen = now
	if state.tokens < 1 {
		return false
	}
	state.tokens--
	return true
}

func (l *ipRateLimiter) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, state := range l.clients {
		if oldestKey == "" || state.seen.Before(oldest) {
			oldestKey = key
			oldest = state.seen
		}
	}
	if oldestKey != "" {
		delete(l.clients, oldestKey)
	}
}

func RequestTimeoutMiddleware(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		buffer := &bufferedWriter{header: make(http.Header), status: http.StatusOK}
		done := make(chan struct{})
		go func() {
			defer func() {
				if recover() != nil {
					Error(buffer, r, http.StatusInternalServerError, "internal_error", "an internal error occurred")
				}
				close(done)
			}()
			next.ServeHTTP(buffer, r.WithContext(ctx))
		}()
		select {
		case <-done:
			buffer.copyTo(w)
		case <-ctx.Done():
			Error(w, r, http.StatusGatewayTimeout, "request_timeout", "request timed out")
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != http.StatusOK {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

type bufferedWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *bufferedWriter) Header() http.Header { return w.header }

func (w *bufferedWriter) WriteHeader(status int) {
	if w.status != http.StatusOK {
		return
	}
	w.status = status
}

func (w *bufferedWriter) Write(body []byte) (int, error) {
	return w.body.Write(body)
}

func (w *bufferedWriter) copyTo(target http.ResponseWriter) {
	for key, values := range w.header {
		for _, value := range values {
			target.Header().Add(key, value)
		}
	}
	target.WriteHeader(w.status)
	_, _ = target.Write(w.body.Bytes())
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
