package store

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

type Gateways struct {
	Pool   *pgxpool.Pool
	Secret []byte
}

var (
	ErrInvalidToken      = errors.New("invalid token")
	ErrTokenExpired      = errors.New("token expired")
	ErrTokenAlreadyUsed  = errors.New("token already claimed")
	ErrInvalidCredential = errors.New("invalid credential")
)

func (g *Gateways) hash(secret string) []byte {
	mac := hmac.New(sha256.New, g.Secret)
	mac.Write([]byte(secret))
	return mac.Sum(nil)
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (g *Gateways) Create(ctx context.Context, orgID, createdBy uuid.UUID, name, description string) (*models.Gateway, error) {
	const q = `
		insert into gateways (org_id, name, description, created_by)
		values ($1, $2, $3, $4)
		returning id, org_id, name, description, status, last_seen_at, created_by, created_at`
	gw := &models.Gateway{}
	err := g.Pool.QueryRow(ctx, q, orgID, name, description, createdBy).Scan(
		&gw.ID, &gw.OrgID, &gw.Name, &gw.Description, &gw.Status, &gw.LastSeenAt, &gw.CreatedBy, &gw.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert gateway: %w", err)
	}
	return gw, nil
}

func (g *Gateways) List(ctx context.Context, orgID uuid.UUID) ([]models.Gateway, error) {
	const q = `
		select id, org_id, name, description, status, last_seen_at, created_by, created_at
		from gateways
		where org_id = $1
		order by created_at desc`
	rows, err := g.Pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Gateway{}
	for rows.Next() {
		var gw models.Gateway
		if err := rows.Scan(&gw.ID, &gw.OrgID, &gw.Name, &gw.Description, &gw.Status, &gw.LastSeenAt, &gw.CreatedBy, &gw.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, gw)
	}
	return out, rows.Err()
}

func (g *Gateways) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Gateway, error) {
	const q = `
		select id, org_id, name, description, status, last_seen_at, created_by, created_at
		from gateways
		where org_id = $1 and id = $2`
	gw := &models.Gateway{}
	err := g.Pool.QueryRow(ctx, q, orgID, id).Scan(
		&gw.ID, &gw.OrgID, &gw.Name, &gw.Description, &gw.Status, &gw.LastSeenAt, &gw.CreatedBy, &gw.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return gw, nil
}

func (g *Gateways) Revoke(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := g.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const gwQ = `update gateways set status = 'revoked' where org_id = $1 and id = $2`
	tag, err := tx.Exec(ctx, gwQ, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	const credQ = `update gateway_credentials set revoked_at = now() where gateway_id = $1 and revoked_at is null`
	if _, err := tx.Exec(ctx, credQ, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (g *Gateways) TouchSeen(ctx context.Context, gatewayID uuid.UUID) error {
	const q = `update gateways set last_seen_at = now(), status = 'online' where id = $1 and status != 'revoked'`
	_, err := g.Pool.Exec(ctx, q, gatewayID)
	return err
}

func (g *Gateways) IssueEnrollmentToken(ctx context.Context, gateway *models.Gateway, createdBy uuid.UUID, ttl time.Duration) (string, error) {
	raw, err := randHex(32)
	if err != nil {
		return "", err
	}
	plaintext := "bg_enroll_" + raw
	tokenHash := g.hash(plaintext)
	const q = `
		insert into gateway_enrollment_tokens (org_id, gateway_id, token_hash, expires_at, created_by)
		values ($1, $2, $3, $4, $5)`
	if _, err := g.Pool.Exec(ctx, q, gateway.OrgID, gateway.ID, tokenHash, time.Now().Add(ttl), createdBy); err != nil {
		return "", fmt.Errorf("insert enrollment token: %w", err)
	}
	return plaintext, nil
}

// ClaimEnrollmentToken mints a gateway credential but does NOT finalize the
// claim — claimed_at stays null until the gateway successfully heartbeats with
// its issued credential (see CommitEnrollmentOnHeartbeat). If a prior issue
// failed to land on a shim (no heartbeat), we revoke the abandoned credential
// and mint a fresh one so the user can retry the install.
func (g *Gateways) ClaimEnrollmentToken(ctx context.Context, plaintext string) (*models.Gateway, string, error) {
	tokenHash := g.hash(plaintext)
	tx, err := g.Pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		tokenID            uuid.UUID
		gatewayID          uuid.UUID
		expiresAt          time.Time
		claimedAt          *time.Time
		commitPending      bool
		issuedCredentialID *uuid.UUID
	)
	const sel = `
		select id, gateway_id, expires_at, claimed_at, commit_pending, issued_credential_id
		from gateway_enrollment_tokens
		where token_hash = $1`
	err = tx.QueryRow(ctx, sel, tokenHash).Scan(&tokenID, &gatewayID, &expiresAt, &claimedAt, &commitPending, &issuedCredentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrInvalidToken
	}
	if err != nil {
		return nil, "", err
	}
	if claimedAt != nil {
		return nil, "", ErrTokenAlreadyUsed
	}
	if time.Now().After(expiresAt) {
		return nil, "", ErrTokenExpired
	}

	gw := &models.Gateway{}
	const gwQ = `
		select id, org_id, name, description, status, last_seen_at, created_by, created_at
		from gateways where id = $1`
	if err := tx.QueryRow(ctx, gwQ, gatewayID).Scan(
		&gw.ID, &gw.OrgID, &gw.Name, &gw.Description, &gw.Status, &gw.LastSeenAt, &gw.CreatedBy, &gw.CreatedAt,
	); err != nil {
		return nil, "", err
	}
	if gw.Status == "revoked" {
		return nil, "", ErrInvalidToken
	}

	// A prior claim that has already led to a successful heartbeat is final —
	// commit_pending will be cleared by CommitEnrollmentOnHeartbeat, but defend
	// against re-claim after heartbeat in case claimed_at fixup raced.
	if commitPending && gw.LastSeenAt != nil {
		return nil, "", ErrTokenAlreadyUsed
	}

	// Re-claim path: revoke the abandoned credential before minting a new one.
	if commitPending && issuedCredentialID != nil {
		const rev = `update gateway_credentials set revoked_at = now() where id = $1 and revoked_at is null`
		if _, err := tx.Exec(ctx, rev, *issuedCredentialID); err != nil {
			return nil, "", err
		}
	}

	secret, err := randHex(32)
	if err != nil {
		return nil, "", err
	}
	plainCred := "bg_gw_" + gw.ID.String() + "." + secret
	credHash := g.hash(plainCred)
	var newCredID uuid.UUID
	const insCred = `insert into gateway_credentials (gateway_id, secret_hash) values ($1, $2) returning id`
	if err := tx.QueryRow(ctx, insCred, gw.ID, credHash).Scan(&newCredID); err != nil {
		return nil, "", err
	}

	const markPending = `
		update gateway_enrollment_tokens
		   set commit_pending = true, issued_credential_id = $2
		 where id = $1`
	if _, err := tx.Exec(ctx, markPending, tokenID, newCredID); err != nil {
		return nil, "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return gw, plainCred, nil
}

// CommitEnrollmentOnHeartbeat finalizes any pending enrollment for this gateway.
// Idempotent: safe to call on every heartbeat.
func (g *Gateways) CommitEnrollmentOnHeartbeat(ctx context.Context, gatewayID uuid.UUID) error {
	const q = `
		update gateway_enrollment_tokens
		   set claimed_at = now(), commit_pending = false
		 where commit_pending = true
		   and issued_credential_id in (
		       select id from gateway_credentials where gateway_id = $1
		   )`
	_, err := g.Pool.Exec(ctx, q, gatewayID)
	return err
}

func (g *Gateways) AuthenticateCredential(ctx context.Context, plaintext string) (*models.Gateway, error) {
	if !strings.HasPrefix(plaintext, "bg_gw_") {
		return nil, ErrInvalidCredential
	}
	rest := strings.TrimPrefix(plaintext, "bg_gw_")
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return nil, ErrInvalidCredential
	}
	gwID, err := uuid.Parse(rest[:dot])
	if err != nil {
		return nil, ErrInvalidCredential
	}
	candidate := g.hash(plaintext)

	const q = `
		select c.secret_hash, c.revoked_at,
		       g.id, g.org_id, g.name, g.description, g.status, g.last_seen_at, g.created_by, g.created_at
		from gateway_credentials c
		join gateways g on g.id = c.gateway_id
		where c.gateway_id = $1`
	rows, err := g.Pool.Query(ctx, q, gwID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stored []byte
		var revokedAt *time.Time
		gw := &models.Gateway{}
		if err := rows.Scan(&stored, &revokedAt,
			&gw.ID, &gw.OrgID, &gw.Name, &gw.Description, &gw.Status, &gw.LastSeenAt, &gw.CreatedBy, &gw.CreatedAt,
		); err != nil {
			return nil, err
		}
		if revokedAt != nil {
			continue
		}
		if gw.Status == "revoked" {
			continue
		}
		if hmac.Equal(stored, candidate) {
			return gw, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrInvalidCredential
}
