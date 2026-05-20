// Package policy hosts the CEL compile + eval surface used by both the
// scheduler sweep and the simulate endpoint. Stays free of *pgxpool deps so
// it remains trivially unit-testable.
//
// We deliberately do NOT enable cel.ext.Strings() or similar add-ons: their
// regex helpers can be made to DoS via backtracking when fed tenant input.
// All evaluations run with a hard cost limit (see evalCostLimit) so a
// pathological policy can't burn unbounded CPU.
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

// evalCostLimit caps CEL evaluation cost on every Eval call. Each AST node
// roughly costs 1; field access roughly 1; the example expressions from the
// issue cost <10. 50k is generous for any reasonable rule and still bounds
// CPU on the sweep goroutine even if a customer pastes something nasty.
const evalCostLimit uint64 = 50_000

// Engine wraps a single, shared *cel.Env. The env is cheap to construct, but
// holding one Engine per process keeps boilerplate out of callers.
type Engine struct {
	env *cel.Env
}

// New builds a fresh Engine.
//
// Wave N+8 widened the env:
//   - `server` is now map<string, dyn> so non-string future fields (bool flags,
//     numeric ports) won't require a second migration. Today's values are still
//     strings (name, address, id, exposure_state) plus the legacy exposureState
//     alias so any policy authored against the old camelCase key keeps working.
//   - `caller` already exposed everything via dyn; in Wave N+8 we additionally
//     populate signature/label/flagged_new/acknowledged so UC9 policies have
//     first-class fields without the policy author needing to introspect the
//     raw caller payload.
//   - `request` is a new map<string, dyn> carrying request.now (UTC time of
//     evaluation). `at` is kept as-is for backwards compatibility.
//
// This env declaration MUST stay byte-for-byte identical to
// cloud/shim/cmd/shim/policy.go's sharedCELEnv().
func New() (*Engine, error) {
	env, err := cel.NewEnv(
		cel.Variable("caller", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("server", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("capability", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("at", cel.TimestampType),
		cel.Variable("status", cel.StringType),
	)
	if err != nil {
		return nil, fmt.Errorf("cel env: %w", err)
	}
	return &Engine{env: env}, nil
}

// PolicyProgram is a compiled, ready-to-eval CEL program.
type PolicyProgram struct {
	prg cel.Program
}

// Compile parses + type-checks + plans a CEL expression. The error string is
// safe to surface verbatim — cel-go already produces line/column markers.
func (e *Engine) Compile(expr string) (*PolicyProgram, error) {
	ast, iss := e.env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, errors.New(iss.Err().Error())
	}
	// Require bool output — anything else is a misconfiguration we should
	// reject at compile time, not at eval time.
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("policy must return bool, got %s", ast.OutputType())
	}
	prg, err := e.env.Program(ast,
		// Hard cost limit so tenant-supplied expressions can't DoS the sweep.
		cel.CostLimit(evalCostLimit),
	)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	return &PolicyProgram{prg: prg}, nil
}

// InvocationContext is the data fed to each Evaluate call.
//
// Server is map<string,string> in storage but lifted into a map[string]any
// for CEL so a future bool/numeric field doesn't break older policies. Caller
// is shipped as raw JSON (the same shape the shim sees) and decoded inline so
// each call gets a clean copy.
type InvocationContext struct {
	At         time.Time
	Status     string
	Caller     json.RawMessage
	Server     map[string]string
	Capability map[string]string
}

// Evaluate runs the compiled program with the given context. Returns the
// boolean result and any runtime error. Cost-limit exceeded surfaces as a
// regular error from cel-go.
func (p *PolicyProgram) Evaluate(ctx context.Context, ic InvocationContext) (bool, error) {
	if p == nil || p.prg == nil {
		return false, errors.New("nil program")
	}
	// Decode caller jsonb to a generic map so CEL can dot-index into it.
	// Empty / invalid JSON degrades to an empty map — a malformed observation
	// shouldn't break the whole sweep.
	caller := map[string]any{}
	if len(ic.Caller) > 0 {
		_ = json.Unmarshal(ic.Caller, &caller)
	}
	if ic.Server == nil {
		ic.Server = map[string]string{}
	}
	if ic.Capability == nil {
		ic.Capability = map[string]string{}
	}
	// Lift map<string,string> server values into a map<string,any> so the env
	// (now map<string,dyn>) accepts them. Same shape the shim feeds in.
	srv := make(map[string]any, len(ic.Server))
	for k, v := range ic.Server {
		srv[k] = v
	}
	now := ic.At
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out, _, err := p.prg.ContextEval(ctx, map[string]any{
		"caller":     caller,
		"server":     srv,
		"capability": ic.Capability,
		"request":    map[string]any{"now": now},
		"at":         ic.At,
		"status":     ic.Status,
	})
	if err != nil {
		return false, err
	}
	// Bool comparisons against types.True/False are the documented form.
	if out == types.True {
		return true, nil
	}
	if out == types.False {
		return false, nil
	}
	return false, nil
}
