package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/mcp"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

// Connections is the data layer for mcp_connections rows and the at-rest
// encryption of their auth secrets.
type Connections struct {
	Pool *pgxpool.Pool
	AEAD cipher.AEAD
}

// NewAEAD derives an AES-256-GCM key from SESSION_SECRET. This is intentionally
// a stop-gap for MVP; production should mount a KMS-managed key instead.
// TODO: replace with KMS-wrapped DEK before opening connections to real customer secrets.
func NewAEAD(sessionSecret []byte) (cipher.AEAD, error) {
	h := sha256.New()
	h.Write([]byte("bg-conn-aead\n"))
	h.Write(sessionSecret)
	key := h.Sum(nil)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt serializes an AuthSecret and returns nonce||ciphertext.
func (c *Connections) Encrypt(s mcp.AuthSecret) ([]byte, error) {
	plaintext, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, c.AEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := c.AEAD.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ct...), nil
}

// Decrypt reverses Encrypt. Returns an empty AuthSecret if the blob is nil.
func (c *Connections) Decrypt(blob []byte) (mcp.AuthSecret, error) {
	if len(blob) == 0 {
		return mcp.AuthSecret{}, nil
	}
	ns := c.AEAD.NonceSize()
	if len(blob) < ns {
		return mcp.AuthSecret{}, errors.New("auth_state too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	pt, err := c.AEAD.Open(nil, nonce, ct, nil)
	if err != nil {
		return mcp.AuthSecret{}, fmt.Errorf("decrypt: %w", err)
	}
	var s mcp.AuthSecret
	if err := json.Unmarshal(pt, &s); err != nil {
		return mcp.AuthSecret{}, err
	}
	return s, nil
}

func (c *Connections) Create(ctx context.Context, orgID, createdBy uuid.UUID, name, endpoint, transport string, method models.AuthMethod, secret mcp.AuthSecret) (*models.MCPConnection, error) {
	var authState []byte
	if method != "" {
		blob, err := c.Encrypt(secret)
		if err != nil {
			return nil, fmt.Errorf("encrypt auth state: %w", err)
		}
		authState = blob
	}
	const q = `
		insert into mcp_connections (org_id, name, endpoint_url, transport, auth_method, auth_state, created_by)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning id, org_id, name, endpoint_url, transport, auth_method, status, last_error,
		         last_discovered_at, mcp_server_id, created_by, created_at, updated_at`
	out := &models.MCPConnection{}
	err := c.Pool.QueryRow(ctx, q, orgID, name, endpoint, transport, method, authState, createdBy).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.EndpointURL, &out.Transport, &out.AuthMethod, &out.Status, &out.LastError,
		&out.LastDiscoveredAt, &out.MCPServerID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Connections) Get(ctx context.Context, orgID, id uuid.UUID) (*models.MCPConnection, error) {
	const q = `
		select id, org_id, name, endpoint_url, transport, auth_method, status, last_error,
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at
		from mcp_connections where org_id = $1 and id = $2`
	out := &models.MCPConnection{}
	err := c.Pool.QueryRow(ctx, q, orgID, id).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.EndpointURL, &out.Transport, &out.AuthMethod, &out.Status, &out.LastError,
		&out.LastDiscoveredAt, &out.MCPServerID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetWithSecret is for internal callers (discovery scheduler) that need the
// decrypted auth secret. Never expose the result through the HTTP layer.
func (c *Connections) GetWithSecret(ctx context.Context, id uuid.UUID) (*models.MCPConnection, mcp.AuthSecret, error) {
	const q = `
		select id, org_id, name, endpoint_url, transport, auth_method, auth_state, status, last_error,
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at
		from mcp_connections where id = $1`
	out := &models.MCPConnection{}
	var authState []byte
	err := c.Pool.QueryRow(ctx, q, id).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.EndpointURL, &out.Transport, &out.AuthMethod, &authState, &out.Status, &out.LastError,
		&out.LastDiscoveredAt, &out.MCPServerID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, mcp.AuthSecret{}, ErrNotFound
	}
	if err != nil {
		return nil, mcp.AuthSecret{}, err
	}
	secret, err := c.Decrypt(authState)
	if err != nil {
		return out, mcp.AuthSecret{}, err
	}
	secret.Method = string(out.AuthMethod)
	return out, secret, nil
}

func (c *Connections) List(ctx context.Context, orgID uuid.UUID) ([]models.MCPConnection, error) {
	const q = `
		select id, org_id, name, endpoint_url, transport, auth_method, status, last_error,
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at
		from mcp_connections where org_id = $1
		order by created_at desc`
	rows, err := c.Pool.Query(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.MCPConnection{}
	for rows.Next() {
		var m models.MCPConnection
		if err := rows.Scan(
			&m.ID, &m.OrgID, &m.Name, &m.EndpointURL, &m.Transport, &m.AuthMethod, &m.Status, &m.LastError,
			&m.LastDiscoveredAt, &m.MCPServerID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (c *Connections) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	const q = `delete from mcp_connections where org_id = $1 and id = $2`
	tag, err := c.Pool.Exec(ctx, q, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateAfterDiscovery records the outcome of a discovery cycle. mcpServerID
// is the row this connection now points at (or nil if discovery failed before
// we could upsert one).
func (c *Connections) UpdateAfterDiscovery(ctx context.Context, id uuid.UUID, mcpServerID *uuid.UUID, status, lastErr string) error {
	const q = `
		update mcp_connections set
		  status = $2,
		  last_error = $3,
		  last_discovered_at = now(),
		  mcp_server_id = coalesce($4, mcp_server_id),
		  updated_at = now()
		where id = $1`
	_, err := c.Pool.Exec(ctx, q, id, status, lastErr, mcpServerID)
	return err
}

// ListDue returns connections whose last_discovered_at is null or older than
// `before`. Used by the scheduler.
func (c *Connections) ListDue(ctx context.Context, before time.Time, limit int) ([]models.MCPConnection, error) {
	const q = `
		select id, org_id, name, endpoint_url, transport, auth_method, status, last_error,
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at
		from mcp_connections
		where last_discovered_at is null or last_discovered_at < $1
		order by coalesce(last_discovered_at, 'epoch'::timestamptz) asc
		limit $2`
	rows, err := c.Pool.Query(ctx, q, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.MCPConnection{}
	for rows.Next() {
		var m models.MCPConnection
		if err := rows.Scan(
			&m.ID, &m.OrgID, &m.Name, &m.EndpointURL, &m.Transport, &m.AuthMethod, &m.Status, &m.LastError,
			&m.LastDiscoveredAt, &m.MCPServerID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TryAdvisoryLock acquires the discovery-scheduler advisory lock. Returns
// true if this process now holds it; lock is released on Pool close (or
// explicitly via pg_advisory_unlock — we rely on Cloud Run lifecycle for now).
func (c *Connections) TryAdvisoryLock(ctx context.Context, key string) (bool, error) {
	var ok bool
	const q = `select pg_try_advisory_lock(hashtext($1)::bigint)`
	if err := c.Pool.QueryRow(ctx, q, key).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}
