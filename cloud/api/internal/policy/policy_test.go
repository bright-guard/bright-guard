package policy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func newEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestCompile_Examples(t *testing.T) {
	e := newEngine(t)
	cases := []string{
		`capability.name == "create_issue" && server.name == "github-mcp"`,
		`server.exposureState == "public" && capability.kind == "resource"`,
		`caller.agent == "copilot-agent" && capability.name.startsWith("write_")`,
		`status == "ok"`,
		`at > timestamp("2026-01-01T00:00:00Z")`,
	}
	for _, expr := range cases {
		if _, err := e.Compile(expr); err != nil {
			t.Errorf("compile %q: %v", expr, err)
		}
	}
}

func TestCompile_RejectsNonBool(t *testing.T) {
	e := newEngine(t)
	cases := []string{
		`capability.name`,    // string output
		`1 + 1`,              // int output
		`server`,             // map output
	}
	for _, expr := range cases {
		if _, err := e.Compile(expr); err == nil {
			t.Errorf("expected non-bool expression %q to fail compile", expr)
		}
	}
}

func TestCompile_SyntaxError(t *testing.T) {
	e := newEngine(t)
	_, err := e.Compile(`capability.name ==`)
	if err == nil {
		t.Fatal("expected syntax error")
	}
	// User-facing message should contain the line:col marker cel-go produces.
	if !strings.Contains(err.Error(), ":1:") && !strings.Contains(err.Error(), "ERROR") {
		t.Errorf("error not user-friendly: %v", err)
	}
}

func TestCompile_UndeclaredVariable(t *testing.T) {
	e := newEngine(t)
	// "user" isn't in the env — only "caller" is. Expect a check error.
	_, err := e.Compile(`user.email == "x"`)
	if err == nil {
		t.Fatal("expected undeclared-variable error")
	}
}

func TestEvaluate_TableExamples(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()
	at := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		expr string
		ic   InvocationContext
		want bool
	}{
		{
			name: "exact tool match on named server",
			expr: `capability.name == "create_issue" && server.name == "github-mcp"`,
			ic: InvocationContext{
				At:         at,
				Status:     "ok",
				Server:     map[string]string{"name": "github-mcp"},
				Capability: map[string]string{"kind": "tool", "name": "create_issue"},
			},
			want: true,
		},
		{
			name: "exact match misses on different server",
			expr: `capability.name == "create_issue" && server.name == "github-mcp"`,
			ic: InvocationContext{
				At:         at,
				Status:     "ok",
				Server:     map[string]string{"name": "gitlab-mcp"},
				Capability: map[string]string{"name": "create_issue"},
			},
			want: false,
		},
		{
			name: "public resource gate matches",
			expr: `server.exposureState == "public" && capability.kind == "resource"`,
			ic: InvocationContext{
				Server:     map[string]string{"exposureState": "public"},
				Capability: map[string]string{"kind": "resource"},
			},
			want: true,
		},
		{
			name: "public resource gate skips internal",
			expr: `server.exposureState == "public" && capability.kind == "resource"`,
			ic: InvocationContext{
				Server:     map[string]string{"exposureState": "internal"},
				Capability: map[string]string{"kind": "resource"},
			},
			want: false,
		},
		{
			name: "caller agent + write_ prefix",
			expr: `caller.agent == "copilot-agent" && capability.name.startsWith("write_")`,
			ic: InvocationContext{
				Caller:     json.RawMessage(`{"agent":"copilot-agent","user":"alice"}`),
				Capability: map[string]string{"name": "write_file"},
			},
			want: true,
		},
		{
			name: "caller agent mismatch",
			expr: `caller.agent == "copilot-agent" && capability.name.startsWith("write_")`,
			ic: InvocationContext{
				Caller:     json.RawMessage(`{"agent":"other-agent"}`),
				Capability: map[string]string{"name": "write_file"},
			},
			want: false,
		},
		{
			name: "status filter",
			expr: `status == "denied"`,
			ic:   InvocationContext{Status: "denied"},
			want: true,
		},
		{
			name: "timestamp filter — after",
			expr: `at > timestamp("2026-01-01T00:00:00Z")`,
			ic:   InvocationContext{At: at},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prg, err := e.Compile(tc.expr)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			got, err := prg.Evaluate(ctx, tc.ic)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluate_MissingCallerKeyDoesNotMatch(t *testing.T) {
	// Indexing a missing key on a map<string,dyn> in CEL returns an error from
	// the eval engine; we surface that as (false, err) and our scheduler
	// treats it as "doesn't match" — never as a hard sweep failure.
	e := newEngine(t)
	prg, err := e.Compile(`caller.agent == "x"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, _ := prg.Evaluate(context.Background(), InvocationContext{
		Caller: json.RawMessage(`{}`),
	})
	if got {
		t.Fatalf("missing key should not match, got true")
	}
}

func TestEvaluate_EmptyCallerJSONIsSafe(t *testing.T) {
	e := newEngine(t)
	prg, err := e.Compile(`status == "ok"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := prg.Evaluate(context.Background(), InvocationContext{
		Status: "ok",
		Caller: nil,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !got {
		t.Fatal("expected true")
	}
}
