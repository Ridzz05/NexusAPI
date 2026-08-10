package loyalfitness

import (
	"context"
	"testing"
	"time"

	"github.com/Ridzz05/NexusAPI/internal/platform/cache"
	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
)

func TestCachedReaderAvoidsRepeatedUpstreamReads(t *testing.T) {
	next := &countingReader{}
	store := &testStore{values: make(map[string][]byte)}
	reader := NewCachedReader(next, store, time.Minute)
	actor := Actor{Subject: "user-1", Roles: []string{"member"}}
	page := httpx.PageRequest{Limit: 20}
	filter := MemberFilter{Query: "alice", Status: "active"}
	first, err := reader.FindMembers(context.Background(), actor, filter, page)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.FindMembers(context.Background(), actor, filter, page)
	if err != nil {
		t.Fatal(err)
	}
	if next.memberCalls != 1 || first.Items[0].ID != second.Items[0].ID {
		t.Fatalf("expected one upstream read, calls=%d first=%#v second=%#v", next.memberCalls, first, second)
	}
}

type countingReader struct {
	memberCalls int
}

func (r *countingReader) FindMembers(context.Context, Actor, MemberFilter, httpx.PageRequest) (MembersPage, error) {
	r.memberCalls++
	return MembersPage{Items: []Member{{ID: "m-1"}}}, nil
}

func (r *countingReader) FindPTSessions(context.Context, Actor, PTSessionFilter, httpx.PageRequest) (PTSessionsPage, error) {
	return PTSessionsPage{}, nil
}

func (r *countingReader) FinanceSummary(context.Context, Actor) (FinanceSummary, error) {
	return FinanceSummary{}, nil
}

func (r *countingReader) MobileDashboard(context.Context, Actor) (MobileDashboard, error) {
	return MobileDashboard{}, nil
}

type testStore struct {
	values map[string][]byte
}

func (s *testStore) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, cache.ErrMiss
	}
	return value, nil
}

func (s *testStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.values[key] = value
	return nil
}
