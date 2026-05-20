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

type Sessions struct {
	Pool   *pgxpool.Pool
	TTL    time.Duration
	Secret []byte
}

func (s *Sessions) hash(secret string) []byte {
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte(secret))
	return mac.Sum(nil)
}

func (s *Sessions) Create(ctx context.Context, userID uuid.UUID, userAgent string) (*models.Session, error) {
	expires := time.Now().Add(s.TTL)
	const q = `
		insert into sessions (user_id, expires_at, user_agent, kind, label)
		values ($1, $2, $3, 'cookie', '')
		returning id, user_id, active_org_id, kind, label, created_at, last_seen_at, expires_at`
	sess := &models.Session{}
	err := s.Pool.QueryRow(ctx, q, userID, expires, userAgent).Scan(
		&sess.ID, &sess.UserID, &sess.ActiveOrgID, &sess.Kind, &sess.Label, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// CreateCLI creates a kind='cli' session and returns the session row plus the
// plaintext bearer token (only returned here; the hash is what's stored).
func (s *Sessions) CreateCLI(ctx context.Context, userID uuid.UUID, label, secret string, ttl time.Duration) (*models.Session, error) {
	expires := time.Now().Add(ttl)
	tokenHash := s.hash(secret)
	const q = `
		insert into sessions (user_id, expires_at, user_agent, kind, label, token_hash)
		values ($1, $2, '', 'cli', $3, $4)
		returning id, user_id, active_org_id, kind, label, created_at, last_seen_at, expires_at`
	sess := &models.Session{}
	err := s.Pool.QueryRow(ctx, q, userID, expires, label, tokenHash).Scan(
		&sess.ID, &sess.UserID, &sess.ActiveOrgID, &sess.Kind, &sess.Label, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Sessions) Get(ctx context.Context, id uuid.UUID) (*models.Session, error) {
	const q = `
		select id, user_id, active_org_id, kind, label, created_at, last_seen_at, expires_at
		from sessions
		where id = $1 and expires_at > now()`
	sess := &models.Session{}
	err := s.Pool.QueryRow(ctx, q, id).Scan(
		&sess.ID, &sess.UserID, &sess.ActiveOrgID, &sess.Kind, &sess.Label, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// GetCLIByToken looks up a kind='cli' session by id and constant-time compares
// the secret against the stored token_hash.
func (s *Sessions) GetCLIByToken(ctx context.Context, id uuid.UUID, secret string) (*models.Session, error) {
	const q = `
		select id, user_id, active_org_id, kind, label, token_hash, created_at, last_seen_at, expires_at
		from sessions
		where id = $1 and kind = 'cli' and expires_at > now()`
	sess := &models.Session{}
	var stored []byte
	err := s.Pool.QueryRow(ctx, q, id).Scan(
		&sess.ID, &sess.UserID, &sess.ActiveOrgID, &sess.Kind, &sess.Label, &stored, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if hmac.Equal(stored, s.hash(secret)) {
		return sess, nil
	}
	return nil, ErrNotFound
}

func (s *Sessions) Touch(ctx context.Context, id uuid.UUID) error {
	const q = `update sessions set last_seen_at = now() where id = $1`
	_, err := s.Pool.Exec(ctx, q, id)
	return err
}

func (s *Sessions) SetActiveOrg(ctx context.Context, sessionID, orgID uuid.UUID) error {
	const q = `update sessions set active_org_id = $2 where id = $1`
	_, err := s.Pool.Exec(ctx, q, sessionID, orgID)
	return err
}

func (s *Sessions) Delete(ctx context.Context, id uuid.UUID) error {
	const q = `delete from sessions where id = $1`
	_, err := s.Pool.Exec(ctx, q, id)
	return err
}

func (s *Sessions) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.Session, error) {
	const q = `
		select id, user_id, active_org_id, kind, label, created_at, last_seen_at, expires_at
		from sessions
		where user_id = $1 and expires_at > now()
		order by created_at desc`
	rows, err := s.Pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Session{}
	for rows.Next() {
		var sess models.Session
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.ActiveOrgID, &sess.Kind, &sess.Label, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// DeleteForUser deletes a session only if it belongs to the given user.
// Returns ErrNotFound if no row matched.
func (s *Sessions) DeleteForUser(ctx context.Context, userID, id uuid.UUID) error {
	const q = `delete from sessions where id = $1 and user_id = $2`
	tag, err := s.Pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
