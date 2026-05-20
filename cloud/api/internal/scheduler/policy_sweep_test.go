package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/policy"
)

func modelsPolicyForTest(orgID uuid.UUID) models.Policy {
	return models.Policy{
		ID:         uuid.New(),
		OrgID:      orgID,
		Name:       "test",
		Expression: `status == "ok"`,
		Action:     models.PolicyActionDeny,
		Enabled:    true,
	}
}

// These tests cover the policy-sweep wiring that's possible without a real DB:
// the engine init path and the bounded-batch defaults. The store + integration
// behavior is covered by the policy CEL tests in internal/policy/ and the
// (DB-gated) policies store tests.

func TestNewPolicySweeper_DefaultsAndEngine(t *testing.T) {
	s := NewPolicySweeper(nil, nil, nil, 0)
	if s.Interval != policySweepInterval {
		t.Errorf("Interval = %v, want %v", s.Interval, policySweepInterval)
	}
	if s.MaxOrgsPerTick != policySweepMaxOrgs {
		t.Errorf("MaxOrgsPerTick = %d", s.MaxOrgsPerTick)
	}
	if s.MaxInvocationsPerOrgTick != policySweepBatchPerOrg {
		t.Errorf("MaxInvocationsPerOrgTick = %d", s.MaxInvocationsPerOrgTick)
	}
	if s.Engine == nil {
		t.Fatal("engine should be auto-constructed when nil is passed")
	}
}

func TestNewPolicySweeper_AcceptsCustomEngine(t *testing.T) {
	e, err := policy.New()
	if err != nil {
		t.Fatalf("policy.New: %v", err)
	}
	s := NewPolicySweeper(nil, nil, e, 5*time.Second)
	if s.Engine != e {
		t.Fatal("custom engine should be retained")
	}
	if s.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", s.Interval)
	}
}

func TestPolicySweeper_NilEngineTickIsSafe(t *testing.T) {
	// If engine construction ever fails (unlikely with a hand-crafted env),
	// tick() must still return without panicking — the sweep is best-effort.
	s := &PolicySweeper{
		Engine:                   nil,
		Policies:                 nil,
		Connections:              nil,
		Interval:                 time.Second,
		MaxOrgsPerTick:           1,
		MaxInvocationsPerOrgTick: 1,
	}
	// Should be a no-op.
	s.tick(context.Background())
}

func TestPolicySweeper_BackfillRunsAgainstNilStoreSafely(t *testing.T) {
	// Defensive: BackfillPolicy is called from a goroutine post-API-handler;
	// a panic there would crash the whole API. Guard against missing deps.
	s := &PolicySweeper{Engine: nil}
	s.BackfillPolicy(context.Background(), modelsPolicyForTest(uuid.New()), time.Hour, 10)
}
