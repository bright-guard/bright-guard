package policy

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestTemplates_Compile is the contract: every shipped template MUST compile
// against the live CEL env. A drift between env and template ships an
// uncreatable policy.
func TestTemplates_Compile(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	for _, tpl := range Templates() {
		if _, err := e.Compile(tpl.Expression); err != nil {
			t.Errorf("template %s: compile %q: %v", tpl.ID, tpl.Expression, err)
		}
	}
}

// TestTemplate_UC8_PublicExposure proves the "block public-exposure servers"
// template ends-to-end: a server with exposure_state=public matches; flipping
// the state to internal stops matching. This is the UC8 deny path the wave
// ships.
func TestTemplate_UC8_PublicExposure(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	var tpl Template
	for _, t := range Templates() {
		if t.ID == "block-public-exposure" {
			tpl = t
			break
		}
	}
	if tpl.ID == "" {
		t.Fatal("template block-public-exposure missing")
	}
	prg, err := e.Compile(tpl.Expression)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	at := time.Now().UTC()

	// public exposure -> match (denied)
	got, err := prg.Evaluate(context.Background(), InvocationContext{
		At:     at,
		Status: "ok",
		Server: map[string]string{"name": "github-mcp", "exposure_state": "public"},
	})
	if err != nil {
		t.Fatalf("eval public: %v", err)
	}
	if !got {
		t.Errorf("public exposure expected to match, got false")
	}

	// internal exposure -> no match (allowed)
	got, err = prg.Evaluate(context.Background(), InvocationContext{
		At:     at,
		Status: "ok",
		Server: map[string]string{"name": "github-mcp", "exposure_state": "internal"},
	})
	if err != nil {
		t.Fatalf("eval internal: %v", err)
	}
	if got {
		t.Errorf("internal exposure expected NOT to match, got true")
	}
}

// TestTemplate_UC9_UnapprovedCaller proves the "block unapproved callers"
// template: flagged_new && !acknowledged denies; acknowledged allows; aged-out
// (flagged_new=false) allows.
func TestTemplate_UC9_UnapprovedCaller(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	var tpl Template
	for _, t := range Templates() {
		if t.ID == "block-unapproved-callers" {
			tpl = t
			break
		}
	}
	if tpl.ID == "" {
		t.Fatal("template block-unapproved-callers missing")
	}
	prg, err := e.Compile(tpl.Expression)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	cases := []struct {
		name        string
		flaggedNew  bool
		acknowledged bool
		want        bool
	}{
		{"flagged_new && !acknowledged → deny", true, false, true},
		{"flagged_new but acknowledged → allow", true, true, false},
		{"aged out (flagged_new=false) → allow", false, false, false},
		{"aged out + acknowledged → allow", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caller := map[string]any{
				"agent":        "demo-agent",
				"flagged_new":  tc.flaggedNew,
				"acknowledged": tc.acknowledged,
			}
			cb, _ := json.Marshal(caller)
			got, err := prg.Evaluate(context.Background(), InvocationContext{
				Caller: cb,
				At:     time.Now().UTC(),
				Status: "ok",
				Server: map[string]string{"name": "s"},
			})
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got != tc.want {
				t.Errorf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

// TestTemplate_RequestNow confirms request.now is usable from a CEL expression
// — the env declaration alone is insufficient if eval doesn't bind a value.
func TestTemplate_RequestNow(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	prg, err := e.Compile(`request.now > timestamp("2025-01-01T00:00:00Z")`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := prg.Evaluate(context.Background(), InvocationContext{
		At:     time.Now().UTC(),
		Status: "ok",
		Server: map[string]string{"name": "s"},
	})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Errorf("expected request.now to be after 2025-01-01")
	}
}
