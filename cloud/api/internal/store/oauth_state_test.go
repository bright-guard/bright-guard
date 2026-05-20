package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/db"
	"github.com/bright-guard/bright-guard/cloud/api/internal/mcp"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

// oauthStateTestSetup is a minimal Postgres harness for the OAuth-state ops.
// Mirrors gatewaysTestSetup; skips when no test DB is configured.
type oauthStateTestSetup struct {
	conns  *Connections
	orgID  uuid.UUID
	userID uuid.UUID
	connID uuid.UUID
}

func newOAuthStateTestSetup(t *testing.T) *oauthStateTestSetup {
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

	tables := []string{
		"oauth_authcode_states",
		"mcp_capabilities",
		"mcp_servers",
		"mcp_connections",
		"org_members",
		"orgs",
		"users",
	}
	for _, tbl := range tables {
		if _, err := pool.Exec(ctx, "truncate "+tbl+" restart identity cascade"); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}

	aead, err := NewAEAD([]byte("test-secret-for-aead-32-bytes-xxx"))
	if err != nil {
		t.Fatalf("NewAEAD: %v", err)
	}
	conns := &Connections{Pool: pool, AEAD: aead}

	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `
		insert into users (email, google_subject) values ($1, $2) returning id`,
		"oauth-test+"+uuid.NewString()+"@example.com", "sub-"+uuid.NewString(),
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var orgID uuid.UUID
	if err := pool.QueryRow(ctx, `
		insert into orgs (name, slug, created_by) values ($1, $2, $3) returning id`,
		"Test Org", "test-"+uuid.NewString(), userID,
	).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	conn, err := conns.CreateWithOpts(ctx, CreateOpts{
		OrgID:     orgID,
		CreatedBy: userID,
		Name:      "test-conn",
		Endpoint:  "https://mcp.example.com/v1",
		Transport: "streamable-http",
		Method:    models.AuthMethodOAuth2Authcode,
		Secret: mcp.AuthSecret{
			Method:       string(models.AuthMethodOAuth2Authcode),
			ClientID:     "client-id",
			AuthorizeURL: "https://idp.example.com/authorize",
			TokenURL:     "https://idp.example.com/token",
		},
		OAuthStatus: models.OAuthStatusPendingAuthorize,
	})
	if err != nil {
		t.Fatalf("CreateWithOpts: %v", err)
	}

	return &oauthStateTestSetup{
		conns:  conns,
		orgID:  orgID,
		userID: userID,
		connID: conn.ID,
	}
}

func TestPutAndTakeOAuthState(t *testing.T) {
	setup := newOAuthStateTestSetup(t)
	ctx := context.Background()

	in := OAuthState{
		State:        "state-token-aaa",
		ConnectionID: setup.connID,
		OrgID:        setup.orgID,
		UserID:       setup.userID,
		CodeVerifier: "verifier-secret",
		ReturnTo:     "/app/mcp-connections",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}
	if err := setup.conns.PutOAuthState(ctx, in); err != nil {
		t.Fatalf("PutOAuthState: %v", err)
	}

	out, err := setup.conns.TakeOAuthState(ctx, in.State)
	if err != nil {
		t.Fatalf("TakeOAuthState: %v", err)
	}
	if out.CodeVerifier != in.CodeVerifier {
		t.Errorf("CodeVerifier = %q, want %q", out.CodeVerifier, in.CodeVerifier)
	}
	if out.ConnectionID != in.ConnectionID {
		t.Errorf("ConnectionID mismatch")
	}

	// Taking the same state again must fail — the row is deleted on take.
	if _, err := setup.conns.TakeOAuthState(ctx, in.State); !errors.Is(err, ErrNotFound) {
		t.Errorf("second take err = %v, want ErrNotFound", err)
	}
}

func TestSweepExpiredOAuthStates(t *testing.T) {
	setup := newOAuthStateTestSetup(t)
	ctx := context.Background()

	past := time.Now().Add(-5 * time.Minute)
	future := time.Now().Add(5 * time.Minute)
	if err := setup.conns.PutOAuthState(ctx, OAuthState{
		State: "expired-1", ConnectionID: setup.connID, OrgID: setup.orgID, UserID: setup.userID,
		CodeVerifier: "v1", ExpiresAt: past,
	}); err != nil {
		t.Fatalf("PutOAuthState: %v", err)
	}
	if err := setup.conns.PutOAuthState(ctx, OAuthState{
		State: "active-1", ConnectionID: setup.connID, OrgID: setup.orgID, UserID: setup.userID,
		CodeVerifier: "v2", ExpiresAt: future,
	}); err != nil {
		t.Fatalf("PutOAuthState: %v", err)
	}

	n, err := setup.conns.SweepExpiredOAuthStates(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("swept = %d, want 1", n)
	}
	if _, err := setup.conns.TakeOAuthState(ctx, "active-1"); err != nil {
		t.Errorf("active state lost: %v", err)
	}
	if _, err := setup.conns.TakeOAuthState(ctx, "expired-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired state still present: %v", err)
	}
}

func TestUpdateOAuthStatus(t *testing.T) {
	setup := newOAuthStateTestSetup(t)
	ctx := context.Background()

	if err := setup.conns.UpdateOAuthStatus(ctx, setup.connID, models.OAuthStatusAuthorized); err != nil {
		t.Fatalf("UpdateOAuthStatus: %v", err)
	}
	conn, err := setup.conns.Get(ctx, setup.orgID, setup.connID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if conn.OAuthStatus != models.OAuthStatusAuthorized {
		t.Errorf("OAuthStatus = %q, want %q", conn.OAuthStatus, models.OAuthStatusAuthorized)
	}
}
