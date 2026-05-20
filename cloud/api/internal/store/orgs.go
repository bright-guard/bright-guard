package store

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

type Orgs struct {
	Pool *pgxpool.Pool
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "org"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func randSuffix() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

// Create inserts an org and adds the creator as owner. If the slug collides,
// it retries with a random suffix.
func (s *Orgs) Create(ctx context.Context, name string, createdBy uuid.UUID) (*models.Org, error) {
	baseSlug := Slugify(name)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var org models.Org
	for attempt := 0; attempt < 5; attempt++ {
		slug := baseSlug
		if attempt > 0 {
			slug = baseSlug + "-" + randSuffix()
		}
		const q = `
			insert into orgs (name, slug, created_by)
			values ($1, $2, $3)
			on conflict (slug) do nothing
			returning id, name, slug, created_by, created_at`
		err = tx.QueryRow(ctx, q, name, slug, createdBy).Scan(
			&org.ID, &org.Name, &org.Slug, &org.CreatedBy, &org.CreatedAt,
		)
		if err == nil {
			break
		}
		if err != pgx.ErrNoRows {
			return nil, fmt.Errorf("insert org: %w", err)
		}
	}
	if org.ID == uuid.Nil {
		return nil, fmt.Errorf("could not allocate unique slug")
	}

	const memQ = `insert into org_members (org_id, user_id, role) values ($1, $2, 'owner')`
	if _, err := tx.Exec(ctx, memQ, org.ID, createdBy); err != nil {
		return nil, fmt.Errorf("insert membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &org, nil
}

func (s *Orgs) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.Membership, error) {
	const q = `
		select o.id, o.name, o.slug, o.created_by, o.created_at, m.role
		from org_members m
		join orgs o on o.id = m.org_id
		where m.user_id = $1
		order by o.created_at asc`
	rows, err := s.Pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Membership
	for rows.Next() {
		var m models.Membership
		if err := rows.Scan(
			&m.Org.ID, &m.Org.Name, &m.Org.Slug, &m.Org.CreatedBy, &m.Org.CreatedAt, &m.Role,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Orgs) UserHasMembership(ctx context.Context, userID, orgID uuid.UUID) (bool, error) {
	const q = `select 1 from org_members where user_id = $1 and org_id = $2`
	var x int
	err := s.Pool.QueryRow(ctx, q, userID, orgID).Scan(&x)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
