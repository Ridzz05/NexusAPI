package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const AttendanceTopic = "nexus.attendance.events"

type Publisher interface {
	Publish(context.Context, string, any) error
}

type RedisPublisher struct {
	client *redis.Client
}

func Enqueue(ctx context.Context, tx pgx.Tx, id, topic string, payload any, createdAt time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode outbox payload: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO nexus_event_outbox (id, topic, payload, created_at)
		VALUES ($1, $2, $3::jsonb, $4)`, id, topic, string(encoded), createdAt); err != nil {
		return fmt.Errorf("enqueue event: %w", err)
	}
	return nil
}

type Dispatcher struct {
	pool      *pgxpool.Pool
	publisher Publisher
	logger    *slog.Logger
	interval  time.Duration
	batchSize int
}

func NewDispatcher(pool *pgxpool.Pool, publisher Publisher, logger *slog.Logger, interval time.Duration) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = time.Second
	}
	return &Dispatcher{pool: pool, publisher: publisher, logger: logger, interval: interval, batchSize: 50}
}

func (d *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		if err := d.DispatchOnce(ctx); err != nil && !isContextError(err) {
			d.logger.Error("event outbox dispatch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) DispatchOnce(ctx context.Context) error {
	if d.pool == nil || d.publisher == nil {
		return nil
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin outbox dispatch: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT id, topic, payload::text
		FROM nexus_event_outbox
		WHERE published_at IS NULL
		ORDER BY created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1`, d.batchSize)
	if err != nil {
		return fmt.Errorf("read event outbox: %w", err)
	}
	items := make([]outboxItem, 0, d.batchSize)
	for rows.Next() {
		var item outboxItem
		if err := rows.Scan(&item.ID, &item.Topic, &item.Payload); err != nil {
			rows.Close()
			return fmt.Errorf("scan event outbox: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read event outbox rows: %w", err)
	}
	rows.Close()
	for _, item := range items {
		if err := d.publisher.Publish(ctx, item.Topic, json.RawMessage(item.Payload)); err != nil {
			if _, updateErr := tx.Exec(ctx, `UPDATE nexus_event_outbox SET attempts = attempts + 1, last_error = LEFT($2, 500) WHERE id = $1`, item.ID, err.Error()); updateErr != nil {
				return fmt.Errorf("record outbox failure: %w", updateErr)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE nexus_event_outbox SET published_at = NOW(), attempts = attempts + 1, last_error = NULL WHERE id = $1`, item.ID); err != nil {
			return fmt.Errorf("mark event published: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit outbox dispatch: %w", err)
	}
	return nil
}

type outboxItem struct {
	ID      string
	Topic   string
	Payload string
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func NewRedisPublisher(client *redis.Client) *RedisPublisher {
	return &RedisPublisher{client: client}
}

func (p *RedisPublisher) Publish(ctx context.Context, topic string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode event payload: %w", err)
	}
	if err := p.client.Publish(ctx, topic, encoded).Err(); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}
	return nil
}
