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
	return c.encryptBytes(plaintext)
}

func (c *Connections) encryptBytes(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.AEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := c.AEAD.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ct...), nil
}

func (c *Connections) decryptBytes(blob []byte) ([]byte, error) {
	ns := c.AEAD.NonceSize()
	if len(blob) < ns {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	return c.AEAD.Open(nil, nonce, ct, nil)
}

// Decrypt reverses Encrypt. Returns an empty AuthSecret if the blob is nil.
func (c *Connections) Decrypt(blob []byte) (mcp.AuthSecret, error) {
	if len(blob) == 0 {
		return mcp.AuthSecret{}, nil
	}
	pt, err := c.decryptBytes(blob)
	if err != nil {
		return mcp.AuthSecret{}, fmt.Errorf("decrypt: %w", err)
	}
	var s mcp.AuthSecret
	if err := json.Unmarshal(pt, &s); err != nil {
		return mcp.AuthSecret{}, err
	}
	return s, nil
}

// CreateOpts captures every persisted field at Create time. oauth_status is
// set up front for OAuth connections so the UI can render the right chip
// before the user completes the dance.
type CreateOpts struct {
	OrgID       uuid.UUID
	CreatedBy   uuid.UUID
	Name        string
	Endpoint    string
	Transport   string
	Method      models.AuthMethod
	Secret      mcp.AuthSecret
	OAuthStatus string
}

func (c *Connections) Create(ctx context.Context, orgID, createdBy uuid.UUID, name, endpoint, transport string, method models.AuthMethod, secret mcp.AuthSecret) (*models.MCPConnection, error) {
	return c.CreateWithOpts(ctx, CreateOpts{
		OrgID:     orgID,
		CreatedBy: createdBy,
		Name:      name,
		Endpoint:  endpoint,
		Transport: transport,
		Method:    method,
		Secret:    secret,
	})
}

func (c *Connections) CreateWithOpts(ctx context.Context, opts CreateOpts) (*models.MCPConnection, error) {
	var authState []byte
	if opts.Method != "" {
		blob, err := c.Encrypt(opts.Secret)
		if err != nil {
			return nil, fmt.Errorf("encrypt auth state: %w", err)
		}
		authState = blob
	}
	const q = `
		insert into mcp_connections (org_id, name, endpoint_url, transport, auth_method, auth_state, created_by, oauth_status)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		returning id, org_id, name, endpoint_url, transport, auth_method, status, last_error,
		         last_discovered_at, mcp_server_id, created_by, created_at, updated_at, oauth_status`
	out := &models.MCPConnection{}
	err := c.Pool.QueryRow(ctx, q,
		opts.OrgID, opts.Name, opts.Endpoint, opts.Transport, opts.Method, authState, opts.CreatedBy, opts.OAuthStatus,
	).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.EndpointURL, &out.Transport, &out.AuthMethod, &out.Status, &out.LastError,
		&out.LastDiscoveredAt, &out.MCPServerID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt, &out.OAuthStatus,
	)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Connections) Get(ctx context.Context, orgID, id uuid.UUID) (*models.MCPConnection, error) {
	const q = `
		select id, org_id, name, endpoint_url, transport, auth_method, status, last_error,
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at, oauth_status
		from mcp_connections where org_id = $1 and id = $2`
	out := &models.MCPConnection{}
	err := c.Pool.QueryRow(ctx, q, orgID, id).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.EndpointURL, &out.Transport, &out.AuthMethod, &out.Status, &out.LastError,
		&out.LastDiscoveredAt, &out.MCPServerID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt, &out.OAuthStatus,
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
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at, oauth_status
		from mcp_connections where id = $1`
	out := &models.MCPConnection{}
	var authState []byte
	err := c.Pool.QueryRow(ctx, q, id).Scan(
		&out.ID, &out.OrgID, &out.Name, &out.EndpointURL, &out.Transport, &out.AuthMethod, &authState, &out.Status, &out.LastError,
		&out.LastDiscoveredAt, &out.MCPServerID, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt, &out.OAuthStatus,
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
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at, oauth_status
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
			&m.LastDiscoveredAt, &m.MCPServerID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.OAuthStatus,
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

// UpdateAuthState re-encrypts and persists the auth secret for a connection.
// Used after OAuth token rotation to write back the new access/refresh tokens.
func (c *Connections) UpdateAuthState(ctx context.Context, id uuid.UUID, secret mcp.AuthSecret) error {
	blob, err := c.Encrypt(secret)
	if err != nil {
		return err
	}
	const q = `update mcp_connections set auth_state = $2, updated_at = now() where id = $1`
	tag, err := c.Pool.Exec(ctx, q, id, blob)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateOAuthStatus is a lightweight status-only update for the OAuth UI chip.
func (c *Connections) UpdateOAuthStatus(ctx context.Context, id uuid.UUID, status string) error {
	const q = `update mcp_connections set oauth_status = $2, updated_at = now() where id = $1`
	tag, err := c.Pool.Exec(ctx, q, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListDue returns connections whose last_discovered_at is null or older than
// `before`. Used by the scheduler. OAuth connections that haven't completed
// the authorize dance yet are excluded — there's no token to use against the
// MCP endpoint, and probing would just produce 401s.
func (c *Connections) ListDue(ctx context.Context, before time.Time, limit int) ([]models.MCPConnection, error) {
	const q = `
		select id, org_id, name, endpoint_url, transport, auth_method, status, last_error,
		       last_discovered_at, mcp_server_id, created_by, created_at, updated_at, oauth_status
		from mcp_connections
		where (last_discovered_at is null or last_discovered_at < $1)
		  and (auth_method <> 'oauth2_authcode' or oauth_status = 'authorized')
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
			&m.LastDiscoveredAt, &m.MCPServerID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.OAuthStatus,
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

// ----- OAuth authorize-state ops -----

// OAuthState is the in-memory form of an oauth_authcode_states row. The
// code_verifier is returned in plaintext; callers should treat it as a one-time
// secret and never persist it back to disk.
type OAuthState struct {
	State        string
	ConnectionID uuid.UUID
	OrgID        uuid.UUID
	UserID       uuid.UUID
	CodeVerifier string
	ReturnTo     string
	ExpiresAt    time.Time
}

func (c *Connections) PutOAuthState(ctx context.Context, s OAuthState) error {
	enc, err := c.encryptBytes([]byte(s.CodeVerifier))
	if err != nil {
		return err
	}
	const q = `
		insert into oauth_authcode_states (state, connection_id, org_id, user_id, code_verifier, return_to, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7)`
	_, err = c.Pool.Exec(ctx, q, s.State, s.ConnectionID, s.OrgID, s.UserID, enc, s.ReturnTo, s.ExpiresAt)
	return err
}

// TakeOAuthState atomically reads and deletes a state row. Returning the row
// means the caller is authoritative for the rest of the dance; a second
// callback for the same `state` will get ErrNotFound.
func (c *Connections) TakeOAuthState(ctx context.Context, state string) (*OAuthState, error) {
	const q = `
		delete from oauth_authcode_states
		where state = $1
		returning connection_id, org_id, user_id, code_verifier, return_to, expires_at`
	var out OAuthState
	out.State = state
	var encVerifier []byte
	err := c.Pool.QueryRow(ctx, q, state).Scan(
		&out.ConnectionID, &out.OrgID, &out.UserID, &encVerifier, &out.ReturnTo, &out.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	pt, err := c.decryptBytes(encVerifier)
	if err != nil {
		return nil, err
	}
	out.CodeVerifier = string(pt)
	return &out, nil
}

// SweepExpiredOAuthStates removes rows past their TTL. Safe to call periodically.
func (c *Connections) SweepExpiredOAuthStates(ctx context.Context) (int64, error) {
	tag, err := c.Pool.Exec(ctx, `delete from oauth_authcode_states where expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ----- mcp.TokenStore adaptation -----

// LoadAuthSecret implements mcp.TokenStore.
func (c *Connections) LoadAuthSecret(ctx context.Context, connID [16]byte) (mcp.AuthSecret, error) {
	const q = `select auth_state, auth_method from mcp_connections where id = $1`
	var blob []byte
	var method string
	if err := c.Pool.QueryRow(ctx, q, uuid.UUID(connID)).Scan(&blob, &method); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mcp.AuthSecret{}, ErrNotFound
		}
		return mcp.AuthSecret{}, err
	}
	secret, err := c.Decrypt(blob)
	if err != nil {
		return mcp.AuthSecret{}, err
	}
	secret.Method = method
	return secret, nil
}

// SaveAuthSecret implements mcp.TokenStore.
func (c *Connections) SaveAuthSecret(ctx context.Context, connID [16]byte, secret mcp.AuthSecret) error {
	return c.UpdateAuthState(ctx, uuid.UUID(connID), secret)
}

// MarkOAuthStatus implements mcp.TokenStore.
func (c *Connections) MarkOAuthStatus(ctx context.Context, connID [16]byte, status string) error {
	return c.UpdateOAuthStatus(ctx, uuid.UUID(connID), status)
}
