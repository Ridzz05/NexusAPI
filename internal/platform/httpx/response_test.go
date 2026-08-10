package httpx

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParsePageRequestBoundsCollections(t *testing.T) {
	req := httptest.NewRequest("GET", "/members?limit=101", nil)
	if _, err := ParsePageRequest(req); err == nil {
		t.Fatal("expected limit above maximum to fail")
	}

	req = httptest.NewRequest("GET", "/members?limit=2&cursor="+EncodeCursor("member-10"), nil)
	page, err := ParsePageRequest(req)
	if err != nil || page.Limit != 2 || page.Cursor == "" {
		t.Fatalf("unexpected page request: %#v, %v", page, err)
	}

	tooLongCursor := strings.Repeat("a", MaxCursorLength+1)
	if _, err := ParsePageRequest(httptest.NewRequest("GET", "/members?cursor="+tooLongCursor, nil)); err == nil {
		t.Fatal("expected an oversized cursor to fail")
	}

	page, err = NormalizePageRequest(PageRequest{})
	if err != nil || page.Limit != DefaultPageSize {
		t.Fatalf("expected zero-value page to receive the default limit: %#v, %v", page, err)
	}
}

func TestBoundedQueryParamRejectsOversizedFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/members?q="+strings.Repeat("x", 101), nil)
	if _, err := BoundedQueryParam(req, "q", 100); err == nil {
		t.Fatal("expected oversized filter to fail")
	}
}

func TestSecurityHeadersAreSetByMiddleware(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	for key, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := recorder.Header().Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestRequestTimeoutContainsHandlerPanic(t *testing.T) {
	handler := RequestTimeoutMiddleware(time.Second, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("test panic")
	}))
	handler = RecoveryMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)), handler)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected panic to become 500, got %d", recorder.Code)
	}
}
