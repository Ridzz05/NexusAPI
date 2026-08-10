package attendance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ridzz05/NexusAPI/internal/platform/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxEventClockSkew = 24 * time.Hour

const maxSerializableAttempts = 3

type PostgresService struct {
	pool  *pgxpool.Pool
	clock func() time.Time
}

func NewPostgresService(pool *pgxpool.Pool) *PostgresService {
	return &PostgresService{pool: pool, clock: time.Now}
}

func (s *PostgresService) CheckIn(ctx context.Context, actor Actor, command CheckCommand) (Event, error) {
	return s.changeState(ctx, actor, command, "check_in", "checked_in")
}

func (s *PostgresService) CheckOut(ctx context.Context, actor Actor, command CheckCommand) (Event, error) {
	return s.changeState(ctx, actor, command, "check_out", "checked_out")
}

func (s *PostgresService) Heartbeat(ctx context.Context, actor Actor, command HeartbeatCommand) (Event, error) {
	if err := ValidateHeartbeatCommand(command); err != nil || strings.TrimSpace(actor.Subject) == "" {
		return Event{}, ErrInvalidCommand
	}
	if !actor.CanSendHeartbeat() {
		return Event{}, ErrNotAuthorized
	}
	now := s.clock()
	if command.ObservedAt.Before(now.Add(-maxEventClockSkew)) || command.ObservedAt.After(now.Add(maxEventClockSkew)) {
		return Event{}, ErrInvalidCommand
	}
	if s.pool == nil {
		return Event{}, errors.New("attendance database is not configured")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Event{}, fmt.Errorf("begin heartbeat transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	deviceID := actor.Subject
	if _, err := tx.Exec(ctx, `
		INSERT INTO attendance_device_heartbeats (device_id, actor_subject, firmware_version, observed_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (device_id) DO UPDATE SET
			actor_subject = EXCLUDED.actor_subject,
			firmware_version = EXCLUDED.firmware_version,
			observed_at = EXCLUDED.observed_at,
			created_at = EXCLUDED.created_at`,
		deviceID, actor.Subject, command.FirmwareVersion, command.ObservedAt, now); err != nil {
		return Event{}, fmt.Errorf("store heartbeat: %w", err)
	}
	event := Event{ID: newID(), Kind: "heartbeat", DeviceID: deviceID, OccurredAt: command.ObservedAt}
	if _, err := tx.Exec(ctx, `
		INSERT INTO attendance_events (id, kind, device_id, actor_subject, occurred_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ID, event.Kind, event.DeviceID, actor.Subject, event.OccurredAt, now); err != nil {
		return Event{}, fmt.Errorf("store heartbeat event: %w", err)
	}
	if err := events.Enqueue(ctx, tx, event.ID, events.AttendanceTopic, event, now); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Event{}, fmt.Errorf("commit heartbeat: %w", err)
	}
	return event, nil
}

func (s *PostgresService) changeState(ctx context.Context, actor Actor, command CheckCommand, kind, nextState string) (Event, error) {
	if err := ValidateCheckCommand(command); err != nil || strings.TrimSpace(actor.Subject) == "" {
		return Event{}, ErrInvalidCommand
	}
	now := s.clock()
	if command.OccurredAt.Before(now.Add(-maxEventClockSkew)) || command.OccurredAt.After(now.Add(maxEventClockSkew)) {
		return Event{}, ErrInvalidCommand
	}
	memberID := strings.TrimSpace(command.MemberID)
	if memberID == "" {
		memberID = actor.Subject
	}
	if memberID != actor.Subject && !actor.CanManageOthers() {
		return Event{}, ErrNotAuthorized
	}
	if s.pool == nil {
		return Event{}, errors.New("attendance database is not configured")
	}
	for attempt := 0; attempt < maxSerializableAttempts; attempt++ {
		event, err := s.changeStateOnce(ctx, actor, command, memberID, kind, nextState)
		if err == nil || !isRetryableTransactionError(err) || attempt == maxSerializableAttempts-1 {
			return event, err
		}
		if err := waitForTransactionRetry(ctx, attempt); err != nil {
			return Event{}, err
		}
	}
	return Event{}, errors.New("attendance transaction retry limit exceeded")
}

func (s *PostgresService) changeStateOnce(ctx context.Context, actor Actor, command CheckCommand, memberID, kind, nextState string) (Event, error) {
	now := s.clock()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Event{}, fmt.Errorf("begin attendance transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentState string
	err = tx.QueryRow(ctx, `SELECT state FROM attendance_member_state WHERE member_id = $1 FOR UPDATE`, memberID).Scan(&currentState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Event{}, fmt.Errorf("read attendance state: %w", err)
	}
	if kind == "check_in" && currentState == "checked_in" {
		return Event{}, ErrConflict
	}
	if kind == "check_out" && currentState != "checked_in" {
		return Event{}, ErrConflict
	}
	if err := upsertState(ctx, tx, memberID, nextState, now); err != nil {
		return Event{}, err
	}
	event := Event{ID: newID(), Kind: kind, MemberID: memberID, DeviceID: actor.Subject, OccurredAt: command.OccurredAt}
	qrHash := sha256.Sum256([]byte(command.QRToken))
	if _, err := tx.Exec(ctx, `
		INSERT INTO attendance_events (id, kind, member_id, device_id, actor_subject, qr_token_hash, occurred_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		event.ID, event.Kind, event.MemberID, event.DeviceID, actor.Subject, hex.EncodeToString(qrHash[:]), event.OccurredAt, now); err != nil {
		return Event{}, fmt.Errorf("store attendance event: %w", err)
	}
	if err := events.Enqueue(ctx, tx, event.ID, events.AttendanceTopic, event, now); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Event{}, fmt.Errorf("commit attendance transaction: %w", err)
	}
	return event, nil
}

func isRetryableTransactionError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

func waitForTransactionRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func upsertState(ctx context.Context, tx pgx.Tx, memberID, state string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO attendance_member_state (member_id, state, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (member_id) DO UPDATE SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at`, memberID, state, now); err != nil {
		return fmt.Errorf("update attendance state: %w", err)
	}
	return nil
}

func newID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(bytes)
}
