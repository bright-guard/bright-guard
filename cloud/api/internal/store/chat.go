package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Chat is the persistence layer for chat_sessions, chat_messages, and the
// daily token-usage ledger used by the budget check.
type Chat struct {
	Pool *pgxpool.Pool
}

type ChatSession struct {
	ID            uuid.UUID `json:"id"`
	OrgID         uuid.UUID `json:"orgId"`
	UserID        uuid.UUID `json:"userId"`
	Title         string    `json:"title"`
	TotalTokens   int64     `json:"totalTokens"`
	CreatedAt     time.Time `json:"createdAt"`
	LastMessageAt time.Time `json:"lastMessageAt"`
}

type ChatMessage struct {
	ID           uuid.UUID       `json:"id"`
	SessionID    uuid.UUID       `json:"sessionId"`
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	ToolCalls    json.RawMessage `json:"toolCalls,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
}

func (c *Chat) CreateSession(ctx context.Context, orgID, userID uuid.UUID, title string) (*ChatSession, error) {
	const q = `
		insert into chat_sessions (org_id, user_id, title)
		values ($1, $2, $3)
		returning id, org_id, user_id, coalesce(title, ''), total_tokens, created_at, last_message_at`
	s := &ChatSession{}
	var t *string
	if title != "" {
		t = &title
	}
	if err := c.Pool.QueryRow(ctx, q, orgID, userID, t).Scan(
		&s.ID, &s.OrgID, &s.UserID, &s.Title, &s.TotalTokens, &s.CreatedAt, &s.LastMessageAt,
	); err != nil {
		return nil, err
	}
	return s, nil
}

func (c *Chat) GetSession(ctx context.Context, orgID, userID, sessionID uuid.UUID) (*ChatSession, error) {
	const q = `
		select id, org_id, user_id, coalesce(title, ''), total_tokens, created_at, last_message_at
		from chat_sessions
		where id = $1 and org_id = $2 and user_id = $3`
	s := &ChatSession{}
	err := c.Pool.QueryRow(ctx, q, sessionID, orgID, userID).Scan(
		&s.ID, &s.OrgID, &s.UserID, &s.Title, &s.TotalTokens, &s.CreatedAt, &s.LastMessageAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ListSessions returns the user's last-30-days sessions, newest first.
func (c *Chat) ListSessions(ctx context.Context, orgID, userID uuid.UUID, limit int) ([]ChatSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	const q = `
		select id, org_id, user_id, coalesce(title, ''), total_tokens, created_at, last_message_at
		from chat_sessions
		where org_id = $1 and user_id = $2 and last_message_at > now() - interval '30 days'
		order by last_message_at desc
		limit $3`
	rows, err := c.Pool.Query(ctx, q, orgID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatSession{}
	for rows.Next() {
		var s ChatSession
		if err := rows.Scan(&s.ID, &s.OrgID, &s.UserID, &s.Title, &s.TotalTokens, &s.CreatedAt, &s.LastMessageAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (c *Chat) DeleteSession(ctx context.Context, orgID, userID, sessionID uuid.UUID) error {
	const q = `delete from chat_sessions where id = $1 and org_id = $2 and user_id = $3`
	tag, err := c.Pool.Exec(ctx, q, sessionID, orgID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type AppendMessage struct {
	Role         string
	Content      json.RawMessage
	InputTokens  int
	OutputTokens int
	ToolCalls    json.RawMessage
}

// AppendMessage writes one message and bumps the session's last_message_at +
// total_tokens. Returns the persisted message.
func (c *Chat) AppendMessage(ctx context.Context, sessionID uuid.UUID, m AppendMessage) (*ChatMessage, error) {
	tx, err := c.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := &ChatMessage{}
	const insQ = `
		insert into chat_messages (session_id, role, content, input_tokens, output_tokens, tool_calls)
		values ($1, $2, $3, $4, $5, $6)
		returning id, session_id, role, content, input_tokens, output_tokens, tool_calls, created_at`
	var tc any
	if len(m.ToolCalls) > 0 {
		tc = []byte(m.ToolCalls)
	}
	if err := tx.QueryRow(ctx, insQ,
		sessionID, m.Role, jsonOrEmpty(m.Content), m.InputTokens, m.OutputTokens, tc,
	).Scan(
		&out.ID, &out.SessionID, &out.Role, &out.Content,
		&out.InputTokens, &out.OutputTokens, &out.ToolCalls, &out.CreatedAt,
	); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		update chat_sessions
		set last_message_at = now(),
		    total_tokens = total_tokens + $2
		where id = $1`, sessionID, int64(m.InputTokens+m.OutputTokens)); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (c *Chat) ListMessages(ctx context.Context, sessionID uuid.UUID) ([]ChatMessage, error) {
	const q = `
		select id, session_id, role, content, input_tokens, output_tokens, tool_calls, created_at
		from chat_messages
		where session_id = $1
		order by created_at asc, id asc`
	rows, err := c.Pool.Query(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content,
			&m.InputTokens, &m.OutputTokens, &m.ToolCalls, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetTitle updates the session title (typically derived from the first user message).
func (c *Chat) SetTitle(ctx context.Context, sessionID uuid.UUID, title string) error {
	const q = `update chat_sessions set title = $2 where id = $1 and (title is null or title = '')`
	_, err := c.Pool.Exec(ctx, q, sessionID, title)
	return err
}

// DailyUsage returns the org's accumulated chat token usage for the UTC day of t.
func (c *Chat) DailyUsage(ctx context.Context, orgID uuid.UUID, t time.Time) (int64, int64, error) {
	day := t.UTC().Format("2006-01-02")
	const q = `
		select coalesce(input_tokens, 0), coalesce(output_tokens, 0)
		from chat_daily_usage
		where org_id = $1 and day = $2`
	var in, out int64
	err := c.Pool.QueryRow(ctx, q, orgID, day).Scan(&in, &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return in, out, nil
}

// AddUsage upserts today's per-org usage counters.
func (c *Chat) AddUsage(ctx context.Context, orgID uuid.UUID, t time.Time, inputTokens, outputTokens int64) error {
	day := t.UTC().Format("2006-01-02")
	const q = `
		insert into chat_daily_usage (org_id, day, input_tokens, output_tokens)
		values ($1, $2, $3, $4)
		on conflict (org_id, day) do update set
		  input_tokens  = chat_daily_usage.input_tokens  + excluded.input_tokens,
		  output_tokens = chat_daily_usage.output_tokens + excluded.output_tokens`
	_, err := c.Pool.Exec(ctx, q, orgID, day, inputTokens, outputTokens)
	return err
}
