package attendance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	dbgen "github.com/Ridzz05/NexusAPI/internal/platform/database/generated"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresIdentifierRegistry stores only SHA-256 QR digests. The caller is
// responsible for using high-entropy QR tokens; a digest is not a password
// KDF and must not be used for low-entropy secrets.
type PostgresIdentifierRegistry struct {
	pool  *pgxpool.Pool
	clock func() time.Time
}

func NewPostgresIdentifierRegistry(pool *pgxpool.Pool) *PostgresIdentifierRegistry {
	return &PostgresIdentifierRegistry{pool: pool, clock: time.Now}
}

func (r *PostgresIdentifierRegistry) RegisterQR(ctx context.Context, memberID, token string, expiresAt *time.Time) (MemberIdentifier, error) {
	memberID = strings.TrimSpace(memberID)
	token = strings.TrimSpace(token)
	if memberID == "" || len(memberID) > 128 || token == "" || len(token) > 4096 {
		return MemberIdentifier{}, ErrInvalidCommand
	}
	if r.pool == nil {
		return MemberIdentifier{}, errors.New("attendance database is not configured")
	}
	id, err := newID()
	if err != nil {
		return MemberIdentifier{}, err
	}
	now := r.clock()
	identifier := MemberIdentifier{
		ID:             id,
		MemberID:       memberID,
		IdentifierType: "qr",
		TokenHash:      hashQRToken(token),
		Status:         "active",
		ExpiresAt:      expiresAt,
	}
	queries := dbgen.New(r.pool)
	if err := queries.InsertAttendanceIdentifier(ctx, dbgen.InsertAttendanceIdentifierParams{
		ID:        identifier.ID,
		MemberID:  identifier.MemberID,
		TokenHash: identifier.TokenHash,
		ExpiresAt: timestamp(expiresAt),
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return MemberIdentifier{}, ErrIdentifierAlreadyExists
		}
		return MemberIdentifier{}, fmt.Errorf("register attendance identifier: %w", err)
	}
	return identifier, nil
}

func (r *PostgresIdentifierRegistry) Revoke(ctx context.Context, id string) error {
	if r.pool == nil {
		return errors.New("attendance database is not configured")
	}
	rows, err := dbgen.New(r.pool).RevokeAttendanceIdentifier(ctx, dbgen.RevokeAttendanceIdentifierParams{
		ID:        strings.TrimSpace(id),
		UpdatedAt: pgtype.Timestamptz{Time: r.clock(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("revoke attendance identifier: %w", err)
	}
	if rows == 0 {
		return ErrIdentifierNotFound
	}
	return nil
}

func (r *PostgresIdentifierRegistry) ResolveQR(ctx context.Context, token string) (MemberIdentifier, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 4096 {
		return MemberIdentifier{}, ErrIdentifierNotFound
	}
	if r.pool == nil {
		return MemberIdentifier{}, errors.New("attendance database is not configured")
	}
	row, err := dbgen.New(r.pool).GetAttendanceIdentifierByHash(ctx, hashQRToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MemberIdentifier{}, ErrIdentifierNotFound
		}
		return MemberIdentifier{}, fmt.Errorf("resolve attendance identifier: %w", err)
	}
	identifier := MemberIdentifier{
		ID:             row.ID,
		MemberID:       row.MemberID,
		IdentifierType: row.IdentifierType,
		TokenHash:      row.TokenHash,
		Status:         row.Status,
		ExpiresAt:      optionalTimestamp(row.ExpiresAt),
	}
	if identifier.Status != "active" {
		return MemberIdentifier{}, ErrIdentifierRevoked
	}
	if identifier.ExpiresAt != nil && !r.clock().Before(*identifier.ExpiresAt) {
		return MemberIdentifier{}, ErrIdentifierExpired
	}
	return identifier, nil
}

func hashQRToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func timestamp(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func optionalTimestamp(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
