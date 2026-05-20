package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/models"
	"github.com/bright-guard/bright-guard/cloud/api/internal/policy"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	policySweepLockKey    = "bg-policy-sweep"
	policySweepInterval   = 30 * time.Second
	policySweepMaxOrgs    = 50
	policySweepBatchPerOrg = 1000
)

// PolicySweeper periodically evaluates new mcp_invocations against each org's
// enabled policies and records mcp_invocation_decisions. Pure audit — never
// touches the data path. Uses its own advisory-lock key so it runs
// independently of the discovery / caller / exposure sweepers.
type PolicySweeper struct {
	Policies    *store.Policies
	Connections *store.Connections // for advisory lock only
	Engine      *policy.Engine
	Interval    time.Duration

	// MaxOrgsPerTick / MaxInvocationsPerOrgTick make the sweep bounds testable.
	MaxOrgsPerTick           int
	MaxInvocationsPerOrgTick int
}

// NewPolicySweeper builds a sweeper with sensible defaults. If engine is nil
// it tries to construct one — fatal here would be wrong because the sweep is
// best-effort.
func NewPolicySweeper(policies *store.Policies, conns *store.Connections, engine *policy.Engine, interval time.Duration) *PolicySweeper {
	if interval <= 0 {
		interval = policySweepInterval
	}
	if engine == nil {
		// If engine construction fails, log and let the sweep no-op safely.
		e, err := policy.New()
		if err != nil {
			log.Printf("policy sweep: engine init: %v", err)
		}
		engine = e
	}
	return &PolicySweeper{
		Policies:                 policies,
		Connections:              conns,
		Engine:                   engine,
		Interval:                 interval,
		MaxOrgsPerTick:           policySweepMaxOrgs,
		MaxInvocationsPerOrgTick: policySweepBatchPerOrg,
	}
}

func (s *PolicySweeper) Run(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *PolicySweeper) tick(ctx context.Context) {
	if s.Engine == nil || s.Policies == nil {
		return
	}
	ok, err := s.Connections.TryAdvisoryLock(ctx, policySweepLockKey)
	if err != nil {
		log.Printf("policy sweep: advisory lock check failed: %v", err)
		return
	}
	if !ok {
		return
	}
	orgs, err := s.Policies.ListOrgsWithDuePolicies(ctx, s.MaxOrgsPerTick)
	if err != nil {
		log.Printf("policy sweep: list orgs: %v", err)
		return
	}
	for _, orgID := range orgs {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := s.sweepOrg(ctx, orgID); err != nil {
			log.Printf("policy sweep: org %s: %v", orgID, err)
		}
	}
}

// sweepOrg evaluates the next batch of new invocations for `orgID` against
// every enabled policy, records decisions, and advances the watermark.
// Exported (lower-case in tests) so the watermark + compile path can be
// exercised independently of the lock + ticker loop.
func (s *PolicySweeper) sweepOrg(ctx context.Context, orgID uuid.UUID) error {
	policies, err := s.Policies.ListEnabledForOrg(ctx, orgID)
	if err != nil {
		return err
	}
	if len(policies) == 0 {
		return nil
	}
	// Compile once per tick; bail any policy that doesn't compile (we logged
	// the user-facing error at create/update time, so it's surprising here).
	type compiled struct {
		policy  models.Policy
		program *policy.PolicyProgram
	}
	progs := make([]compiled, 0, len(policies))
	for _, p := range policies {
		prg, err := s.Engine.Compile(p.Expression)
		if err != nil {
			log.Printf("policy sweep: compile %s (%s): %v", p.ID, p.Name, err)
			continue
		}
		progs = append(progs, compiled{policy: p, program: prg})
	}
	if len(progs) == 0 {
		return nil
	}

	water, err := s.Policies.SweepWatermark(ctx, orgID)
	if err != nil {
		return err
	}
	invs, err := s.Policies.ListInvocationsForSweep(ctx, orgID, water, s.MaxInvocationsPerOrgTick)
	if err != nil {
		return err
	}
	if len(invs) == 0 {
		return nil
	}

	decisions := make([]store.DecisionRow, 0, len(invs)*len(progs))
	var maxAt time.Time
	for _, inv := range invs {
		if inv.At.After(maxAt) {
			maxAt = inv.At
		}
		ic := policy.InvocationContext{
			At:         inv.At,
			Status:     inv.Status,
			Caller:     inv.Caller,
			Server:     inv.Server,
			Capability: inv.Capability,
			Workload:   inv.Workload,
			Network:    inv.Network,
		}
		for _, c := range progs {
			matched, err := c.program.Evaluate(ctx, ic)
			if err != nil {
				// Treat eval errors (missing keys, cost-limit, etc.) as
				// non-match. The alternative — writing a "matched=false"
				// decision — would inflate the table without benefit; the
				// activity view only surfaces matched=true rows.
				continue
			}
			if !matched {
				continue
			}
			decisions = append(decisions, store.DecisionRow{
				InvocationID: inv.ID,
				PolicyID:     c.policy.ID,
				Matched:      true,
				Action:       c.policy.Action,
			})
		}
	}
	if len(decisions) > 0 {
		if err := s.Policies.RecordDecisions(ctx, decisions); err != nil {
			return err
		}
	}
	if !maxAt.IsZero() {
		if err := s.Policies.SetSweepWatermark(ctx, orgID, maxAt); err != nil {
			return err
		}
	}
	return nil
}

// BackfillPolicy is the one-shot post-create / post-update hook: run a single
// policy against the last `window` of an org's invocations and persist
// matching decisions. Cap at `limit` rows scanned so a noisy seed dataset
// can't drag a single API handler. Best-effort: errors are logged.
func (s *PolicySweeper) BackfillPolicy(ctx context.Context, p models.Policy, window time.Duration, limit int) {
	if s.Engine == nil || s.Policies == nil {
		return
	}
	if limit <= 0 {
		limit = 5000
	}
	prg, err := s.Engine.Compile(p.Expression)
	if err != nil {
		log.Printf("policy backfill: compile %s: %v", p.ID, err)
		return
	}
	to := time.Now().UTC()
	from := to.Add(-window)
	invs, err := s.Policies.ListInvocationsInWindow(ctx, p.OrgID, from, to, limit)
	if err != nil {
		log.Printf("policy backfill: list invocations: %v", err)
		return
	}
	decisions := make([]store.DecisionRow, 0)
	for _, inv := range invs {
		ic := policy.InvocationContext{
			At:         inv.At,
			Status:     inv.Status,
			Caller:     inv.Caller,
			Server:     inv.Server,
			Capability: inv.Capability,
			Workload:   inv.Workload,
			Network:    inv.Network,
		}
		matched, err := prg.Evaluate(ctx, ic)
		if err != nil || !matched {
			continue
		}
		decisions = append(decisions, store.DecisionRow{
			InvocationID: inv.ID,
			PolicyID:     p.ID,
			Matched:      true,
			Action:       p.Action,
		})
	}
	if len(decisions) > 0 {
		if err := s.Policies.RecordDecisions(ctx, decisions); err != nil {
			log.Printf("policy backfill: record decisions: %v", err)
		}
	}
}
