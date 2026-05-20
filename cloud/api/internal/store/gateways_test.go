package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/db"
)

// gatewaysTestSetup spins up a fresh schema in TEST_DATABASE_URL and returns a
// stores struct plus an org/user fixture. Skips when no test DB is configured.
type gatewaysTestSetup struct {
	gateways *Gateways
	orgID    uuid.UUID
	userID   uuid.UUID
}

func newGatewaysTestSetup(t *testing.T) *gatewaysTestSetup {
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

	// Reset relevant tables so tests are independent.
	tables := []string{
		"gateway_enrollment_tokens",
		"gateway_credentials",
		"gateways",
		"org_members",
		"orgs",
		"users",
	}
	for _, tbl := range tables {
		if _, err := pool.Exec(ctx, "truncate "+tbl+" restart identity cascade"); err != nil {
			// Tables may not exist on a fresh DB; migrate then retry once.
			if err := db.Migrate(ctx, pool); err != nil {
				t.Fatalf("db.Migrate: %v", err)
			}
			if _, err := pool.Exec(ctx, "truncate "+tbl+" restart identity cascade"); err != nil {
				t.Fatalf("truncate %s: %v", tbl, err)
			}
		}
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}

	var userID uuid.UUID
	err = pool.QueryRow(ctx, `
		insert into users (email, google_subject) values ($1, $2) returning id`,
		"test+"+uuid.NewString()+"@example.com", "sub-"+uuid.NewString(),
	).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var orgID uuid.UUID
	err = pool.QueryRow(ctx, `
		insert into orgs (name, slug, created_by) values ($1, $2, $3) returning id`,
		"Test Org", "test-"+uuid.NewString(), userID,
	).Scan(&orgID)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}

	return &gatewaysTestSetup{
		gateways: &Gateways{Pool: pool, Secret: []byte("test-secret-for-hmac-32-bytes-xx")},
		orgID:    orgID,
		userID:   userID,
	}
}

func TestClaimEnrollment_ReclaimAfterFailedPersist(t *testing.T) {
	s := newGatewaysTestSetup(t)
	ctx := context.Background()

	gw, err := s.gateways.Create(ctx, s.orgID, s.userID, "gw-reclaim", "")
	if err != nil {
		t.Fatalf("Create gateway: %v", err)
	}
	tok, err := s.gateways.IssueEnrollmentToken(ctx, gw, s.userID, time.Hour)
	if err != nil {
		t.Fatalf("IssueEnrollmentToken: %v", err)
	}

	// First claim mints credential but leaves claimed_at null (commit_pending).
	_, cred1, err := s.gateways.ClaimEnrollmentToken(ctx, tok)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	// Simulate shim failing to persist: gateway never heartbeats. Second claim
	// must revoke cred1 and mint cred2.
	_, cred2, err := s.gateways.ClaimEnrollmentToken(ctx, tok)
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if cred1 == cred2 {
		t.Fatal("re-claim returned same credential as first claim")
	}
	// cred1 should no longer authenticate.
	if _, err := s.gateways.AuthenticateCredential(ctx, cred1); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("cred1 still valid: err=%v", err)
	}
	// cred2 must authenticate.
	if _, err := s.gateways.AuthenticateCredential(ctx, cred2); err != nil {
		t.Fatalf("cred2 invalid: %v", err)
	}
}

func TestClaimEnrollment_NoReclaimAfterHeartbeat(t *testing.T) {
	s := newGatewaysTestSetup(t)
	ctx := context.Background()

	gw, err := s.gateways.Create(ctx, s.orgID, s.userID, "gw-heartbeat", "")
	if err != nil {
		t.Fatalf("Create gateway: %v", err)
	}
	tok, err := s.gateways.IssueEnrollmentToken(ctx, gw, s.userID, time.Hour)
	if err != nil {
		t.Fatalf("IssueEnrollmentToken: %v", err)
	}

	_, _, err = s.gateways.ClaimEnrollmentToken(ctx, tok)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	// Gateway heartbeats: TouchSeen + CommitEnrollmentOnHeartbeat.
	if err := s.gateways.TouchSeen(ctx, gw.ID); err != nil {
		t.Fatalf("TouchSeen: %v", err)
	}
	if err := s.gateways.CommitEnrollmentOnHeartbeat(ctx, gw.ID); err != nil {
		t.Fatalf("CommitEnrollmentOnHeartbeat: %v", err)
	}

	// Now the token is finalized — a second claim must fail.
	if _, _, err := s.gateways.ClaimEnrollmentToken(ctx, tok); !errors.Is(err, ErrTokenAlreadyUsed) {
		t.Fatalf("second claim should be ErrTokenAlreadyUsed, got %v", err)
	}
}

func TestCommitEnrollmentOnHeartbeat_Idempotent(t *testing.T) {
	s := newGatewaysTestSetup(t)
	ctx := context.Background()

	gw, err := s.gateways.Create(ctx, s.orgID, s.userID, "gw-idem", "")
	if err != nil {
		t.Fatalf("Create gateway: %v", err)
	}
	tok, err := s.gateways.IssueEnrollmentToken(ctx, gw, s.userID, time.Hour)
	if err != nil {
		t.Fatalf("IssueEnrollmentToken: %v", err)
	}
	if _, _, err := s.gateways.ClaimEnrollmentToken(ctx, tok); err != nil {
		t.Fatalf("claim: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := s.gateways.CommitEnrollmentOnHeartbeat(ctx, gw.ID); err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}

	// Even on a gateway that has no pending token, calling is a no-op.
	otherGW, err := s.gateways.Create(ctx, s.orgID, s.userID, "gw-other", "")
	if err != nil {
		t.Fatalf("Create other gateway: %v", err)
	}
	if err := s.gateways.CommitEnrollmentOnHeartbeat(ctx, otherGW.ID); err != nil {
		t.Fatalf("commit on no-pending gateway: %v", err)
	}
}
