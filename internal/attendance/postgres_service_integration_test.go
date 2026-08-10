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
	}()

	now := time.Now().UTC().Truncate(time.Microsecond)
	service := NewPostgresService(pool)
	service.clock = func() time.Time { return now }
	memberActor := Actor{Subject: memberID, Roles: []string{"member"}}
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

	if _, err := service.CheckIn(ctx, memberActor, CheckCommand{QRToken: "qr-in-duplicate", OccurredAt: now}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate check-in conflict, got %v", err)
	}
	checkOut, err := service.CheckOut(ctx, memberActor, CheckCommand{QRToken: "qr-out", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	eventIDs = append(eventIDs, checkOut.ID)
	if _, err := service.CheckOut(ctx, memberActor, CheckCommand{QRToken: "qr-out-duplicate", OccurredAt: now}); !errors.Is(err, ErrConflict) {
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
