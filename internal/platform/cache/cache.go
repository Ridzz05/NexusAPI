package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrMiss = errors.New("cache miss")

// Store is deliberately small so domain modules can be tested without Redis
// while production uses the shared Redis client.
type Store interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
}

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrMiss
	}
	if err != nil {
		return nil, fmt.Errorf("get cache key: %w", err)
	}
	return value, nil
}

func (s *RedisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := s.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("set cache key: %w", err)
	}
	return nil
}

func GetJSON[T any](ctx context.Context, store Store, key string, target *T) (bool, error) {
	value, err := store.Get(ctx, key)
	if errors.Is(err, ErrMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(value, target); err != nil {
		return false, fmt.Errorf("decode cache key: %w", err)
	}
	return true, nil
}

func SetJSON[T any](ctx context.Context, store Store, key string, value T, ttl time.Duration) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode cache value: %w", err)
	}
	return store.Set(ctx, key, encoded, ttl)
}
