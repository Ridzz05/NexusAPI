package loyalfitness

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
)

func TestHTTPReaderMapsBoundedMemberResponseAndActorScope(t *testing.T) {
	var gotAuthorization string
	var gotQuery string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/members" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"m-1","display_name":"A","status":"active"}],"meta":{"next_cursor":"next","has_more":true}}`)),
			Header:     make(http.Header),
		}, nil
	})}

	reader, err := NewHTTPReader("https://source.example", "service-token", client)
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.FindMembers(context.Background(), Actor{Subject: "user-1", Roles: []string{"member"}}, MemberFilter{Query: "alice", Status: "active"}, httpx.PageRequest{Limit: 10, Cursor: httpx.EncodeCursor("m-0")})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "m-1" || page.NextCursor != "next" || !page.HasMore {
		t.Fatalf("unexpected page: %#v", page)
	}
	if gotAuthorization != "Bearer service-token" {
		t.Fatalf("unexpected authorization header: %q", gotAuthorization)
	}
	for _, expected := range []string{"actor_subject=user-1", "actor_roles=member", "actor_scope=self", "q=alice", "status=active", "limit=10"} {
		if !strings.Contains(gotQuery, expected) {
			t.Fatalf("query %q did not include %q", gotQuery, expected)
		}
	}
}

func TestHTTPReaderDoesNotExposeUpstreamBody(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("secret upstream details")),
			Header:     make(http.Header),
		}, nil
	})}
	reader, err := NewHTTPReader("https://source.example", "", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.FinanceSummary(context.Background(), Actor{Subject: "user-1"})
	if err == nil || !strings.Contains(err.Error(), "status 500") || strings.Contains(err.Error(), "secret upstream") {
		t.Fatalf("unexpected upstream error: %v", err)
	}
}

func TestHTTPReaderForwardsPTSessionFilters(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/pt-sessions" || r.URL.Query().Get("status") != "scheduled" || r.URL.Query().Get("from") != "2026-01-01" || r.URL.Query().Get("to") != "2026-01-31" {
			t.Fatalf("unexpected PT filter request: %s", r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[],"meta":{"has_more":false}}`)),
			Header:     make(http.Header),
		}, nil
	})}
	reader, err := NewHTTPReader("https://source.example", "", client)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reader.FindPTSessions(context.Background(), Actor{Subject: "user-1"}, PTSessionFilter{Status: "scheduled", From: "2026-01-01", To: "2026-01-31"}, httpx.PageRequest{Limit: 20})
	if err != nil || len(result.Items) != 0 || result.HasMore {
		t.Fatalf("unexpected PT result: %#v err=%v", result, err)
	}
}

func TestHTTPReaderRejectsPageFromUpstreamThatExceedsLimit(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"m-1"},{"id":"m-2"}],"meta":{"has_more":true}}`)),
			Header:     make(http.Header),
		}, nil
	})}
	reader, err := NewHTTPReader("https://source.example", "", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.FindMembers(context.Background(), Actor{Subject: "user-1"}, MemberFilter{}, httpx.PageRequest{Limit: 1})
	if err == nil || !strings.Contains(err.Error(), "exceeds requested limit") {
		t.Fatalf("expected oversized upstream page error, got %v", err)
	}
}

func TestHTTPReaderRejectsTrailingUpstreamJSON(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":{"period":"2026","total":1,"currency":"IDR"}} {"unexpected":true}`)),
			Header:     make(http.Header),
		}, nil
	})}
	reader, err := NewHTTPReader("https://source.example", "", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.FinanceSummary(context.Background(), Actor{Subject: "user-1"})
	if err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}
}

func TestHTTPReaderRejectsUnboundedDirectReadRequest(t *testing.T) {
	reader, err := NewHTTPReader("https://source.example", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.FindMembers(context.Background(), Actor{Subject: "user-1"}, MemberFilter{}, httpx.PageRequest{Limit: httpx.MaxPageSize + 1})
	if err == nil || !strings.Contains(err.Error(), "invalid Loyal Fitness read request") {
		t.Fatalf("expected bounded page validation error, got %v", err)
	}
	if _, err := reader.FinanceSummary(context.Background(), Actor{}); err == nil || !strings.Contains(err.Error(), "invalid Loyal Fitness read request") {
		t.Fatalf("expected empty actor validation error, got %v", err)
	}
}

func TestHTTPReaderRejectsUnsupportedURL(t *testing.T) {
	if _, err := NewHTTPReader("ftp://source.example", "", nil); err == nil {
		t.Fatal("expected unsupported URL scheme to be rejected")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
