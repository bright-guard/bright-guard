package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// newDeterministicRNG returns a math/rand source seeded with a fixed value so
// the synthesiseContext distribution test stays reproducible. Local to the
// test file — no production code path should rely on a fixed seed.
func newDeterministicRNG() *rand.Rand {
	return rand.New(rand.NewSource(42))
}

func TestCompilePolicy_ValidExpressions(t *testing.T) {
	// Same set the server-side policy package tests against — verifies
	// the shim's CEL env exactly matches the control plane's.
	cases := []string{
		`capability.name == "create_issue" && server.name == "github-mcp"`,
		`server.exposureState == "public" && capability.kind == "resource"`,
		`caller.agent == "copilot-agent" && capability.name.startsWith("write_")`,
		`status == "ok"`,
		`at > timestamp("2026-01-01T00:00:00Z")`,
	}
	for _, expr := range cases {
		if _, err := compilePolicy(bundlePolicyWire{
			ID:         "p1",
			Name:       "p1",
			Action:     "deny",
			Expression: expr,
		}); err != nil {
			t.Errorf("compile %q: %v", expr, err)
		}
	}
}

func TestCompilePolicy_RejectsNonBool(t *testing.T) {
	cases := []string{`capability.name`, `1 + 1`, `server`}
	for _, expr := range cases {
		if _, err := compilePolicy(bundlePolicyWire{
			ID: "p", Name: "p", Action: "deny", Expression: expr,
		}); err == nil {
			t.Errorf("expected non-bool expr %q to fail compile", expr)
		}
	}
}

func TestCompilePolicy_SyntaxError(t *testing.T) {
	_, err := compilePolicy(bundlePolicyWire{
		ID: "p", Name: "p", Action: "deny", Expression: `capability.name ==`,
	})
	if err == nil {
		t.Fatal("expected syntax error")
	}
}

func TestCompilePolicy_UndeclaredVariable(t *testing.T) {
	// "user" isn't in the env — only "caller" is. Same restriction as server.
	_, err := compilePolicy(bundlePolicyWire{
		ID: "p", Name: "p", Action: "deny", Expression: `user.email == "x"`,
	})
	if err == nil {
		t.Fatal("expected undeclared-variable error")
	}
}

func TestEvaluatePolicies_DenyMatchEmits(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "pol-1", Name: "block create_issue", Action: "deny",
		Expression: `capability.name == "create_issue" && server.name == "github-mcp"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		caller:     map[string]any{"agent": "demo-agent"},
		server:     map[string]any{"name": "github-mcp"},
		capability: map[string]string{"kind": "tool", "name": "create_issue"},
		at:         time.Now().UTC(),
		status:     "ok",
	})
	if len(decs) != 1 {
		t.Fatalf("want 1 decision, got %d", len(decs))
	}
	d := decs[0]
	if d.PolicyID != "pol-1" || d.Action != "deny" || !d.Matched {
		t.Errorf("bad decision: %+v", d)
	}
}

func TestEvaluatePolicies_MissEmitsNothing(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "pol-1", Name: "x", Action: "deny",
		Expression: `capability.name == "create_issue" && server.name == "github-mcp"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		server:     map[string]any{"name": "gitlab-mcp"},
		capability: map[string]string{"name": "create_issue"},
	})
	if len(decs) != 0 {
		t.Errorf("expected no decisions, got %+v", decs)
	}
}

func TestEvaluatePolicies_WarnAndDenyBothEmit(t *testing.T) {
	deny, err := compilePolicy(bundlePolicyWire{
		ID: "d", Name: "deny rule", Action: "deny",
		Expression: `capability.name == "create_issue"`,
	})
	if err != nil {
		t.Fatalf("compile deny: %v", err)
	}
	warn, err := compilePolicy(bundlePolicyWire{
		ID: "w", Name: "warn rule", Action: "warn",
		Expression: `capability.kind == "tool"`,
	})
	if err != nil {
		t.Fatalf("compile warn: %v", err)
	}
	decs := evaluatePolicies([]compiledPolicy{deny, warn}, evalContext{
		server:     map[string]any{"name": "github-mcp"},
		capability: map[string]string{"kind": "tool", "name": "create_issue"},
	})
	if len(decs) != 2 {
		t.Fatalf("want 2 decisions, got %d", len(decs))
	}
	actions := map[string]bool{}
	for _, d := range decs {
		actions[d.Action] = true
	}
	if !actions["deny"] || !actions["warn"] {
		t.Errorf("want both deny+warn, got %+v", actions)
	}
}

func TestEvaluatePolicies_EvalErrorTreatedAsNonMatch(t *testing.T) {
	// caller.agent on an empty caller jsonb is a runtime error in CEL — we
	// surface it as non-match, never as a panic / hard failure.
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "p", Name: "p", Action: "deny",
		Expression: `caller.agent == "x"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		caller: map[string]any{},
	})
	if len(decs) != 0 {
		t.Errorf("expected no decisions on eval error, got %+v", decs)
	}
}

func TestPolicyCache_BadBundleKeepsPrevious(t *testing.T) {
	// fail-closed semantics: if applying a new bundle fails compile on any
	// policy, the cache must retain the previous version + programs.
	c := newPolicyCache()

	good := &policyBundleWire{
		Version: 1,
		Policies: []bundlePolicyWire{
			{ID: "p1", Name: "p1", Action: "deny", Expression: `status == "ok"`},
		},
	}
	c.apply(good)
	if c.version() != 1 {
		t.Fatalf("expected v1 active, got %d", c.version())
	}
	if len(c.programs()) != 1 {
		t.Fatalf("expected 1 program, got %d", len(c.programs()))
	}

	bad := &policyBundleWire{
		Version: 2,
		Policies: []bundlePolicyWire{
			{ID: "p1", Name: "p1", Action: "deny", Expression: `status == "ok"`},
			{ID: "p2", Name: "broken", Action: "deny", Expression: `capability.name ==`},
		},
	}
	c.apply(bad)
	if c.version() != 1 {
		t.Errorf("expected version to stay at 1 after bad bundle, got %d", c.version())
	}
	if len(c.programs()) != 1 {
		t.Errorf("expected 1 program retained, got %d", len(c.programs()))
	}
}

func TestPolicyCache_NilBundleNoop(t *testing.T) {
	c := newPolicyCache()
	c.apply(&policyBundleWire{Version: 1, Policies: []bundlePolicyWire{
		{ID: "p", Name: "p", Action: "deny", Expression: `status == "ok"`},
	}})
	c.apply(nil) // server returned no bundle (we're up to date)
	if c.version() != 1 || len(c.programs()) != 1 {
		t.Errorf("nil bundle should be no-op, got v=%d progs=%d", c.version(), len(c.programs()))
	}
}

// End-to-end-ish: with a real CEL deny policy matching a synthetic invocation
// context, the same shape the emit loop builds, we expect status flip + a
// matched=true decision recorded.
func TestShimEmitLoop_StatusFlipOnDeny(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "pol-deny-create-issue", Name: "deny create_issue", Action: "deny",
		Expression: `capability.name == "create_issue"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	progs := []compiledPolicy{cp}

	status := "ok"
	caller := map[string]any{"agent": "demo-agent"}
	decisions := evaluatePolicies(progs, evalContext{
		caller:     caller,
		server:     map[string]any{"name": "github-mcp", "transport": "http", "address": "x"},
		capability: map[string]string{"kind": "tool", "name": "create_issue", "description": ""},
		at:         time.Now().UTC(),
		status:     status,
	})
	for _, d := range decisions {
		if d.Action == "deny" && status != "denied" {
			status = "denied"
		}
	}
	if status != "denied" {
		t.Fatalf("expected status flip to denied, got %q", status)
	}
	if len(decisions) != 1 || decisions[0].PolicyID != "pol-deny-create-issue" {
		t.Errorf("expected one matching decision, got %+v", decisions)
	}
}

func TestShimEmitLoop_WarnDoesNotFlipStatus(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "pol-warn-tool", Name: "warn tool", Action: "warn",
		Expression: `capability.kind == "tool"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	status := "ok"
	decisions := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		capability: map[string]string{"kind": "tool", "name": "anything"},
	})
	for _, d := range decisions {
		if d.Action == "deny" && status != "denied" {
			status = "denied"
		}
	}
	if status != "ok" {
		t.Errorf("warn should not flip status, got %q", status)
	}
	if len(decisions) != 1 || decisions[0].Action != "warn" {
		t.Errorf("expected one warn decision, got %+v", decisions)
	}
}

// TestUC8_BlockPublicExposure_EndToEnd: the shipped UC8 template, fed a fake
// invocation against a server with exposure_state=public, produces decision=
// denied. Flipping to internal returns no decision (allowed). This is the
// load-bearing assertion that the Wave N+8 deny path actually denies.
func TestUC8_BlockPublicExposure_EndToEnd(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "tpl-uc8", Name: "block public", Action: "deny",
		Expression: `server.exposure_state == "public"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// public exposure -> deny
	status := "ok"
	decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		caller:     map[string]any{"agent": "demo-agent", "flagged_new": false, "acknowledged": false},
		server:     map[string]any{"name": "github-mcp", "exposure_state": "public", "exposureState": "public"},
		capability: map[string]string{"kind": "tool", "name": "create_issue"},
		at:         time.Now().UTC(),
		status:     status,
	})
	for _, d := range decs {
		if d.Action == "deny" && status != "denied" {
			status = "denied"
		}
	}
	if status != "denied" {
		t.Fatalf("public exposure: expected status flip to denied, got %q (decisions=%+v)", status, decs)
	}
	if len(decs) != 1 || decs[0].PolicyID != "tpl-uc8" {
		t.Errorf("expected one matching decision, got %+v", decs)
	}

	// internal exposure -> allow
	status = "ok"
	decs = evaluatePolicies([]compiledPolicy{cp}, evalContext{
		caller:     map[string]any{"agent": "demo-agent"},
		server:     map[string]any{"name": "github-mcp", "exposure_state": "internal", "exposureState": "internal"},
		capability: map[string]string{"kind": "tool", "name": "create_issue"},
		at:         time.Now().UTC(),
		status:     status,
	})
	for _, d := range decs {
		if d.Action == "deny" && status != "denied" {
			status = "denied"
		}
	}
	if status != "ok" {
		t.Errorf("internal exposure: status should stay ok, got %q", status)
	}
	if len(decs) != 0 {
		t.Errorf("internal exposure: expected no decisions, got %+v", decs)
	}
}

// TestUC9_BlockUnapprovedCallers_EndToEnd: the shipped UC9 template denies
// flagged_new=true && !acknowledged callers and allows everything else.
func TestUC9_BlockUnapprovedCallers_EndToEnd(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "tpl-uc9", Name: "block unapproved callers", Action: "deny",
		Expression: `caller.flagged_new && !caller.acknowledged`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	cases := []struct {
		name        string
		flaggedNew  bool
		acknowledged bool
		wantDenied  bool
	}{
		{"flagged_new && !acknowledged → denied", true, false, true},
		{"flagged_new but acknowledged → allowed", true, true, false},
		{"aged out (flagged_new=false) → allowed", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := "ok"
			decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
				caller: map[string]any{
					"agent":        "demo-agent",
					"flagged_new":  tc.flaggedNew,
					"acknowledged": tc.acknowledged,
				},
				server:     map[string]any{"name": "github-mcp", "exposure_state": "internal"},
				capability: map[string]string{"kind": "tool", "name": "list_issues"},
				at:         time.Now().UTC(),
				status:     status,
			})
			for _, d := range decs {
				if d.Action == "deny" && status != "denied" {
					status = "denied"
				}
			}
			wantStatus := "ok"
			if tc.wantDenied {
				wantStatus = "denied"
			}
			if status != wantStatus {
				t.Errorf("got status=%q want=%q (decisions=%+v)", status, wantStatus, decs)
			}
		})
	}
}

// TestBundleSnapshot_PopulatesCallerCache: applying a bundle with callers[]
// makes callerBySignature(sig) return the populated row. This is the
// integration point between the heartbeat-delivered payload and the shim's
// per-invocation enrichment.
func TestBundleSnapshot_PopulatesCallerCache(t *testing.T) {
	c := newPolicyCache()
	c.apply(&policyBundleWire{
		Version: 7,
		Policies: []bundlePolicyWire{
			{ID: "p1", Name: "x", Action: "warn", Expression: `status == "ok"`},
		},
		Callers: []bundleCallerWire{
			{Signature: "abc123", Label: "alice", FlaggedNew: true, Acknowledged: false},
			{Signature: "def456", Label: "bot", FlaggedNew: false, Acknowledged: true},
		},
		Servers: []bundleServerWire{
			{ID: "s-1", Name: "github-mcp", Address: "https://api.github.com/mcp", ExposureState: "public"},
		},
	})

	got := c.callerBySignature("abc123")
	if got.Label != "alice" || !got.FlaggedNew || got.Acknowledged {
		t.Errorf("alice lookup: %+v", got)
	}
	got = c.callerBySignature("def456")
	if got.Label != "bot" || got.FlaggedNew || !got.Acknowledged {
		t.Errorf("bot lookup: %+v", got)
	}
	srv := c.serverByName("github-mcp")
	if srv.ID != "s-1" || srv.ExposureState != "public" {
		t.Errorf("github-mcp lookup: %+v", srv)
	}
	// Unknown lookups return zero values, not a panic.
	miss := c.callerBySignature("nope")
	if miss.Signature != "" || miss.FlaggedNew {
		t.Errorf("unknown caller lookup should be zero, got %+v", miss)
	}
	missSrv := c.serverByName("nope")
	if missSrv.Name != "" {
		t.Errorf("unknown server lookup should be zero, got %+v", missSrv)
	}
}

// TestEvalContext_BundleEnrichmentWires_CallerFlaggedNew: end-to-end through
// the cache + signature + enrichment helpers. A fake caller produced by the
// shim, when its signature is in the bundle, makes the UC9 template deny.
func TestEvalContext_BundleEnrichmentWires_CallerFlaggedNew(t *testing.T) {
	caller := map[string]any{"agent": "demo-agent"}
	sig := callerSignature(caller)
	if sig == "" {
		t.Fatal("empty signature")
	}

	c := newPolicyCache()
	c.apply(&policyBundleWire{
		Version: 1,
		Policies: []bundlePolicyWire{
			{ID: "uc9", Name: "uc9", Action: "deny", Expression: `caller.flagged_new && !caller.acknowledged`},
		},
		Callers: []bundleCallerWire{
			{Signature: sig, Label: "demo-agent", FlaggedNew: true, Acknowledged: false},
		},
	})

	enriched := enrichCallerForEval(caller, sig, c.callerBySignature(sig))
	if v, ok := enriched["flagged_new"].(bool); !ok || !v {
		t.Fatalf("enrichment missing flagged_new=true, got %+v", enriched)
	}

	status := "ok"
	decs := evaluatePolicies(c.programs(), evalContext{
		caller:     enriched,
		server:     map[string]any{"name": "github-mcp", "exposure_state": "internal"},
		capability: map[string]string{"kind": "tool", "name": "create_issue"},
		at:         time.Now().UTC(),
		status:     status,
	})
	for _, d := range decs {
		if d.Action == "deny" && status != "denied" {
			status = "denied"
		}
	}
	if status != "denied" {
		t.Errorf("expected status=denied, got %q (decisions=%+v)", status, decs)
	}
}

func TestCallerSignature_StableAcrossKeyOrder(t *testing.T) {
	a := callerSignature(map[string]any{"agent": "demo", "user": "alice"})
	b := callerSignature(map[string]any{"user": "alice", "agent": "demo"})
	if a != b {
		t.Errorf("signature differs with key order: %s vs %s", a, b)
	}
}

// TestEvaluatePolicies_WorkloadScope_Present mirrors the API-side
// TestEvaluate_WorkloadScope test — same expression, same expected verdict —
// to assert the byte-for-byte env match also produces the same eval semantics
// for the new UC6 scope.
func TestEvaluatePolicies_WorkloadScope_Present(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "uc6", Name: "uc6", Action: "deny",
		Expression: `workload.cluster == "prod"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		workload: map[string]any{"cluster": "prod"},
		at:       time.Now().UTC(),
		status:   "ok",
	})
	if len(decs) != 1 {
		t.Fatalf("workload.cluster==prod: want 1 decision, got %d", len(decs))
	}
}

// TestEvaluatePolicies_WorkloadScope_Absent confirms a policy referencing
// workload.* does not match (and does not error) when no workload context is
// supplied. This is the load-bearing behaviour for backwards-compat with
// older invocations that don't carry UC6 metadata.
func TestEvaluatePolicies_WorkloadScope_Absent(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "uc6", Name: "uc6", Action: "deny",
		Expression: `workload.cluster == "prod"`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		at:     time.Now().UTC(),
		status: "ok",
	})
	if len(decs) != 0 {
		t.Errorf("absent workload should not match, got %+v", decs)
	}
}

// TestEvaluatePolicies_NetworkScope verifies the UC7 corp-net template on the
// shim — same shape as the API-side test, asserts the shim's eval honors both
// the present + absent paths.
func TestEvaluatePolicies_NetworkScope(t *testing.T) {
	cp, err := compilePolicy(bundlePolicyWire{
		ID: "uc7", Name: "uc7", Action: "deny",
		Expression: `network.subnet != "" && !network.subnet.startsWith("10.")`,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Public subnet → match.
	decs := evaluatePolicies([]compiledPolicy{cp}, evalContext{
		network: map[string]any{"subnet": "172.16.0.0/16"},
		at:      time.Now().UTC(),
		status:  "ok",
	})
	if len(decs) != 1 {
		t.Errorf("172.16/16: want 1 decision, got %+v", decs)
	}
	// Internal subnet → no match.
	decs = evaluatePolicies([]compiledPolicy{cp}, evalContext{
		network: map[string]any{"subnet": "10.0.1.0/24"},
	})
	if len(decs) != 0 {
		t.Errorf("10.0/24: want 0 decisions, got %+v", decs)
	}
	// Absent network → subnet=="" filter short-circuits to false.
	decs = evaluatePolicies([]compiledPolicy{cp}, evalContext{})
	if len(decs) != 0 {
		t.Errorf("absent network: want 0 decisions, got %+v", decs)
	}
}

// TestSynthesiseContext_DistributesAndOmits asserts the shim's fake-data
// generator both produces non-nil context most of the time AND occasionally
// returns (nil, nil) so the missing-context path is exercised. Statistical
// not exact — 200 draws give enough headroom that 0 nils OR 200 nils would
// both be a real bug rather than a flake.
func TestSynthesiseContext_DistributesAndOmits(t *testing.T) {
	rng := newDeterministicRNG()
	nilCount, presentCount := 0, 0
	for i := 0; i < 200; i++ {
		wl, nw := synthesiseContext(rng)
		if wl == nil && nw == nil {
			nilCount++
		} else if wl != nil && nw != nil {
			presentCount++
		}
	}
	if nilCount == 0 {
		t.Errorf("synthesiseContext never returned nil — missing-context path not exercised")
	}
	if presentCount == 0 {
		t.Errorf("synthesiseContext never returned non-nil — UC6/UC7 templates would never match")
	}
}

func TestSharedCELEnv_NoExtensions(t *testing.T) {
	// Defensive: if cel.NewEnv ever gains accidental ext additions the env
	// would diverge from the server's. As long as compiling the documented
	// example expressions works, we're aligned.
	env, err := sharedCELEnv()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	_, iss := env.Compile(`caller.agent == "x"`)
	if iss != nil && iss.Err() != nil {
		t.Fatalf("env compile: %v", iss.Err())
	}
	// ext.Strings would expose `quote()` etc. Compile must fail without it.
	_, iss = env.Compile(`quote("hi")`)
	if iss == nil || iss.Err() == nil {
		t.Fatal("ext.Strings.quote() should not be available")
	}
	if !strings.Contains(iss.Err().Error(), "quote") {
		t.Errorf("unexpected error: %v", iss.Err())
	}
}
