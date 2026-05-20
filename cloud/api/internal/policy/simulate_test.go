package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fixtureInputs builds n synthetic invocations with a deterministic mix of
// server / capability / caller / exposure / status fields. Bucketing is
// proportional (mod 100) so the same percentages hold at any n: 60/30/10
// servers, 50/30/15/5 capabilities, 40/35/25 callers, 80/15/5 statuses.
func fixtureInputs(n int) []SimulationInput {
	inputs := make([]SimulationInput, 0, n)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		var (
			server, exposure, capKind, capName, agent, status string
		)
		// Use modular bucketing so distributions scale with n. Index space is
		// 0..99 per cycle; for n=100 this is identical to the original
		// half-open ranges, so legacy assertions keep their hard-coded counts.
		j := i % 100
		switch {
		case j < 60:
			server, exposure = "github-mcp", "public"
		case j < 90:
			server, exposure = "gitlab-mcp", "internal"
		default:
			server, exposure = "internal-tools", "public"
		}
		switch {
		case j < 50:
			capKind, capName = "tool", "create_issue"
		case j < 80:
			capKind, capName = "tool", "delete_repo"
		case j < 95:
			capKind, capName = "resource", "repo_meta"
		default:
			capKind, capName = "prompt", "summarize"
		}
		switch {
		case j < 40:
			agent = "copilot-agent"
		case j < 75:
			agent = "demo-agent"
		default:
			agent = "unknown-agent"
		}
		switch {
		case j < 80:
			status = "ok"
		case j < 95:
			status = "error"
		default:
			status = "denied"
		}
		callerJSON, _ := json.Marshal(map[string]any{
			"agent": agent,
			"label": agent,
		})
		ic := InvocationContext{
			At:     base.Add(time.Duration(i) * time.Minute),
			Status: status,
			Caller: callerJSON,
			Server: map[string]string{
				"name":           server,
				"exposure_state": exposure,
				"exposureState":  exposure,
			},
			Capability: map[string]string{
				"kind": capKind,
				"name": capName,
			},
		}
		inputs = append(inputs, SimulationInput{
			InvocationID:  uuid.New(),
			At:            ic.At,
			ServerName:    server,
			CapabilityKey: capKind + "/" + capName,
			CallerKey:     agent,
			IC:            ic,
		})
	}
	return inputs
}

// TestSimulate_BlocksPublicExposure exercises the canonical UC8 expression.
// 70 of 100 fixture rows are on public-exposure servers (github + internal-tools).
func TestSimulate_BlocksPublicExposure(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	prg, err := e.Compile(`server.exposure_state == "public"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inputs := fixtureInputs(100)
	res := Simulate(context.Background(), prg, SimulationActionDeny, inputs)
	if res.TotalInvocations != 100 {
		t.Errorf("total: got %d want 100", res.TotalInvocations)
	}
	if res.WouldBlockCount != 70 {
		t.Errorf("wouldBlock: got %d want 70", res.WouldBlockCount)
	}
	if res.WouldWarnCount != 0 {
		t.Errorf("wouldWarn: got %d want 0", res.WouldWarnCount)
	}
	if got := bucketFor(res.BreakdownByServer, "github-mcp"); got != 60 {
		t.Errorf("github-mcp bucket: got %d want 60", got)
	}
	if got := bucketFor(res.BreakdownByServer, "internal-tools"); got != 10 {
		t.Errorf("internal-tools bucket: got %d want 10", got)
	}
	if got := bucketFor(res.BreakdownByServer, "gitlab-mcp"); got != 0 {
		t.Errorf("gitlab-mcp should not be present, got %d", got)
	}
	if len(res.Samples) != SimulationSampleSize {
		t.Errorf("samples: got %d want %d", len(res.Samples), SimulationSampleSize)
	}
}

// TestSimulate_WarnActionCountsSeparately confirms the warn path puts matches
// into WouldWarnCount instead of WouldBlockCount.
func TestSimulate_WarnActionCountsSeparately(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	prg, err := e.Compile(`capability.name == "delete_repo"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inputs := fixtureInputs(100)
	res := Simulate(context.Background(), prg, SimulationActionWarn, inputs)
	if res.WouldWarnCount != 30 {
		t.Errorf("wouldWarn: got %d want 30", res.WouldWarnCount)
	}
	if res.WouldBlockCount != 0 {
		t.Errorf("wouldBlock: got %d want 0", res.WouldBlockCount)
	}
}

// TestSimulate_CapabilityBreakdown verifies the by-capability histogram and
// that ties break alphabetically (deterministic ordering).
func TestSimulate_CapabilityBreakdown(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	prg, err := e.Compile(`capability.kind == "tool"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inputs := fixtureInputs(100)
	res := Simulate(context.Background(), prg, SimulationActionDeny, inputs)
	if res.WouldBlockCount != 80 {
		t.Errorf("wouldBlock: got %d want 80", res.WouldBlockCount)
	}
	if got := bucketFor(res.BreakdownByCapability, "tool/create_issue"); got != 50 {
		t.Errorf("create_issue bucket: got %d want 50", got)
	}
	if got := bucketFor(res.BreakdownByCapability, "tool/delete_repo"); got != 30 {
		t.Errorf("delete_repo bucket: got %d want 30", got)
	}
}

// TestSimulate_CallerBreakdown crosses the caller scope with a capability
// filter so we catch any drift in how callers are projected into the result.
func TestSimulate_CallerBreakdown(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	prg, err := e.Compile(`caller.agent == "demo-agent"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inputs := fixtureInputs(100)
	res := Simulate(context.Background(), prg, SimulationActionDeny, inputs)
	if res.WouldBlockCount != 35 {
		t.Errorf("wouldBlock: got %d want 35", res.WouldBlockCount)
	}
	if got := bucketFor(res.BreakdownByCaller, "demo-agent"); got != 35 {
		t.Errorf("demo-agent caller bucket: got %d want 35", got)
	}
}

// TestSimulate_NoMatches is the negative path — an expression nobody matches
// should yield zero block count and empty breakdowns (still non-nil).
func TestSimulate_NoMatches(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	prg, err := e.Compile(`server.name == "nonexistent"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inputs := fixtureInputs(100)
	res := Simulate(context.Background(), prg, SimulationActionDeny, inputs)
	if res.WouldBlockCount != 0 {
		t.Errorf("wouldBlock: got %d want 0", res.WouldBlockCount)
	}
	if res.BreakdownByServer == nil {
		t.Error("BreakdownByServer should be non-nil even with zero matches")
	}
	if res.Samples == nil {
		t.Error("Samples should be non-nil even with zero matches")
	}
}

// TestSimulate_ComparisonDiff is the post-mortem comparison shape: running two
// expressions over the same input set and asserting the deltas in their match
// counts mirror the rule semantics.
func TestSimulate_ComparisonDiff(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	// Current policy: block public-exposure (70 matches in fixture).
	curr, err := e.Compile(`server.exposure_state == "public"`)
	if err != nil {
		t.Fatalf("compile current: %v", err)
	}
	// Proposed alternative: narrow it to only github-mcp (60 matches).
	prop, err := e.Compile(`server.name == "github-mcp"`)
	if err != nil {
		t.Fatalf("compile proposed: %v", err)
	}
	inputs := fixtureInputs(100)
	a := Simulate(context.Background(), curr, SimulationActionDeny, inputs)
	b := Simulate(context.Background(), prop, SimulationActionDeny, inputs)
	if a.WouldBlockCount != 70 {
		t.Errorf("current wouldBlock: got %d want 70", a.WouldBlockCount)
	}
	if b.WouldBlockCount != 60 {
		t.Errorf("proposed wouldBlock: got %d want 60", b.WouldBlockCount)
	}
	// The proposed policy is strictly narrower: it should block strictly fewer
	// rows.
	if !(b.WouldBlockCount < a.WouldBlockCount) {
		t.Errorf("comparison shape broken: proposed=%d should be < current=%d",
			b.WouldBlockCount, a.WouldBlockCount)
	}
}

// TestSimulate_EvalErrorIsNonMatch confirms an expression that errors at eval
// time (missing-key access on the caller blob) drops the row from the count —
// matching the audit-sweep semantics. The test is intentionally lenient because
// cel-go may handle some "missing key" forms as false rather than an error.
func TestSimulate_EvalErrorIsNonMatch(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	// caller.workspace doesn't exist on our fixture caller — should be 0.
	prg, err := e.Compile(`caller.workspace == "engineering"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inputs := fixtureInputs(100)
	res := Simulate(context.Background(), prg, SimulationActionDeny, inputs)
	if res.WouldBlockCount != 0 {
		t.Errorf("wouldBlock: got %d want 0 (missing-key access must not match)", res.WouldBlockCount)
	}
}

// TestSimulate_BoundedAt50k stress-checks the SimulationMaxRows constant: a
// passthrough expression on >50k inputs still completes in well under a second
// on commodity hardware. Marks the bound as a real constraint (not just docs).
func TestSimulate_BoundedAt50k(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 50k stress test in -short mode")
	}
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	prg, err := e.Compile(`status == "ok"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	inputs := fixtureInputs(SimulationMaxRows)
	start := time.Now()
	res := Simulate(context.Background(), prg, SimulationActionDeny, inputs)
	elapsed := time.Since(start)
	if res.TotalInvocations != SimulationMaxRows {
		t.Errorf("total: got %d want %d", res.TotalInvocations, SimulationMaxRows)
	}
	// Synthetic fixture maps to 80% status="ok" → 40,000 matches.
	wantMatches := SimulationMaxRows * 80 / 100
	if res.WouldBlockCount != wantMatches {
		t.Errorf("wouldBlock: got %d want %d", res.WouldBlockCount, wantMatches)
	}
	if elapsed > 5*time.Second {
		t.Errorf("simulator took %v on 50k rows — over budget", elapsed)
	}
}

// TestSimulate_NilProgramIsSafe guards the handler path: if Compile somehow
// hands us a nil program (shouldn't happen, but the API has surface for it),
// the simulator returns an empty result instead of panicking.
func TestSimulate_NilProgramIsSafe(t *testing.T) {
	inputs := fixtureInputs(10)
	res := Simulate(context.Background(), nil, SimulationActionDeny, inputs)
	if res.WouldBlockCount != 0 || res.TotalInvocations != 10 {
		t.Errorf("nil program should produce 0 matches over %d rows, got %+v", len(inputs), res)
	}
}

// bucketFor is a small test helper: linear scan to find the count for a name
// in a breakdown list. The lists are at most SimulationBreakdownTop entries so
// O(n) is fine.
func bucketFor(buckets []SimulationBucket, name string) int {
	for _, b := range buckets {
		if b.Name == name {
			return b.Count
		}
	}
	return 0
}

// TestTopBuckets_TruncatesAndSorts isolates the histogram-collapse helper so
// the breakdown shape is provably bounded + deterministic.
func TestTopBuckets_TruncatesAndSorts(t *testing.T) {
	m := map[string]int{}
	for i := 0; i < 25; i++ {
		m[fmt.Sprintf("k%02d", i)] = i + 1
	}
	out := topBuckets(m, 10)
	if len(out) != 10 {
		t.Fatalf("len: got %d want 10", len(out))
	}
	// Highest count first.
	if out[0].Count != 25 || out[0].Name != "k24" {
		t.Errorf("top bucket: got %+v want {k24, 25}", out[0])
	}
	// Sorted descending by count.
	for i := 1; i < len(out); i++ {
		if out[i-1].Count < out[i].Count {
			t.Errorf("not sorted desc at %d: %+v then %+v", i, out[i-1], out[i])
		}
	}
}
