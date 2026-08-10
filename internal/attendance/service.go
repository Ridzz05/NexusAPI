package attendance

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidCommand = errors.New("invalid attendance command")
	ErrNotAuthorized  = errors.New("attendance command is not authorized")
	ErrConflict       = errors.New("attendance command conflicts with current state")
)

type Actor struct {
	Subject string
	Roles   []string
}

func (a Actor) CanManageOthers() bool {
	for _, role := range a.Roles {
		switch role {
		case "admin", "owner", "manager", "staff", "kiosk", "device":
			return true
		}
	}
	return false
}

func (a Actor) CanSendHeartbeat() bool {
	for _, role := range a.Roles {
		switch role {
		case "admin", "owner", "kiosk", "device":
			return true
		}
	}
	return false
}

type CheckCommand struct {
	MemberID   string    `json:"member_id,omitempty"`
	QRToken    string    `json:"qr_token"`
	OccurredAt time.Time `json:"occurred_at"`
}

type HeartbeatCommand struct {
	FirmwareVersion string    `json:"firmware_version,omitempty"`
	ObservedAt      time.Time `json:"observed_at"`
}

type Event struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	MemberID   string    `json:"member_id,omitempty"`
	DeviceID   string    `json:"device_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Service interface {
	CheckIn(context.Context, Actor, CheckCommand) (Event, error)
	CheckOut(context.Context, Actor, CheckCommand) (Event, error)
	Heartbeat(context.Context, Actor, HeartbeatCommand) (Event, error)
}

func ValidateCheckCommand(command CheckCommand) error {
	if strings.TrimSpace(command.QRToken) == "" || len(command.QRToken) > 4096 || len(command.MemberID) > 128 || command.OccurredAt.IsZero() {
		return ErrInvalidCommand
	}
	return nil
}

func ValidateHeartbeatCommand(command HeartbeatCommand) error {
	if len(command.FirmwareVersion) > 128 || command.ObservedAt.IsZero() {
		return ErrInvalidCommand
	}
	return nil
}
