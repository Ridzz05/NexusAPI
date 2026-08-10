package cache

import (
	"context"
	"testing"
	"time"
)

func TestJSONHelpersRoundTrip(t *testing.T) {
	store := &memoryStore{values: make(map[string][]byte)}
	want := struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}{Name: "member", Size: 2}
	if err := SetJSON(context.Background(), store, "member:1", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}
	found, err := GetJSON(context.Background(), store, "member:1", &got)
	if err != nil || !found || got != want {
		t.Fatalf("unexpected cache result: found=%v value=%#v err=%v", found, got, err)
	}
	found, err = GetJSON(context.Background(), store, "missing", &got)
	if err != nil || found {
		t.Fatalf("expected cache miss, found=%v err=%v", found, err)
	}
}

type memoryStore struct {
	values map[string][]byte
}

func (s *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, ErrMiss
	}
	return value, nil
}

func (s *memoryStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.values[key] = value
	return nil
}
