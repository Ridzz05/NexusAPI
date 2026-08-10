package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestRateLimiterBoundsTrackedClients(t *testing.T) {
	limiter := &ipRateLimiter{
		clients: make(map[string]*clientLimit),
		rps:     1,
		burst:   1,
	}
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < maxTrackedRateLimitClients+100; i++ {
		if !limiter.allow(string(rune(i+1)), base) {
			t.Fatalf("first request for client %d was rejected", i)
		}
	}
	if len(limiter.clients) > maxTrackedRateLimitClients {
		t.Fatalf("tracked clients exceeded bound: %d", len(limiter.clients))
	}
}

func TestCORSRejectsUnknownOriginsAndHandlesPreflight(t *testing.T) {
	handler := CORSMiddleware([]string{"https://app.example"}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	preflight := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	preflight.Header.Set("Origin", "https://app.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, preflight)
	if recorder.Code != http.StatusNoContent || recorder.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("unexpected allowed preflight: %d headers=%v", recorder.Code, recorder.Header())
	}
	unknown := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	unknown.Header.Set("Origin", "https://evil.example")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, unknown)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected unknown origin to be rejected, got %d", recorder.Code)
	}
}

func TestRateLimiterUsesResolvedClientIP(t *testing.T) {
	handler := NewRateLimitMiddleware(1, 1, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(client string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "127.0.0.1:8080"
		req.Header.Set("X-Forwarded-For", client)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	if got := request("203.0.113.1").Code; got != http.StatusNoContent {
		t.Fatalf("first client request got %d", got)
	}
	if got := request("203.0.113.2").Code; got != http.StatusNoContent {
		t.Fatalf("second client should have an independent bucket, got %d", got)
	}
	if got := request("203.0.113.1").Code; got != http.StatusTooManyRequests {
		t.Fatalf("repeated first client should be rate limited, got %d", got)
	}
}
