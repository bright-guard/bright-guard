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
func New() (*Engine, error) {
	env, err := cel.NewEnv(
		cel.Variable("caller", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("server", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("capability", cel.MapType(cel.StringType, cel.StringType)),
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
	out, _, err := p.prg.ContextEval(ctx, map[string]any{
		"caller":     caller,
		"server":     ic.Server,
		"capability": ic.Capability,
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
