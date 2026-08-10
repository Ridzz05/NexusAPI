package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestErrorUsesTopLevelRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Error(w, r, http.StatusBadRequest, "invalid", "invalid request")
	})).ServeHTTP(recorder, req)
	var body struct {
		Error     APIError `json:"error"`
		RequestID string   `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "invalid" || body.RequestID == "" {
		t.Fatalf("unexpected error envelope: %#v", body)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	var nested map[string]string
	if err := json.Unmarshal(raw["error"], &nested); err != nil {
		t.Fatal(err)
	}
	if _, exists := nested["request_id"]; exists {
		t.Fatal("request_id must remain at the top level of an error envelope")
	}
}

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
	handler := RecoveryMiddleware(slog.New(slog.NewTextHandler(io.Discard, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("test panic")
	}))
	handler = RequestTimeoutMiddleware(time.Second, handler)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected panic to become 500, got %d", recorder.Code)
	}
}
