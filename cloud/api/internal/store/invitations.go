package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

// Invitations is the data layer for org_invitations.
type Invitations struct {
	Pool *pgxpool.Pool
}

// ErrAlreadyExists indicates a pending invite already exists for (org, email),
// or the user is already a member. Callers map this to HTTP 422.
var ErrAlreadyExists = errors.New("already exists")

// InvitationTTL is the default lifetime of a pending invite. Short enough to
// avoid stale invites lingering forever; long enough that a teammate has time
// to act over a weekend.
const InvitationTTL = 14 * 24 * time.Hour

// selectInvitation is the column list used by every read. The joined
// org/inviter columns let the SPA render without extra round-trips.
const selectInvitation = `
	select i.id, i.org_id, o.name, o.slug, i.email, i.invited_by,
	       coalesce(u.email, ''), coalesce(u.display_name, ''),
	       i.role, i.status, i.accepted_at, i.declined_at, i.created_at, i.expires_at
	from org_invitations i
	join orgs o on o.id = i.org_id
	left join users u on u.id = i.invited_by`

func scanInvitation(row pgx.Row, inv *models.Invitation) error {
	return row.Scan(
		&inv.ID, &inv.OrgID, &inv.OrgName, &inv.OrgSlug, &inv.Email, &inv.InvitedBy,
		&inv.InviterEmail, &inv.InviterName,
		&inv.Role, &inv.Status, &inv.AcceptedAt, &inv.DeclinedAt, &inv.CreatedAt, &inv.ExpiresAt,
	)
}

// Create inserts a pending invite. Returns ErrAlreadyExists when a pending
// invite already exists for (org, lower(email)) — that conflict is enforced
// by the partial unique index.
func (s *Invitations) Create(ctx context.Context, orgID, invitedBy uuid.UUID, email string, role models.OrgRole) (*models.Invitation, error) {
	email = strings.TrimSpace(email)
	const insertQ = `
		insert into org_invitations (org_id, email, invited_by, role, expires_at)
		values ($1, $2, $3, $4, $5)
		on conflict do nothing
		returning id`
	expires := time.Now().Add(InvitationTTL)
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, insertQ, orgID, email, invitedBy, role, expires).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAlreadyExists
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Get returns the invitation with joined org/inviter columns.
func (s *Invitations) Get(ctx context.Context, id uuid.UUID) (*models.Invitation, error) {
	q := selectInvitation + ` where i.id = $1`
	inv := &models.Invitation{}
	err := scanInvitation(s.Pool.QueryRow(ctx, q, id), inv)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// ListForOrg returns invitations for an org. If statusFilter is non-empty,
// only rows matching it are returned.
func (s *Invitations) ListForOrg(ctx context.Context, orgID uuid.UUID, statusFilter string) ([]models.Invitation, error) {
	args := []any{orgID}
	q := selectInvitation + ` where i.org_id = $1`
	if statusFilter != "" {
		q += ` and i.status = $2`
		args = append(args, statusFilter)
	}
	q += ` order by i.created_at desc`
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Invitation{}
	for rows.Next() {
		var inv models.Invitation
		if err := scanInvitation(rows, &inv); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// ListPendingForEmail returns non-expired pending invites whose email matches
// the caller's email (case-insensitive). Expired invites are filtered out so
// the SPA banner doesn't nag.
func (s *Invitations) ListPendingForEmail(ctx context.Context, email string) ([]models.Invitation, error) {
	q := selectInvitation + ` where i.status = 'pending' and lower(i.email) = lower($1) and i.expires_at > now() order by i.created_at desc`
	rows, err := s.Pool.Query(ctx, q, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Invitation{}
	for rows.Next() {
		var inv models.Invitation
		if err := scanInvitation(rows, &inv); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// Revoke marks a pending invitation revoked. No-op if it isn't pending.
func (s *Invitations) Revoke(ctx context.Context, orgID, id uuid.UUID) error {
	const q = `update org_invitations set status = 'revoked' where id = $1 and org_id = $2 and status = 'pending'`
	tag, err := s.Pool.Exec(ctx, q, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAccepted transitions a pending invite to accepted. The caller is
// responsible for inserting the org_members row in the same logical operation.
func (s *Invitations) MarkAccepted(ctx context.Context, id uuid.UUID) error {
	const q = `update org_invitations set status = 'accepted', accepted_at = now() where id = $1 and status = 'pending'`
	tag, err := s.Pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkDeclined transitions a pending invite to declined.
func (s *Invitations) MarkDeclined(ctx context.Context, id uuid.UUID) error {
	const q = `update org_invitations set status = 'declined', declined_at = now() where id = $1 and status = 'pending'`
	tag, err := s.Pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
