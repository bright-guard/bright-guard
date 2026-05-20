package store

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/db"
	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
)

func TestJoinHelper(t *testing.T) {
	if got := join(nil, ","); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := join([]string{"a"}, ","); got != "a" {
		t.Errorf("single = %q", got)
	}
	if got := join([]string{"a", "b", "c"}, ", "); got != "a, b, c" {
		t.Errorf("multi = %q", got)
	}
}

// ---- Integration tests below; gated on TEST_DATABASE_URL ----

type policiesTestSetup struct {
	policies  *Policies
	discovery *Discovery
	pool      interface {
		Close()
	}
	orgID    uuid.UUID
	userID   uuid.UUID
	serverID uuid.UUID
}

func newPoliciesTestSetup(t *testing.T) *policiesTestSetup {
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
		"mcp_invocation_decisions", "policy_sweep_state", "policies",
		"mcp_invocations", "mcp_capabilities", "mcp_servers",
		"gateway_enrollment_tokens", "gateway_credentials", "gateways",
		"org_members", "orgs", "users",
	}
	for _, tbl := range tables {
		// Some tables may not exist on older migrations; ignore those errors.
		_, _ = pool.Exec(ctx, "truncate "+tbl+" restart identity cascade")
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
	return &policiesTestSetup{
		policies:  &Policies{Pool: pool},
		discovery: d,
		pool:      pool,
		orgID:     orgID,
		userID:    userID,
		serverID:  srv.ID,
	}
}

func TestPolicies_CreateGetListUpdateDelete(t *testing.T) {
	s := newPoliciesTestSetup(t)
	ctx := context.Background()

	created, err := s.policies.Create(ctx, PolicyCreate{
		OrgID:       s.orgID,
		CreatedBy:   s.userID,
		Name:        "block create_issue",
		Description: "audit-mode block",
		Expression:  `capability.name == "create_issue"`,
		Action:      models.PolicyActionDeny,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("Create returned nil id")
	}

	got, err := s.policies.Get(ctx, s.orgID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != created.Name {
		t.Errorf("Get.Name = %q", got.Name)
	}

	list, err := s.policies.List(ctx, s.orgID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	newName := "renamed"
	newAction := models.PolicyActionWarn
	updated, err := s.policies.Update(ctx, s.orgID, created.ID, PolicyPatch{
		Name:   &newName,
		Action: &newAction,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != newName || updated.Action != newAction {
		t.Errorf("Update result: %+v", updated)
	}

	if err := s.policies.Delete(ctx, s.orgID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.policies.Get(ctx, s.orgID, created.ID); err == nil {
		t.Fatal("Get after Delete should fail")
	}
}

func TestPolicies_RecordDecisionsAndReplay(t *testing.T) {
	s := newPoliciesTestSetup(t)
	ctx := context.Background()
	pol, err := s.policies.Create(ctx, PolicyCreate{
		OrgID:      s.orgID,
		CreatedBy:  s.userID,
		Name:       "p",
		Expression: `capability.name == "create_issue"`,
		Action:     models.PolicyActionDeny,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Two real invocations.
	at := time.Now().UTC()
	if err := s.discovery.InsertInvocation(ctx, s.orgID, s.serverID, "tool", "create_issue",
		json.RawMessage(`{}`), "ok", 12, at); err != nil {
		t.Fatalf("RecordInvocation: %v", err)
	}
	if err := s.discovery.InsertInvocation(ctx, s.orgID, s.serverID, "tool", "create_issue",
		json.RawMessage(`{}`), "ok", 12, at.Add(time.Second)); err != nil {
		t.Fatalf("RecordInvocation 2: %v", err)
	}
	// Fetch their ids from mcp_invocations.
	rows, err := s.discovery.Pool.Query(ctx, `select id from mcp_invocations where org_id = $1 order by at asc`, s.orgID)
	if err != nil {
		t.Fatalf("query invocations: %v", err)
	}
	defer rows.Close()
	var invIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		invIDs = append(invIDs, id)
	}
	if len(invIDs) != 2 {
		t.Fatalf("expected 2 invocations, got %d", len(invIDs))
	}
	decisions := []DecisionRow{
		{InvocationID: invIDs[0], PolicyID: pol.ID, Matched: true, Action: models.PolicyActionDeny},
		{InvocationID: invIDs[1], PolicyID: pol.ID, Matched: true, Action: models.PolicyActionDeny},
	}
	if err := s.policies.RecordDecisions(ctx, decisions); err != nil {
		t.Fatalf("RecordDecisions: %v", err)
	}
	// Replay (idempotent upsert) — should not double-write.
	if err := s.policies.RecordDecisions(ctx, decisions); err != nil {
		t.Fatalf("RecordDecisions replay: %v", err)
	}
	out, err := s.policies.DecisionsForInvocations(ctx, s.orgID, invIDs)
	if err != nil {
		t.Fatalf("DecisionsForInvocations: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 invocations with decisions, got %d", len(out))
	}
	for _, decs := range out {
		if len(decs) != 1 {
			t.Errorf("expected exactly 1 decision per inv, got %d", len(decs))
		}
		if decs[0].PolicyName == "" {
			t.Error("PolicyName should be populated by JOIN")
		}
	}
}

func TestPolicies_SweepWatermarkRoundTrip(t *testing.T) {
	s := newPoliciesTestSetup(t)
	ctx := context.Background()
	// No row yet → epoch fallback.
	w, err := s.policies.SweepWatermark(ctx, s.orgID)
	if err != nil {
		t.Fatalf("SweepWatermark: %v", err)
	}
	if w.Unix() > 0 {
		t.Errorf("watermark before set should be ~epoch, got %v", w)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.policies.SetSweepWatermark(ctx, s.orgID, now); err != nil {
		t.Fatalf("SetSweepWatermark: %v", err)
	}
	got, err := s.policies.SweepWatermark(ctx, s.orgID)
	if err != nil {
		t.Fatalf("SweepWatermark 2: %v", err)
	}
	if !got.Equal(now) {
		t.Errorf("watermark = %v, want %v", got, now)
	}
	// Going backward must not regress.
	earlier := now.Add(-time.Hour)
	if err := s.policies.SetSweepWatermark(ctx, s.orgID, earlier); err != nil {
		t.Fatalf("SetSweepWatermark earlier: %v", err)
	}
	got, _ = s.policies.SweepWatermark(ctx, s.orgID)
	if !got.Equal(now) {
		t.Errorf("watermark should not regress, got %v", got)
	}
}

func TestPolicies_BundleFor_VersionAndContents(t *testing.T) {
	s := newPoliciesTestSetup(t)
	ctx := context.Background()

	// Pristine org with no policies: version 0, empty slice.
	v0, list, err := s.policies.BundleFor(ctx, s.orgID)
	if err != nil {
		t.Fatalf("BundleFor empty: %v", err)
	}
	if v0 != 0 {
		t.Errorf("expected initial version 0, got %d", v0)
	}
	if len(list) != 0 {
		t.Errorf("expected no policies, got %d", len(list))
	}

	// Create two policies — trigger should bump version twice.
	for i, name := range []string{"p-a", "p-b"} {
		if _, err := s.policies.Create(ctx, PolicyCreate{
			OrgID: s.orgID, CreatedBy: s.userID, Name: name,
			Expression: `status == "ok"`, Action: models.PolicyActionDeny,
			Enabled: i == 0,
		}); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	v2, list2, err := s.policies.BundleFor(ctx, s.orgID)
	if err != nil {
		t.Fatalf("BundleFor after create: %v", err)
	}
	if v2 != 2 {
		t.Errorf("expected version 2 after two creates, got %d", v2)
	}
	// Only enabled policies make it into the bundle.
	if len(list2) != 1 {
		t.Errorf("expected 1 enabled policy, got %d", len(list2))
	}
	if list2[0].Name != "p-a" {
		t.Errorf("expected p-a in bundle, got %q", list2[0].Name)
	}
}

func TestPolicies_BundleVersionBumpsOnCRUD(t *testing.T) {
	s := newPoliciesTestSetup(t)
	ctx := context.Background()

	readVer := func() int64 {
		v, _, err := s.policies.BundleFor(ctx, s.orgID)
		if err != nil {
			t.Fatalf("BundleFor: %v", err)
		}
		return v
	}

	if v := readVer(); v != 0 {
		t.Fatalf("initial version = %d, want 0", v)
	}

	// Insert bumps.
	pol, err := s.policies.Create(ctx, PolicyCreate{
		OrgID: s.orgID, CreatedBy: s.userID, Name: "v-test",
		Expression: `status == "ok"`, Action: models.PolicyActionDeny, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if v := readVer(); v != 1 {
		t.Errorf("after insert: version = %d, want 1", v)
	}

	// Update bumps.
	disabled := false
	if _, err := s.policies.Update(ctx, s.orgID, pol.ID, PolicyPatch{Enabled: &disabled}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v := readVer(); v != 2 {
		t.Errorf("after update: version = %d, want 2", v)
	}

	// Delete bumps.
	if err := s.policies.Delete(ctx, s.orgID, pol.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if v := readVer(); v != 3 {
		t.Errorf("after delete: version = %d, want 3", v)
	}
}

func TestInsertInvocation_WithDecisionsWritesBothInOneTx(t *testing.T) {
	s := newPoliciesTestSetup(t)
	ctx := context.Background()

	pol, err := s.policies.Create(ctx, PolicyCreate{
		OrgID: s.orgID, CreatedBy: s.userID, Name: "p",
		Expression: `capability.name == "create_issue"`,
		Action:     models.PolicyActionDeny, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	at := time.Now().UTC()
	if err := s.discovery.InsertInvocation(ctx,
		s.orgID, s.serverID, "tool", "create_issue",
		json.RawMessage(`{}`), "denied", 12, at,
		WithDecisions([]InvocationDecision{
			{PolicyID: pol.ID, Action: models.PolicyActionDeny, Matched: true},
		}),
	); err != nil {
		t.Fatalf("InsertInvocation with decisions: %v", err)
	}

	// Decision row should be present, joined back via DecisionsForInvocations.
	var invID uuid.UUID
	if err := s.discovery.Pool.QueryRow(ctx,
		`select id from mcp_invocations where org_id = $1`, s.orgID).Scan(&invID); err != nil {
		t.Fatalf("query inv id: %v", err)
	}
	got, err := s.policies.DecisionsForInvocations(ctx, s.orgID, []uuid.UUID{invID})
	if err != nil {
		t.Fatalf("DecisionsForInvocations: %v", err)
	}
	if len(got[invID]) != 1 || got[invID][0].PolicyID != pol.ID {
		t.Fatalf("expected one decision for the new invocation, got %+v", got)
	}

	// Sweep loader must skip rows already having decisions.
	invs, err := s.policies.ListInvocationsForSweep(ctx, s.orgID, time.Unix(0, 0).UTC(), 100)
	if err != nil {
		t.Fatalf("ListInvocationsForSweep: %v", err)
	}
	for _, inv := range invs {
		if inv.ID == invID {
			t.Errorf("sweep should skip invocations that already have decisions")
		}
	}
}

func TestPolicies_DuplicateNameRejected(t *testing.T) {
	s := newPoliciesTestSetup(t)
	ctx := context.Background()
	_, err := s.policies.Create(ctx, PolicyCreate{
		OrgID: s.orgID, CreatedBy: s.userID, Name: "dup",
		Expression: `true`, Action: models.PolicyActionDeny, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	_, err = s.policies.Create(ctx, PolicyCreate{
		OrgID: s.orgID, CreatedBy: s.userID, Name: "dup",
		Expression: `false`, Action: models.PolicyActionWarn, Enabled: true,
	})
	if err == nil {
		t.Fatal("Create #2: expected unique violation")
	}
	if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "23505") {
		t.Errorf("error doesn't look like a unique violation: %v", err)
	}
}
