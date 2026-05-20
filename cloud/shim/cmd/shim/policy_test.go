package main

import (
	"strings"
	"testing"
	"time"
)

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
		server:     map[string]string{"name": "github-mcp"},
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
		server:     map[string]string{"name": "gitlab-mcp"},
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
		server:     map[string]string{"name": "github-mcp"},
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
		server:     map[string]string{"name": "github-mcp", "transport": "http", "address": "x"},
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
