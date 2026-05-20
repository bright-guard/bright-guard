package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

type DeviceAuth struct {
	Pool   *pgxpool.Pool
	Secret []byte
}

var ErrAlreadyResolved = errors.New("device authorization already resolved")

func (d *DeviceAuth) hash(secret string) []byte {
	mac := hmac.New(sha256.New, d.Secret)
	mac.Write([]byte(secret))
	return mac.Sum(nil)
}

// Create inserts a new pending device authorization.
func (d *DeviceAuth) Create(ctx context.Context, deviceCode, userCode, clientLabel string, ttl time.Duration) (*models.DeviceAuthorization, error) {
	deviceHash := d.hash(deviceCode)
	expires := time.Now().Add(ttl)
	const q = `
		insert into device_authorizations (user_code, device_hash, client_label, expires_at)
		values ($1, $2, $3, $4)
		returning id, user_code, client_label, status, approved_at, expires_at, created_at`
	out := &models.DeviceAuthorization{}
	err := d.Pool.QueryRow(ctx, q, userCode, deviceHash, clientLabel, expires).Scan(
		&out.ID, &out.UserCode, &out.ClientLabel, &out.Status, &out.ApprovedAt, &out.ExpiresAt, &out.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetByUserCode loads by the human-typed user code.
func (d *DeviceAuth) GetByUserCode(ctx context.Context, userCode string) (*models.DeviceAuthorization, error) {
	const q = `
		select id, user_code, client_label, status, user_id, session_id, approved_at, expires_at, created_at
		from device_authorizations
		where user_code = $1`
	out := &models.DeviceAuthorization{}
	err := d.Pool.QueryRow(ctx, q, userCode).Scan(
		&out.ID, &out.UserCode, &out.ClientLabel, &out.Status, &out.UserID, &out.SessionID, &out.ApprovedAt, &out.ExpiresAt, &out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetByDeviceCode looks up by the CLI-held device_code (matched via HMAC).
func (d *DeviceAuth) GetByDeviceCode(ctx context.Context, deviceCode string) (*models.DeviceAuthorization, error) {
	deviceHash := d.hash(deviceCode)
	const q = `
		select id, user_code, client_label, status, user_id, session_id, approved_at, expires_at, created_at
		from device_authorizations
		where device_hash = $1`
	out := &models.DeviceAuthorization{}
	err := d.Pool.QueryRow(ctx, q, deviceHash).Scan(
		&out.ID, &out.UserCode, &out.ClientLabel, &out.Status, &out.UserID, &out.SessionID, &out.ApprovedAt, &out.ExpiresAt, &out.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ApproveWithBearer atomically transitions pending -> approved, attaching the
// user, the CLI session id, and the plaintext bearer token (held until the
// CLI polls successfully).
func (d *DeviceAuth) ApproveWithBearer(ctx context.Context, id, userID, sessionID uuid.UUID, bearer string) error {
	const q = `
		update device_authorizations
		set status = 'approved', user_id = $2, session_id = $3, bearer_secret = $4, approved_at = now()
		where id = $1 and status = 'pending' and expires_at > now()`
	tag, err := d.Pool.Exec(ctx, q, id, userID, sessionID, bearer)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyResolved
	}
	return nil
}

// ConsumeApproved reads the bearer for an approved device authorization and
// deletes the row in the same transaction so the token can only be claimed once.
// Returns (bearer, sessionID, true, nil) on first call, (_, _, false, nil) if
// the row was already consumed or no longer approved.
func (d *DeviceAuth) ConsumeApproved(ctx context.Context, id uuid.UUID) (string, uuid.UUID, bool, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return "", uuid.Nil, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var bearer *string
	var sessionID *uuid.UUID
	var status string
	const sel = `
		select status, session_id, bearer_secret
		from device_authorizations
		where id = $1
		for update`
	err = tx.QueryRow(ctx, sel, id).Scan(&status, &sessionID, &bearer)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", uuid.Nil, false, nil
	}
	if err != nil {
		return "", uuid.Nil, false, err
	}
	if status != "approved" || bearer == nil || sessionID == nil {
		return "", uuid.Nil, false, nil
	}
	const del = `delete from device_authorizations where id = $1`
	if _, err := tx.Exec(ctx, del, id); err != nil {
		return "", uuid.Nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", uuid.Nil, false, err
	}
	return *bearer, *sessionID, true, nil
}

// Deny transitions pending -> denied.
func (d *DeviceAuth) Deny(ctx context.Context, id, userID uuid.UUID) error {
	const q = `
		update device_authorizations
		set status = 'denied', user_id = $2
		where id = $1 and status = 'pending'`
	tag, err := d.Pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyResolved
	}
	return nil
}

