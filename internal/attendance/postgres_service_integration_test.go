//go:build integration

package attendance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Ridzz05/NexusAPI/internal/platform/events"
	"github.com/Ridzz05/NexusAPI/internal/platform/migrations"
	"github.com/Ridzz05/NexusAPI/internal/platform/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAttendanceStateAndOutbox(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := openIntegrationPool(ctx, databaseURL)
	if pool == nil {
		t.Fatal("could not connect to PostgreSQL")
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	memberID := "member-" + suffix
	deviceID := "device-" + suffix
	var eventIDs []string
	defer func() {
		for _, id := range eventIDs {
			_, _ = pool.Exec(context.Background(), `DELETE FROM nexus_event_outbox WHERE id = $1`, id)
			_, _ = pool.Exec(context.Background(), `DELETE FROM attendance_events WHERE id = $1`, id)
		}
		_, _ = pool.Exec(context.Background(), `DELETE FROM attendance_member_state WHERE member_id = $1`, memberID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM attendance_device_heartbeats WHERE device_id = $1`, deviceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM attendance_identifiers WHERE member_id = $1`, memberID)
	}()

	now := time.Now().UTC().Truncate(time.Microsecond)
	registry := NewPostgresIdentifierRegistry(pool)
	if _, err := registry.RegisterQR(ctx, memberID, "qr-in", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterQR(ctx, memberID, "qr-out", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterQR(ctx, memberID, "qr-in", nil); !errors.Is(err, ErrIdentifierAlreadyExists) {
		t.Fatalf("expected duplicate QR registration to fail, got %v", err)
	}
	var storedRawToken int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM attendance_identifiers WHERE member_id = $1 AND token_hash = $2`, memberID, "qr-in").Scan(&storedRawToken); err != nil {
		t.Fatal(err)
	}
	if storedRawToken != 0 {
		t.Fatal("raw QR token was persisted")
	}
	if _, err := registry.ResolveQR(ctx, "unknown-qr"); !errors.Is(err, ErrIdentifierNotFound) {
		t.Fatalf("expected unknown QR to be rejected, got %v", err)
	}
	if err := testIdentifierLifecycle(ctx, registry, memberID, now); err != nil {
		t.Fatal(err)
	}
	service := NewPostgresService(pool, registry)
	service.clock = func() time.Time { return now }
	memberActor := Actor{Subject: memberID, Roles: []string{"member"}}
	if _, err := service.CheckIn(ctx, memberActor, CheckCommand{MemberID: "other-member", QRToken: "qr-in", OccurredAt: now}); !errors.Is(err, ErrIdentifierMismatch) {
		t.Fatalf("expected member assertion mismatch, got %v", err)
	}
	checkIn, err := service.CheckIn(ctx, memberActor, CheckCommand{QRToken: "qr-in", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	eventIDs = append(eventIDs, checkIn.ID)
	if checkIn.Kind != "check_in" || checkIn.MemberID != memberID {
		t.Fatalf("unexpected check-in: %#v", checkIn)
	}
	var qrHash string
	if err := pool.QueryRow(ctx, `SELECT qr_token_hash FROM attendance_events WHERE id = $1`, checkIn.ID).Scan(&qrHash); err != nil {
		t.Fatal(err)
	}
	if len(qrHash) != 64 || qrHash == "qr-in" {
		t.Fatalf("QR token was not stored as a SHA-256 hash: %q", qrHash)
	}

	if _, err := service.CheckIn(ctx, memberActor, CheckCommand{QRToken: "qr-in", OccurredAt: now}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate check-in conflict, got %v", err)
	}
	checkOut, err := service.CheckOut(ctx, memberActor, CheckCommand{QRToken: "qr-out", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	eventIDs = append(eventIDs, checkOut.ID)
	if _, err := service.CheckOut(ctx, memberActor, CheckCommand{QRToken: "qr-out", OccurredAt: now}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate check-out conflict, got %v", err)
	}

	deviceActor := Actor{Subject: deviceID, Roles: []string{"device"}}
	heartbeat, err := service.Heartbeat(ctx, deviceActor, HeartbeatCommand{FirmwareVersion: "1.0.0", ObservedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	eventIDs = append(eventIDs, heartbeat.ID)

	var pending int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM nexus_event_outbox WHERE id IN ($1, $2, $3) AND published_at IS NULL`, eventIDs[0], eventIDs[1], eventIDs[2]).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 3 {
		t.Fatalf("expected three pending outbox events, got %d", pending)
	}

	publisher := &integrationPublisher{}
	dispatcher := events.NewDispatcher(pool, publisher, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
	if err := dispatcher.DispatchOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(publisher.ids) < 3 {
		t.Fatalf("expected dispatcher to publish events, got %v", publisher.ids)
	}
	var published int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM nexus_event_outbox WHERE id IN ($1, $2, $3) AND published_at IS NOT NULL`, eventIDs[0], eventIDs[1], eventIDs[2]).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != 3 {
		t.Fatalf("expected three published outbox events, got %d", published)
	}
}

func testIdentifierLifecycle(ctx context.Context, registry *PostgresIdentifierRegistry, memberID string, now time.Time) error {
	expiredAt := now.Add(-time.Minute)
	if _, err := registry.RegisterQR(ctx, memberID, "expired-qr", &expiredAt); err != nil {
		return err
	}
	if _, err := registry.ResolveQR(ctx, "expired-qr"); !errors.Is(err, ErrIdentifierExpired) {
		return fmt.Errorf("expected expired QR error, got %w", err)
	}
	revoked, err := registry.RegisterQR(ctx, memberID, "revoked-qr", nil)
	if err != nil {
		return err
	}
	if err := registry.Revoke(ctx, revoked.ID); err != nil {
		return err
	}
	if _, err := registry.ResolveQR(ctx, "revoked-qr"); !errors.Is(err, ErrIdentifierRevoked) {
		return fmt.Errorf("expected revoked QR error, got %w", err)
	}
	return nil
}

func TestConcurrentCheckInsResolveThroughSerializableRetry(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := openIntegrationPool(ctx, databaseURL)
	if pool == nil {
		t.Fatal("could not connect to PostgreSQL")
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}

	memberID := fmt.Sprintf("concurrent-member-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Microsecond)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM nexus_event_outbox WHERE payload->>'member_id' = $1`, memberID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM attendance_events WHERE member_id = $1`, memberID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM attendance_member_state WHERE member_id = $1`, memberID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM attendance_identifiers WHERE member_id = $1`, memberID)
	}()

	registry := NewPostgresIdentifierRegistry(pool)
	if _, err := registry.RegisterQR(ctx, memberID, "concurrent-qr", nil); err != nil {
		t.Fatal(err)
	}
	service := NewPostgresService(pool, registry)
	service.clock = func() time.Time { return now }
	actor := Actor{Subject: memberID, Roles: []string{"member"}}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer group.Done()
			<-start
			_, err := service.CheckIn(ctx, actor, CheckCommand{QRToken: "concurrent-qr", OccurredAt: now})
			results <- err
		}()
	}
	close(start)
	group.Wait()
	close(results)

	var success, conflict int
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrConflict):
			conflict++
		default:
			t.Fatalf("unexpected concurrent check-in error: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("expected one success and one conflict, got success=%d conflict=%d", success, conflict)
	}
}

func openIntegrationPool(ctx context.Context, databaseURL string) *pgxpool.Pool {
	deadline, cancel := context.WithCancel(ctx)
	defer cancel()
	for {
		pool, err := postgres.Open(deadline, databaseURL, 1, 4)
		if err == nil {
			return pool
		}
		select {
		case <-deadline.Done():
			return nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}

type integrationPublisher struct {
	ids []string
}

func (p *integrationPublisher) Publish(_ context.Context, _ string, payload any) error {
	raw, ok := payload.(json.RawMessage)
	if !ok {
		return fmt.Errorf("unexpected payload type %T", payload)
	}
	var event struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return err
	}
	p.ids = append(p.ids, event.ID)
	return nil
}
