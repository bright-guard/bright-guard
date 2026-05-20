package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/db"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

// patchSetup builds a real Server backed by TEST_DATABASE_URL so we can exercise
// the PATCH .../capabilities/{capId} handler end-to-end, including the membership
// join. Skips when no test DB is configured.
type patchSetup struct {
	server   *Server
	orgID    uuid.UUID
	otherOrg uuid.UUID
	userID   uuid.UUID
	serverID uuid.UUID
	capID    uuid.UUID
}

func newPatchSetup(t *testing.T) *patchSetup {
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
	for _, tbl := range []string{
		"mcp_invocations",
		"mcp_capabilities",
		"mcp_servers",
		"gateway_enrollment_tokens",
		"gateway_credentials",
		"gateways",
		"org_members",
		"orgs",
		"users",
	} {
		if _, err := pool.Exec(ctx, "truncate "+tbl+" restart identity cascade"); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}

	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `insert into users (email, google_subject) values ($1, $2) returning id`,
		"u-"+uuid.NewString()+"@example.com", "sub-"+uuid.NewString()).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var orgID, otherOrg uuid.UUID
	if err := pool.QueryRow(ctx, `insert into orgs (name, slug, created_by) values ($1, $2, $3) returning id`,
		"O1", "o1-"+uuid.NewString(), userID).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into orgs (name, slug, created_by) values ($1, $2, $3) returning id`,
		"O2", "o2-"+uuid.NewString(), userID).Scan(&otherOrg); err != nil {
		t.Fatalf("insert other org: %v", err)
	}
	var gatewayID uuid.UUID
	if err := pool.QueryRow(ctx, `insert into gateways (org_id, name, created_by) values ($1, $2, $3) returning id`,
		orgID, "gw-"+uuid.NewString(), userID).Scan(&gatewayID); err != nil {
		t.Fatalf("insert gateway: %v", err)
	}

	d := &store.Discovery{Pool: pool}
	srv, err := d.UpsertMCPServer(ctx, orgID, gatewayID, "github-mcp", "http://x", "http", "1", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("UpsertMCPServer: %v", err)
	}
	cap, err := d.UpsertCapability(ctx, srv.ID, "tool", "create_issue", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("UpsertCapability: %v", err)
	}

	s := &Server{Discovery: d}
	return &patchSetup{
		server:   s,
		orgID:    orgID,
		otherOrg: otherOrg,
		userID:   userID,
		serverID: srv.ID,
		capID:    cap.ID,
	}
}

// callPatch invokes handleSetCapabilityEnabled with the given URL params and an
// authenticated user, bypassing the chi router (so we can inject context cleanly).
func (p *patchSetup) callPatch(t *testing.T, orgID, serverID, capID uuid.UUID, enabled bool) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]bool{"enabled": enabled})
	r := httptest.NewRequest(http.MethodPatch, "/x", bytes.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxKeyOrgID, orgID)
	ctx = auth.WithUserForTest(ctx, &models.User{ID: p.userID})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("serverId", serverID.String())
	rctx.URLParams.Add("capId", capID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	w := httptest.NewRecorder()
	p.server.handleSetCapabilityEnabled(w, r.WithContext(ctx))
	return w
}

func TestPatchCapability_DisablesAndReEnables(t *testing.T) {
	p := newPatchSetup(t)

	w := p.callPatch(t, p.orgID, p.serverID, p.capID, false)
	if w.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", w.Code, w.Body.String())
	}
	var det models.MCPServerDetail
	if err := json.Unmarshal(w.Body.Bytes(), &det); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(det.Capabilities) != 1 || det.Capabilities[0].Enabled {
		t.Fatalf("expected cap to be disabled, got %+v", det.Capabilities)
	}
	if det.Capabilities[0].DisabledByEmail == "" {
		t.Fatalf("expected disabled_by_email to be populated, got empty")
	}

	w = p.callPatch(t, p.orgID, p.serverID, p.capID, true)
	if w.Code != http.StatusOK {
		t.Fatalf("re-enable status = %d body=%s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &det)
	if !det.Capabilities[0].Enabled {
		t.Fatalf("expected cap to be enabled after toggle, got %+v", det.Capabilities[0])
	}
}

func TestPatchCapability_Idempotent(t *testing.T) {
	p := newPatchSetup(t)
	// Two consecutive disables — both 200, final state still disabled.
	for i := 0; i < 2; i++ {
		w := p.callPatch(t, p.orgID, p.serverID, p.capID, false)
		if w.Code != http.StatusOK {
			t.Fatalf("disable %d status = %d body=%s", i, w.Code, w.Body.String())
		}
	}
}

func TestPatchCapability_RejectsCrossOrg(t *testing.T) {
	p := newPatchSetup(t)
	w := p.callPatch(t, p.otherOrg, p.serverID, p.capID, false)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-org cap, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPatchCapability_RejectsWrongServer(t *testing.T) {
	p := newPatchSetup(t)
	w := p.callPatch(t, p.orgID, uuid.New(), p.capID, false)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong server, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPatchCapability_RejectsUnknownCap(t *testing.T) {
	p := newPatchSetup(t)
	w := p.callPatch(t, p.orgID, p.serverID, uuid.New(), false)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown cap, got %d body=%s", w.Code, w.Body.String())
	}
}
