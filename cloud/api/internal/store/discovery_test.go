package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/db"
)

// discoveryTestSetup spins up a fresh schema in TEST_DATABASE_URL with a user,
// org, gateway, mcp server, and one capability ready to toggle. Skips when no
// test DB is configured.
type discoveryTestSetup struct {
	discovery *Discovery
	orgID     uuid.UUID
	userID    uuid.UUID
	gatewayID uuid.UUID
	serverID  uuid.UUID
	capID     uuid.UUID
}

func newDiscoveryTestSetup(t *testing.T) *discoveryTestSetup {
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
	// Truncate after migration so the new enabled column exists.
	tables := []string{
		"mcp_invocations",
		"mcp_capabilities",
		"mcp_servers",
		"gateway_enrollment_tokens",
		"gateway_credentials",
		"gateways",
		"org_members",
		"orgs",
		"users",
	}
	for _, tbl := range tables {
		if _, err := pool.Exec(ctx, "truncate "+tbl+" restart identity cascade"); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}

	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `insert into users (email, google_subject) values ($1, $2) returning id`,
		"test+"+uuid.NewString()+"@example.com", "sub-"+uuid.NewString()).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var orgID uuid.UUID
	if err := pool.QueryRow(ctx, `insert into orgs (name, slug, created_by) values ($1, $2, $3) returning id`,
		"Test Org", "test-"+uuid.NewString(), userID).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	var gatewayID uuid.UUID
	if err := pool.QueryRow(ctx, `insert into gateways (org_id, name, created_by) values ($1, $2, $3) returning id`,
		orgID, "gw-"+uuid.NewString(), userID).Scan(&gatewayID); err != nil {
		t.Fatalf("insert gateway: %v", err)
	}

	d := &Discovery{Pool: pool}
	srv, err := d.UpsertMCPServer(ctx, orgID, gatewayID, "github-mcp", "http://x", "http", "1", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("UpsertMCPServer: %v", err)
	}
	cap, err := d.UpsertCapability(ctx, srv.ID, "tool", "create_issue", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("UpsertCapability: %v", err)
	}

	return &discoveryTestSetup{
		discovery: d,
		orgID:     orgID,
		userID:    userID,
		gatewayID: gatewayID,
		serverID:  srv.ID,
		capID:     cap.ID,
	}
}

func TestSetCapabilityEnabled_DisablesAndReEnables(t *testing.T) {
	s := newDiscoveryTestSetup(t)
	ctx := context.Background()

	// Fresh cap should be enabled by default.
	det, err := s.discovery.GetServerDetail(ctx, s.orgID, s.serverID)
	if err != nil {
		t.Fatalf("GetServerDetail: %v", err)
	}
	if len(det.Capabilities) != 1 || !det.Capabilities[0].Enabled {
		t.Fatalf("expected one enabled cap, got %+v", det.Capabilities)
	}

	// Disable.
	if err := s.discovery.SetCapabilityEnabled(ctx, s.capID, false, s.userID); err != nil {
		t.Fatalf("SetCapabilityEnabled(false): %v", err)
	}
	det, _ = s.discovery.GetServerDetail(ctx, s.orgID, s.serverID)
	c := det.Capabilities[0]
	if c.Enabled {
		t.Fatalf("expected disabled, got enabled")
	}
	if c.DisabledAt == nil || c.DisabledBy == nil || *c.DisabledBy != s.userID {
		t.Fatalf("expected disabled_at + disabled_by stamped, got at=%v by=%v", c.DisabledAt, c.DisabledBy)
	}

	// Re-enable clears stamps.
	if err := s.discovery.SetCapabilityEnabled(ctx, s.capID, true, s.userID); err != nil {
		t.Fatalf("SetCapabilityEnabled(true): %v", err)
	}
	det, _ = s.discovery.GetServerDetail(ctx, s.orgID, s.serverID)
	c = det.Capabilities[0]
	if !c.Enabled {
		t.Fatalf("expected enabled after re-enable")
	}
	if c.DisabledAt != nil || c.DisabledBy != nil {
		t.Fatalf("expected stamps cleared, got at=%v by=%v", c.DisabledAt, c.DisabledBy)
	}
}

func TestSetCapabilityEnabled_Idempotent(t *testing.T) {
	s := newDiscoveryTestSetup(t)
	ctx := context.Background()

	// Disable twice in a row — second call must succeed and stamp again.
	if err := s.discovery.SetCapabilityEnabled(ctx, s.capID, false, s.userID); err != nil {
		t.Fatalf("first disable: %v", err)
	}
	if err := s.discovery.SetCapabilityEnabled(ctx, s.capID, false, s.userID); err != nil {
		t.Fatalf("second disable: %v", err)
	}
	// Enable twice.
	if err := s.discovery.SetCapabilityEnabled(ctx, s.capID, true, s.userID); err != nil {
		t.Fatalf("first enable: %v", err)
	}
	if err := s.discovery.SetCapabilityEnabled(ctx, s.capID, true, s.userID); err != nil {
		t.Fatalf("second enable: %v", err)
	}
}

func TestSetCapabilityEnabled_UnknownID(t *testing.T) {
	s := newDiscoveryTestSetup(t)
	ctx := context.Background()
	if err := s.discovery.SetCapabilityEnabled(ctx, uuid.New(), false, s.userID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCapabilityBelongsToOrgServer_Membership(t *testing.T) {
	s := newDiscoveryTestSetup(t)
	ctx := context.Background()

	// Happy path.
	ok, err := s.discovery.CapabilityBelongsToOrgServer(ctx, s.orgID, s.serverID, s.capID)
	if err != nil {
		t.Fatalf("CapabilityBelongsToOrgServer: %v", err)
	}
	if !ok {
		t.Fatalf("expected cap to belong to org+server")
	}

	// Wrong org.
	ok, err = s.discovery.CapabilityBelongsToOrgServer(ctx, uuid.New(), s.serverID, s.capID)
	if err != nil {
		t.Fatalf("wrong-org err: %v", err)
	}
	if ok {
		t.Fatalf("expected wrong-org to return false")
	}

	// Wrong server.
	ok, err = s.discovery.CapabilityBelongsToOrgServer(ctx, s.orgID, uuid.New(), s.capID)
	if err != nil {
		t.Fatalf("wrong-server err: %v", err)
	}
	if ok {
		t.Fatalf("expected wrong-server to return false")
	}

	// Unknown cap.
	ok, err = s.discovery.CapabilityBelongsToOrgServer(ctx, s.orgID, s.serverID, uuid.New())
	if err != nil {
		t.Fatalf("unknown-cap err: %v", err)
	}
	if ok {
		t.Fatalf("expected unknown cap to return false")
	}
}

func TestListDisabledCapabilitiesForGateway(t *testing.T) {
	s := newDiscoveryTestSetup(t)
	ctx := context.Background()

	// Initially no disabled caps.
	got, err := s.discovery.ListDisabledCapabilitiesForGateway(ctx, s.gatewayID)
	if err != nil {
		t.Fatalf("ListDisabledCapabilitiesForGateway: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty initially, got %+v", got)
	}

	// Disable and verify the ref tuple appears.
	if err := s.discovery.SetCapabilityEnabled(ctx, s.capID, false, s.userID); err != nil {
		t.Fatalf("SetCapabilityEnabled: %v", err)
	}
	got, err = s.discovery.ListDisabledCapabilitiesForGateway(ctx, s.gatewayID)
	if err != nil {
		t.Fatalf("ListDisabledCapabilitiesForGateway: %v", err)
	}
	if len(got) != 1 || got[0].ServerName != "github-mcp" || got[0].Kind != "tool" || got[0].Name != "create_issue" {
		t.Fatalf("unexpected denylist: %+v", got)
	}

	// Different gateway sees nothing.
	got, err = s.discovery.ListDisabledCapabilitiesForGateway(ctx, uuid.New())
	if err != nil {
		t.Fatalf("other-gateway err: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty for other gateway, got %+v", got)
	}
}
