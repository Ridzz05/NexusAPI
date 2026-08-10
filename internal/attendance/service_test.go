package attendance

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestCommandsValidateRequiredFields(t *testing.T) {
	if err := ValidateCheckCommand(CheckCommand{OccurredAt: time.Now()}); err == nil {
		t.Fatal("expected QR token to be required")
	}
	if err := ValidateHeartbeatCommand(HeartbeatCommand{ObservedAt: time.Now(), FirmwareVersion: string(make([]byte, 129))}); err == nil {
		t.Fatal("expected firmware version length to be bounded")
	}
	if err := ValidateCheckCommand(CheckCommand{QRToken: "qr", OccurredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestActorCannotManageAnotherMemberByDefault(t *testing.T) {
	actor := Actor{Subject: "member-1", Roles: []string{"member"}}
	if actor.CanManageOthers() {
		t.Fatal("member must not manage other members")
	}
	staff := Actor{Subject: "staff-1", Roles: []string{"staff"}}
	if !staff.CanManageOthers() {
		t.Fatal("staff should manage attendance for other members")
	}
	if staff.CanSendHeartbeat() {
		t.Fatal("staff alone should not impersonate a device heartbeat")
	}
	device := Actor{Subject: "device-1", Roles: []string{"device"}}
	if !device.CanSendHeartbeat() {
		t.Fatal("device should send heartbeat")
	}
}

func TestRetryableTransactionErrorsAreRecognized(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		err := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code})
		if !isRetryableTransactionError(err) {
			t.Fatalf("expected PostgreSQL code %s to be retryable", code)
		}
	}
	if isRetryableTransactionError(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique violations must not be retried")
	}
}
