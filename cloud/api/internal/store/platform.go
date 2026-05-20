package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Platform is the data layer for platform_admins + platform_audit_log and the
// cross-tenant queries that power the backoffice console.
type Platform struct {
	Pool *pgxpool.Pool
	// SeedEmails is the lower-cased list of bootstrap-admin emails. On every
	// user upsert, if the email matches and there's no platform_admins row,
	// we insert one (system-added — added_by = NULL).
	SeedEmails []string
}

// MaybeBootstrap inserts a platform_admins row for the user iff their email
// matches the configured seed list. Idempotent — ON CONFLICT does nothing.
func (p *Platform) MaybeBootstrap(ctx context.Context, userID uuid.UUID, email string) error {
	if !p.isSeed(email) {
		return nil
	}
	const q = `
		insert into platform_admins (user_id, added_by)
		values ($1, null)
		on conflict (user_id) do nothing`
	_, err := p.Pool.Exec(ctx, q, userID)
	if err != nil {
		return fmt.Errorf("bootstrap platform admin: %w", err)
	}
	return nil
}

func (p *Platform) isSeed(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	for _, s := range p.SeedEmails {
		if s == e {
			return true
		}
	}
	return false
}

// IsActiveAdmin reports whether the user has an active (non-revoked) row.
func (p *Platform) IsActiveAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	const q = `select 1 from platform_admins where user_id = $1 and revoked_at is null`
	var x int
	err := p.Pool.QueryRow(ctx, q, userID).Scan(&x)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CountActiveAdmins returns the number of active platform_admins. Used by the
// demote path to refuse demoting the last active admin.
func (p *Platform) CountActiveAdmins(ctx context.Context) (int, error) {
	const q = `select count(*) from platform_admins where revoked_at is null`
	var n int
	if err := p.Pool.QueryRow(ctx, q).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Promote inserts (or re-activates) a platform_admins row. Uses upsert so a
// previously-demoted user becomes active again with a fresh added_by/added_at.
func (p *Platform) Promote(ctx context.Context, userID, actorID uuid.UUID) error {
	const q = `
		insert into platform_admins (user_id, added_by, added_at, revoked_at)
		values ($1, $2, now(), null)
		on conflict (user_id) do update
		set added_by   = excluded.added_by,
		    added_at   = now(),
		    revoked_at = null`
	_, err := p.Pool.Exec(ctx, q, userID, actorID)
	return err
}

// Demote sets revoked_at on the row. No-op if not active.
func (p *Platform) Demote(ctx context.Context, userID uuid.UUID) error {
	const q = `update platform_admins set revoked_at = now() where user_id = $1 and revoked_at is null`
	_, err := p.Pool.Exec(ctx, q, userID)
	return err
}

// Audit appends a row to platform_audit_log. Details are marshalled to jsonb.
// A nil/empty details map is stored as '{}'.
func (p *Platform) Audit(ctx context.Context, actorID uuid.UUID, action, targetKind string, targetID uuid.UUID, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}
	const q = `
		insert into platform_audit_log (actor_id, action, target_kind, target_id, details)
		values ($1, $2, $3, $4, $5)`
	_, err = p.Pool.Exec(ctx, q, actorID, action, targetKind, targetID, raw)
	return err
}

// AuditEntry is one row of the audit feed for the UI.
type AuditEntry struct {
	ID         uuid.UUID       `json:"id"`
	ActorID    uuid.UUID       `json:"actorId"`
	ActorEmail string          `json:"actorEmail"`
	Action     string          `json:"action"`
	TargetKind string          `json:"targetKind"`
	TargetID   uuid.UUID       `json:"targetId"`
	Details    json.RawMessage `json:"details"`
	At         time.Time       `json:"at"`
}

// ListAudit returns the most recent audit entries (most-recent first). Cursor
// reuses the existing (at,id) tuple encoding.
func (p *Platform) ListAudit(ctx context.Context, limit int, cursor string) ([]AuditEntry, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	args := []any{}
	where := ""
	if cursor != "" {
		curAt, curID, err := DecodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		args = append(args, curAt, curID)
		where = "where (l.at, l.id) < ($1, $2)"
	}
	args = append(args, limit+1)
	q := `
		select l.id, l.actor_id, coalesce(u.email,''), l.action, l.target_kind, l.target_id, l.details, l.at
		from platform_audit_log l
		left join users u on u.id = l.actor_id
		` + where + `
		order by l.at desc, l.id desc
		limit $` + fmt.Sprintf("%d", len(args))
	rows, err := p.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorEmail, &e.Action, &e.TargetKind, &e.TargetID, &e.Details, &e.At); err != nil {
			return nil, "", err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		last := out[limit-1]
		next = EncodeCursor(last.At, last.ID)
		out = out[:limit]
	}
	return out, next, nil
}

// Overview is the aggregate-metrics payload for the dashboard.
type Overview struct {
	Users        OverviewUsers        `json:"users"`
	Orgs         OverviewOrgs         `json:"orgs"`
	Gateways     OverviewGateways     `json:"gateways"`
	MCPServers   OverviewMCPServers   `json:"mcpServers"`
	Capabilities OverviewCapabilities `json:"capabilities"`
	Invocations  OverviewInvocations  `json:"invocations"`
	Connections  OverviewConnections  `json:"connections"`
	Callers      OverviewCallers      `json:"callers"`
}

type OverviewUsers struct {
	Total     int `json:"total"`
	Active30d int `json:"active30d"`
	NewLast7d int `json:"newLast7d"`
}
type OverviewOrgs struct {
	Total     int `json:"total"`
	NewLast7d int `json:"newLast7d"`
}
type OverviewGateways struct {
	Total  int `json:"total"`
	Online int `json:"online"`
}
type OverviewMCPServers struct {
	Total          int `json:"total"`
	PublicExposure int `json:"publicExposure"`
}
type OverviewCapabilities struct {
	Total  int                  `json:"total"`
	ByKind CapabilityKindTotals `json:"byKind"`
}
type OverviewInvocations struct {
	Last24h   int `json:"last24h"`
	Last7d    int `json:"last7d"`
	Denied24h int `json:"denied24h"`
}
type OverviewConnections struct {
	Total        int `json:"total"`
	OAuthPending int `json:"oauthPending"`
	NeedsReauth  int `json:"needsReauth"`
}
type OverviewCallers struct {
	Total      int `json:"total"`
	FlaggedNew int `json:"flaggedNew"`
}

// Overview gathers cross-tenant aggregate metrics. One query per concern keeps
// each statement cheap on the existing indexes; this is the dashboard hot path
// but is gated to platform admins and called sparingly.
func (p *Platform) Overview(ctx context.Context) (Overview, error) {
	var o Overview

	const usersQ = `
		select
			count(*),
			count(*) filter (where exists (
				select 1 from sessions s
				where s.user_id = u.id and s.last_seen_at > now() - interval '30 days'
			)),
			count(*) filter (where u.created_at > now() - interval '7 days')
		from users u`
	if err := p.Pool.QueryRow(ctx, usersQ).Scan(&o.Users.Total, &o.Users.Active30d, &o.Users.NewLast7d); err != nil {
		return o, fmt.Errorf("overview users: %w", err)
	}

	const orgsQ = `
		select count(*),
		       count(*) filter (where created_at > now() - interval '7 days')
		from orgs`
	if err := p.Pool.QueryRow(ctx, orgsQ).Scan(&o.Orgs.Total, &o.Orgs.NewLast7d); err != nil {
		return o, fmt.Errorf("overview orgs: %w", err)
	}

	const gwQ = `
		select count(*),
		       count(*) filter (where status = 'online')
		from gateways`
	if err := p.Pool.QueryRow(ctx, gwQ).Scan(&o.Gateways.Total, &o.Gateways.Online); err != nil {
		return o, fmt.Errorf("overview gateways: %w", err)
	}

	const mcpQ = `
		select count(*),
		       count(*) filter (where exposure_state = 'public')
		from mcp_servers`
	if err := p.Pool.QueryRow(ctx, mcpQ).Scan(&o.MCPServers.Total, &o.MCPServers.PublicExposure); err != nil {
		return o, fmt.Errorf("overview mcp servers: %w", err)
	}

	const capQ = `
		select count(*),
		       count(*) filter (where kind = 'tool'),
		       count(*) filter (where kind = 'resource'),
		       count(*) filter (where kind = 'prompt')
		from mcp_capabilities`
	if err := p.Pool.QueryRow(ctx, capQ).Scan(
		&o.Capabilities.Total,
		&o.Capabilities.ByKind.Tool,
		&o.Capabilities.ByKind.Resource,
		&o.Capabilities.ByKind.Prompt,
	); err != nil {
		return o, fmt.Errorf("overview capabilities: %w", err)
	}

	const invQ = `
		select
			count(*) filter (where at > now() - interval '24 hours'),
			count(*) filter (where at > now() - interval '7 days'),
			count(*) filter (where at > now() - interval '24 hours' and status = 'denied')
		from mcp_invocations`
	if err := p.Pool.QueryRow(ctx, invQ).Scan(&o.Invocations.Last24h, &o.Invocations.Last7d, &o.Invocations.Denied24h); err != nil {
		return o, fmt.Errorf("overview invocations: %w", err)
	}

	const connQ = `
		select count(*),
		       count(*) filter (where oauth_status = 'pending_authorize'),
		       count(*) filter (where oauth_status = 'needs_reauth' or oauth_status = 'expired_refresh')
		from mcp_connections`
	if err := p.Pool.QueryRow(ctx, connQ).Scan(&o.Connections.Total, &o.Connections.OAuthPending, &o.Connections.NeedsReauth); err != nil {
		return o, fmt.Errorf("overview connections: %w", err)
	}

	const callersQ = `
		select count(*),
		       count(*) filter (where flagged_new)
		from org_callers`
	if err := p.Pool.QueryRow(ctx, callersQ).Scan(&o.Callers.Total, &o.Callers.FlaggedNew); err != nil {
		return o, fmt.Errorf("overview callers: %w", err)
	}

	return o, nil
}

// --- Users (platform view) ---

type PlatformUser struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	DisplayName   string     `json:"displayName"`
	AvatarURL     string     `json:"avatarUrl"`
	CreatedAt     time.Time  `json:"createdAt"`
	OrgCount      int        `json:"orgCount"`
	LastSeenAt    *time.Time `json:"lastSeenAt,omitempty"`
	SuspendedAt   *time.Time `json:"suspendedAt,omitempty"`
	PlatformAdmin bool       `json:"platformAdmin"`
}

// ListUsers returns a page of users. Cursor uses (created_at, id) — newest
// first — to keep keyset pagination stable.
func (p *Platform) ListUsers(ctx context.Context, q string, limit int, cursor string) ([]PlatformUser, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	args := []any{}
	where := []string{}
	if s := strings.TrimSpace(q); s != "" {
		args = append(args, "%"+strings.ToLower(s)+"%")
		where = append(where, fmt.Sprintf("(lower(u.email) like $%d or lower(u.display_name) like $%d)", len(args), len(args)))
	}
	if cursor != "" {
		curAt, curID, err := DecodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		args = append(args, curAt, curID)
		where = append(where, fmt.Sprintf("(u.created_at, u.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "where " + strings.Join(where, " and ")
	}
	query := `
		select u.id, u.email, u.display_name, u.avatar_url, u.created_at,
			(select count(*) from org_members om where om.user_id = u.id)              as org_count,
			(select max(s.last_seen_at) from sessions s where s.user_id = u.id)        as last_seen_at,
			u.suspended_at,
			exists(select 1 from platform_admins pa where pa.user_id = u.id and pa.revoked_at is null) as is_admin
		from users u
		` + whereSQL + `
		order by u.created_at desc, u.id desc
		limit $` + fmt.Sprintf("%d", len(args))
	rows, err := p.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]PlatformUser, 0, limit)
	for rows.Next() {
		var u PlatformUser
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.CreatedAt,
			&u.OrgCount, &u.LastSeenAt, &u.SuspendedAt, &u.PlatformAdmin); err != nil {
			return nil, "", err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		last := out[limit-1]
		next = EncodeCursor(last.CreatedAt, last.ID)
		out = out[:limit]
	}
	return out, next, nil
}

type PlatformUserOrg struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
	Role string    `json:"role"`
}

type PlatformUserDetail struct {
	PlatformUser
	Orgs          []PlatformUserOrg `json:"orgs"`
	SessionCount  int               `json:"sessionCount"`
	LastActivityAt *time.Time       `json:"lastActivityAt,omitempty"`
}

func (p *Platform) UserDetail(ctx context.Context, id uuid.UUID) (*PlatformUserDetail, error) {
	const userQ = `
		select u.id, u.email, u.display_name, u.avatar_url, u.created_at,
			(select count(*) from org_members om where om.user_id = u.id),
			(select max(s.last_seen_at) from sessions s where s.user_id = u.id),
			u.suspended_at,
			exists(select 1 from platform_admins pa where pa.user_id = u.id and pa.revoked_at is null)
		from users u where u.id = $1`
	var d PlatformUserDetail
	err := p.Pool.QueryRow(ctx, userQ, id).Scan(
		&d.ID, &d.Email, &d.DisplayName, &d.AvatarURL, &d.CreatedAt,
		&d.OrgCount, &d.LastSeenAt, &d.SuspendedAt, &d.PlatformAdmin,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	const orgsQ = `
		select o.id, o.name, o.slug, m.role
		from org_members m
		join orgs o on o.id = m.org_id
		where m.user_id = $1
		order by o.created_at asc`
	rows, err := p.Pool.Query(ctx, orgsQ, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	d.Orgs = []PlatformUserOrg{}
	for rows.Next() {
		var o PlatformUserOrg
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Role); err != nil {
			return nil, err
		}
		d.Orgs = append(d.Orgs, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const sessQ = `select count(*) from sessions where user_id = $1 and expires_at > now()`
	if err := p.Pool.QueryRow(ctx, sessQ, id).Scan(&d.SessionCount); err != nil {
		return nil, err
	}

	// Last activity = most-recent invocation across any org the user belongs to.
	const actQ = `
		select max(i.at)
		from mcp_invocations i
		join org_members m on m.org_id = i.org_id
		where m.user_id = $1`
	if err := p.Pool.QueryRow(ctx, actQ, id).Scan(&d.LastActivityAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// SuspendUser sets suspended_at and clears the user's sessions in one shot.
// Returns ErrNotFound when no row was updated.
func (p *Platform) SuspendUser(ctx context.Context, id, actorID uuid.UUID) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `update users set suspended_at = now(), suspended_by = $2 where id = $1 and suspended_at is null`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either no such user, or already suspended. Verify which:
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from users where id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	if _, err := tx.Exec(ctx, `delete from sessions where user_id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Platform) UnsuspendUser(ctx context.Context, id uuid.UUID) error {
	tag, err := p.Pool.Exec(ctx, `update users set suspended_at = null, suspended_by = null where id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Verify existence to differentiate "no such user" from "already unsuspended".
		var exists bool
		if err := p.Pool.QueryRow(ctx, `select exists(select 1 from users where id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

// DeleteUser hard-deletes a user; child rows cascade. Returns the deleted
// email so the API layer can use it in audit details.
func (p *Platform) DeleteUser(ctx context.Context, id uuid.UUID) (string, error) {
	var email string
	err := p.Pool.QueryRow(ctx, `delete from users where id = $1 returning email`, id).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return email, nil
}

// UserEmail returns the email for the given user id (or ErrNotFound).
func (p *Platform) UserEmail(ctx context.Context, id uuid.UUID) (string, error) {
	var email string
	err := p.Pool.QueryRow(ctx, `select email from users where id = $1`, id).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// --- Orgs (platform view) ---

type PlatformOrg struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	CreatedBy      uuid.UUID  `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	MemberCount    int        `json:"memberCount"`
	GatewayCount   int        `json:"gatewayCount"`
	MCPServerCount int        `json:"mcpServerCount"`
	ConnectionCount int       `json:"connectionCount"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	SuspendedAt    *time.Time `json:"suspendedAt,omitempty"`
}

func (p *Platform) ListOrgs(ctx context.Context, q string, limit int, cursor string) ([]PlatformOrg, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	args := []any{}
	where := []string{}
	if s := strings.TrimSpace(q); s != "" {
		args = append(args, "%"+strings.ToLower(s)+"%")
		where = append(where, fmt.Sprintf("(lower(o.name) like $%d or lower(o.slug) like $%d)", len(args), len(args)))
	}
	if cursor != "" {
		curAt, curID, err := DecodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		args = append(args, curAt, curID)
		where = append(where, fmt.Sprintf("(o.created_at, o.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "where " + strings.Join(where, " and ")
	}
	query := `
		select o.id, o.name, o.slug, o.created_by, o.created_at,
			(select count(*) from org_members om where om.org_id = o.id),
			(select count(*) from gateways g where g.org_id = o.id),
			(select count(*) from mcp_servers s where s.org_id = o.id),
			(select count(*) from mcp_connections c where c.org_id = o.id),
			(select max(i.at) from mcp_invocations i where i.org_id = o.id),
			o.suspended_at
		from orgs o
		` + whereSQL + `
		order by o.created_at desc, o.id desc
		limit $` + fmt.Sprintf("%d", len(args))
	rows, err := p.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]PlatformOrg, 0, limit)
	for rows.Next() {
		var o PlatformOrg
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedBy, &o.CreatedAt,
			&o.MemberCount, &o.GatewayCount, &o.MCPServerCount, &o.ConnectionCount,
			&o.LastActivityAt, &o.SuspendedAt); err != nil {
			return nil, "", err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		last := out[limit-1]
		next = EncodeCursor(last.CreatedAt, last.ID)
		out = out[:limit]
	}
	return out, next, nil
}

type PlatformOrgMember struct {
	UserID      uuid.UUID `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
}

type PlatformOrgGateway struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

type PlatformOrgConnection struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	OAuthStatus string    `json:"oauthStatus"`
}

type PlatformOrgMCPServer struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	ExposureState string    `json:"exposureState"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
}

type PlatformOrgDetail struct {
	PlatformOrg
	Members      []PlatformOrgMember     `json:"members"`
	Gateways     []PlatformOrgGateway    `json:"gateways"`
	Connections  []PlatformOrgConnection `json:"connections"`
	MCPServers   []PlatformOrgMCPServer  `json:"mcpServers"`
}

func (p *Platform) OrgDetail(ctx context.Context, id uuid.UUID) (*PlatformOrgDetail, error) {
	const orgQ = `
		select o.id, o.name, o.slug, o.created_by, o.created_at,
			(select count(*) from org_members om where om.org_id = o.id),
			(select count(*) from gateways g where g.org_id = o.id),
			(select count(*) from mcp_servers s where s.org_id = o.id),
			(select count(*) from mcp_connections c where c.org_id = o.id),
			(select max(i.at) from mcp_invocations i where i.org_id = o.id),
			o.suspended_at
		from orgs o where o.id = $1`
	var d PlatformOrgDetail
	err := p.Pool.QueryRow(ctx, orgQ, id).Scan(
		&d.ID, &d.Name, &d.Slug, &d.CreatedBy, &d.CreatedAt,
		&d.MemberCount, &d.GatewayCount, &d.MCPServerCount, &d.ConnectionCount,
		&d.LastActivityAt, &d.SuspendedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	const memberQ = `
		select m.user_id, u.email, u.display_name, m.role::text
		from org_members m
		join users u on u.id = m.user_id
		where m.org_id = $1
		order by m.created_at asc`
	mrows, err := p.Pool.Query(ctx, memberQ, id)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	d.Members = []PlatformOrgMember{}
	for mrows.Next() {
		var m PlatformOrgMember
		if err := mrows.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role); err != nil {
			return nil, err
		}
		d.Members = append(d.Members, m)
	}
	if err := mrows.Err(); err != nil {
		return nil, err
	}

	const gwQ = `select id, name, status, last_seen_at from gateways where org_id = $1 order by created_at asc`
	grows, err := p.Pool.Query(ctx, gwQ, id)
	if err != nil {
		return nil, err
	}
	defer grows.Close()
	d.Gateways = []PlatformOrgGateway{}
	for grows.Next() {
		var g PlatformOrgGateway
		if err := grows.Scan(&g.ID, &g.Name, &g.Status, &g.LastSeenAt); err != nil {
			return nil, err
		}
		d.Gateways = append(d.Gateways, g)
	}
	if err := grows.Err(); err != nil {
		return nil, err
	}

	const connQ = `select id, name, status, oauth_status from mcp_connections where org_id = $1 order by created_at asc`
	crows, err := p.Pool.Query(ctx, connQ, id)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	d.Connections = []PlatformOrgConnection{}
	for crows.Next() {
		var c PlatformOrgConnection
		if err := crows.Scan(&c.ID, &c.Name, &c.Status, &c.OAuthStatus); err != nil {
			return nil, err
		}
		d.Connections = append(d.Connections, c)
	}
	if err := crows.Err(); err != nil {
		return nil, err
	}

	const mcpQ = `select id, name, exposure_state, last_seen_at from mcp_servers where org_id = $1 order by last_seen_at desc`
	srows, err := p.Pool.Query(ctx, mcpQ, id)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	d.MCPServers = []PlatformOrgMCPServer{}
	for srows.Next() {
		var s PlatformOrgMCPServer
		if err := srows.Scan(&s.ID, &s.Name, &s.ExposureState, &s.LastSeenAt); err != nil {
			return nil, err
		}
		d.MCPServers = append(d.MCPServers, s)
	}
	return &d, srows.Err()
}

func (p *Platform) SuspendOrg(ctx context.Context, id, actorID uuid.UUID) error {
	tag, err := p.Pool.Exec(ctx, `update orgs set suspended_at = now(), suspended_by = $2 where id = $1 and suspended_at is null`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := p.Pool.QueryRow(ctx, `select exists(select 1 from orgs where id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (p *Platform) DeleteOrg(ctx context.Context, id uuid.UUID) (string, error) {
	var slug string
	err := p.Pool.QueryRow(ctx, `delete from orgs where id = $1 returning slug`, id).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return slug, err
}

func (p *Platform) OrgSlug(ctx context.Context, id uuid.UUID) (string, error) {
	var slug string
	err := p.Pool.QueryRow(ctx, `select slug from orgs where id = $1`, id).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return slug, err
}

// --- Admin list ---

type AdminEntry struct {
	UserID        uuid.UUID  `json:"userId"`
	Email         string     `json:"email"`
	DisplayName   string     `json:"displayName"`
	AddedBy       *uuid.UUID `json:"addedBy,omitempty"`
	AddedByEmail  string     `json:"addedByEmail"`
	AddedAt       time.Time  `json:"addedAt"`
}

func (p *Platform) ListAdmins(ctx context.Context) ([]AdminEntry, error) {
	const q = `
		select pa.user_id, u.email, u.display_name, pa.added_by, coalesce(au.email,''), pa.added_at
		from platform_admins pa
		join users u on u.id = pa.user_id
		left join users au on au.id = pa.added_by
		where pa.revoked_at is null
		order by pa.added_at asc`
	rows, err := p.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminEntry{}
	for rows.Next() {
		var a AdminEntry
		if err := rows.Scan(&a.UserID, &a.Email, &a.DisplayName, &a.AddedBy, &a.AddedByEmail, &a.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- Cross-tenant activity feed ---

// ActivityCrossOrg lists invocations across all tenants. Identical to
// Activity.List but without the org_id filter; used only by the platform-admin
// activity feed.
func (a *Activity) ActivityCrossOrg(ctx context.Context, f ActivityFilter) ([]ActivityRow, string, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	args := []any{}
	clauses := []string{}
	if !f.From.IsZero() {
		args = append(args, f.From)
		clauses = append(clauses, fmt.Sprintf("i.at >= $%d", len(args)))
	}
	if !f.To.IsZero() {
		args = append(args, f.To)
		clauses = append(clauses, fmt.Sprintf("i.at < $%d", len(args)))
	}
	if f.Cursor != "" {
		curAt, curID, err := DecodeCursor(f.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		args = append(args, curAt, curID)
		clauses = append(clauses, fmt.Sprintf("(i.at, i.id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	args = append(args, limit+1)
	whereSQL := ""
	if len(clauses) > 0 {
		whereSQL = "where " + strings.Join(clauses, " and ")
	}
	q := `
		select i.id, i.at, s.id, s.name, i.capability_kind, i.capability_name,
		       i.status, i.latency_ms, i.caller
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		` + whereSQL + `
		order by i.at desc, i.id desc
		limit $` + fmt.Sprintf("%d", len(args))
	rows, err := a.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]ActivityRow, 0, limit)
	for rows.Next() {
		var r ActivityRow
		if err := rows.Scan(&r.ID, &r.At, &r.MCPServer.ID, &r.MCPServer.Name,
			&r.CapabilityKind, &r.CapabilityName, &r.Status, &r.LatencyMs, &r.Caller); err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		last := out[limit-1]
		next = EncodeCursor(last.At, last.ID)
		out = out[:limit]
	}
	return out, next, nil
}
