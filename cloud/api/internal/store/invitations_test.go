package store

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/db"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

type invitationsTestSetup struct {
	invites *Invitations
	orgs    *Orgs
	users   *Users
	orgID   uuid.UUID
	owner   uuid.UUID
}

func newInvitationsTestSetup(t *testing.T) *invitationsTestSetup {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	for _, tbl := range []string{"org_invitations", "org_members", "orgs", "users"} {
		if _, err := pool.Exec(ctx, "truncate "+tbl+" restart identity cascade"); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}

	var ownerID uuid.UUID
	err = pool.QueryRow(ctx, `
		insert into users (email, google_subject) values ($1, $2) returning id`,
		"owner@example.com", "sub-"+uuid.NewString(),
	).Scan(&ownerID)
	if err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	var orgID uuid.UUID
	err = pool.QueryRow(ctx, `
		insert into orgs (name, slug, created_by) values ($1, $2, $3) returning id`,
		"Acme", "acme-"+uuid.NewString()[:8], ownerID,
	).Scan(&orgID)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into org_members (org_id, user_id, role) values ($1, $2, 'owner')`, orgID, ownerID); err != nil {
		t.Fatalf("insert membership: %v", err)
	}

	return &invitationsTestSetup{
		invites: &Invitations{Pool: pool},
		orgs:    &Orgs{Pool: pool},
		users:   &Users{Pool: pool},
		orgID:   orgID,
		owner:   ownerID,
	}
}

func TestInvitations_CreateGetListLifecycle(t *testing.T) {
	s := newInvitationsTestSetup(t)
	ctx := context.Background()

	inv, err := s.invites.Create(ctx, s.orgID, s.owner, "Invitee@Example.com", models.RoleMember)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if inv.OrgName != "Acme" || inv.Status != "pending" {
		t.Fatalf("created invite: %+v", inv)
	}
	if inv.InviterEmail != "owner@example.com" {
		t.Errorf("inviter email = %q", inv.InviterEmail)
	}

	// Duplicate pending → ErrAlreadyExists (partial unique index on lower(email)).
	if _, err := s.invites.Create(ctx, s.orgID, s.owner, "invitee@example.com", models.RoleMember); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	got, err := s.invites.Get(ctx, inv.ID)
	if err != nil || got.ID != inv.ID {
		t.Fatalf("Get: %v, %+v", err, got)
	}

	pending, err := s.invites.ListPendingForEmail(ctx, "INVITEE@example.com")
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPendingForEmail: err=%v len=%d", err, len(pending))
	}

	list, err := s.invites.ListForOrg(ctx, s.orgID, "pending")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListForOrg pending: err=%v len=%d", err, len(list))
	}

	if err := s.invites.MarkAccepted(ctx, inv.ID); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	// Second transition is a no-op (only pending rows match).
	if err := s.invites.MarkAccepted(ctx, inv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-accept: expected ErrNotFound, got %v", err)
	}

	all, err := s.invites.ListForOrg(ctx, s.orgID, "")
	if err != nil || len(all) != 1 || all[0].Status != "accepted" {
		t.Fatalf("ListForOrg: %v %+v", err, all)
	}
}

func TestInvitations_RevokeAndDecline(t *testing.T) {
	s := newInvitationsTestSetup(t)
	ctx := context.Background()

	inv1, err := s.invites.Create(ctx, s.orgID, s.owner, "rev@example.com", models.RoleMember)
	if err != nil {
		t.Fatalf("Create rev: %v", err)
	}
	if err := s.invites.Revoke(ctx, s.orgID, inv1.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := s.invites.Revoke(ctx, s.orgID, inv1.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-revoke: %v", err)
	}

	inv2, err := s.invites.Create(ctx, s.orgID, s.owner, "decline@example.com", models.RoleMember)
	if err != nil {
		t.Fatalf("Create decline: %v", err)
	}
	if err := s.invites.MarkDeclined(ctx, inv2.ID); err != nil {
		t.Fatalf("MarkDeclined: %v", err)
	}
}

func TestOrgs_RoleForAndAddMember(t *testing.T) {
	s := newInvitationsTestSetup(t)
	ctx := context.Background()

	role, err := s.orgs.RoleFor(ctx, s.owner, s.orgID)
	if err != nil || role != models.RoleOwner {
		t.Fatalf("RoleFor owner: %v %q", err, role)
	}

	var otherID uuid.UUID
	if err := s.invites.Pool.QueryRow(ctx, `insert into users (email, google_subject) values ($1, $2) returning id`,
		"other@example.com", "sub-"+uuid.NewString()).Scan(&otherID); err != nil {
		t.Fatalf("insert other: %v", err)
	}
	if _, err := s.orgs.RoleFor(ctx, otherID, s.orgID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RoleFor non-member: %v", err)
	}
	if err := s.orgs.AddMember(ctx, s.orgID, otherID, models.RoleAdmin); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	r, err := s.orgs.RoleFor(ctx, otherID, s.orgID)
	if err != nil || r != models.RoleAdmin {
		t.Fatalf("RoleFor after add: %v %q", err, r)
	}
	// Idempotent — no error, role unchanged.
	if err := s.orgs.AddMember(ctx, s.orgID, otherID, models.RoleMember); err != nil {
		t.Fatalf("AddMember idempotent: %v", err)
	}
	r, _ = s.orgs.RoleFor(ctx, otherID, s.orgID)
	if r != models.RoleAdmin {
		t.Errorf("role should stay admin, got %q", r)
	}
}

func TestUsers_ByEmail(t *testing.T) {
	s := newInvitationsTestSetup(t)
	ctx := context.Background()

	u, err := s.users.ByEmail(ctx, "OWNER@example.com")
	if err != nil || u.ID != s.owner {
		t.Fatalf("ByEmail: %v %+v", err, u)
	}
	if _, err := s.users.ByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ByEmail missing: %v", err)
	}
}
