package attendance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	dbgen "github.com/Ridzz05/NexusAPI/internal/platform/database/generated"
	"github.com/Ridzz05/NexusAPI/internal/platform/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxEventClockSkew = 24 * time.Hour

const maxSerializableAttempts = 3

type PostgresService struct {
	pool        *pgxpool.Pool
	identifiers IdentifierResolver
	clock       func() time.Time
}

func NewPostgresService(pool *pgxpool.Pool, resolvers ...IdentifierResolver) *PostgresService {
	var resolver IdentifierResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	} else {
		resolver = NewPostgresIdentifierRegistry(pool)
	}
	return &PostgresService{pool: pool, identifiers: resolver, clock: time.Now}
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
	queries := dbgen.New(tx)
	if err := queries.UpsertAttendanceDeviceHeartbeat(ctx, dbgen.UpsertAttendanceDeviceHeartbeatParams{
		DeviceID:        deviceID,
		ActorSubject:    actor.Subject,
		FirmwareVersion: command.FirmwareVersion,
		ObservedAt:      pgtype.Timestamptz{Time: command.ObservedAt, Valid: true},
		CreatedAt:       pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		return Event{}, fmt.Errorf("store heartbeat: %w", err)
	}
	eventID, err := newID()
	if err != nil {
		return Event{}, err
	}
	event := Event{ID: eventID, Kind: "heartbeat", DeviceID: deviceID, OccurredAt: command.ObservedAt}
	if err := queries.InsertAttendanceEvent(ctx, dbgen.InsertAttendanceEventParams{
		ID:           event.ID,
		Kind:         event.Kind,
		DeviceID:     event.DeviceID,
		ActorSubject: actor.Subject,
		OccurredAt:   pgtype.Timestamptz{Time: event.OccurredAt, Valid: true},
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
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
	if s.identifiers == nil {
		return Event{}, errors.New("attendance identifier resolver is not configured")
	}
	identifier, err := s.identifiers.ResolveQR(ctx, command.QRToken)
	if err != nil {
		return Event{}, err
	}
	memberID := strings.TrimSpace(identifier.MemberID)
	if memberID == "" {
		return Event{}, ErrIdentifierNotFound
	}
	if assertion := strings.TrimSpace(command.MemberID); assertion != "" && assertion != memberID {
		return Event{}, ErrIdentifierMismatch
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
	queries := dbgen.New(tx)

	var currentState string
	currentState, err = queries.GetAttendanceMemberState(ctx, memberID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Event{}, fmt.Errorf("read attendance state: %w", err)
	}
	if kind == "check_in" && currentState == "checked_in" {
		return Event{}, ErrConflict
	}
	if kind == "check_out" && currentState != "checked_in" {
		return Event{}, ErrConflict
	}
	if err := queries.UpsertAttendanceMemberState(ctx, dbgen.UpsertAttendanceMemberStateParams{
		MemberID:  memberID,
		State:     nextState,
		UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		return Event{}, fmt.Errorf("update attendance state: %w", err)
	}
	eventID, err := newID()
	if err != nil {
		return Event{}, err
	}
	event := Event{ID: eventID, Kind: kind, MemberID: memberID, DeviceID: actor.Subject, OccurredAt: command.OccurredAt}
	if err := queries.InsertAttendanceEvent(ctx, dbgen.InsertAttendanceEventParams{
		ID:           event.ID,
		Kind:         event.Kind,
		MemberID:     pgtype.Text{String: event.MemberID, Valid: true},
		DeviceID:     event.DeviceID,
		ActorSubject: actor.Subject,
		QrTokenHash:  pgtype.Text{String: hashQRToken(command.QRToken), Valid: true},
		OccurredAt:   pgtype.Timestamptz{Time: event.OccurredAt, Valid: true},
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
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

func newID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
