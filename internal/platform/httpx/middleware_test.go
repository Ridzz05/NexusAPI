package httpx

import (
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
