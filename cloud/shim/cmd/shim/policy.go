package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

// evalCostLimit MUST stay identical to cloud/api/internal/policy.evalCostLimit.
// Same expression → same evaluation budget → same verdict server- and shim-side.
const evalCostLimit uint64 = 50_000

// celEnv is shared by every policy program. The variable declarations MUST
// match cloud/api/internal/policy.New() byte-for-byte:
//
//	cel.Variable("caller",     cel.MapType(cel.StringType, cel.DynType))
//	cel.Variable("server",     cel.MapType(cel.StringType, cel.DynType))
//	cel.Variable("capability", cel.MapType(cel.StringType, cel.StringType))
//	cel.Variable("request",    cel.MapType(cel.StringType, cel.DynType))
//	cel.Variable("workload",   cel.MapType(cel.StringType, cel.DynType))
//	cel.Variable("network",    cel.MapType(cel.StringType, cel.DynType))
//	cel.Variable("at",         cel.TimestampType)
//	cel.Variable("status",     cel.StringType)
//
// No ext.Strings(), no other extensions — same environment as the server.
// Wave N+8 widened `server` to dyn and added `request` so the UC8/UC9 policy
// templates can read server.exposure_state and request.now without the env
// authoring layer rejecting unknown fields.
// Wave N+9 added `workload` (UC6: host/cluster/namespace/agent_class) and
// `network` (UC7: subnet/vpc/zone/caller_ip). Missing values surface as ""
// so a policy never errors on absent context.
var (
	celEnvOnce sync.Once
	celEnvVal  *cel.Env
	celEnvErr  error
)

func sharedCELEnv() (*cel.Env, error) {
	celEnvOnce.Do(func() {
		celEnvVal, celEnvErr = cel.NewEnv(
			cel.Variable("caller", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("server", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("capability", cel.MapType(cel.StringType, cel.StringType)),
			cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("workload", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("network", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("at", cel.TimestampType),
			cel.Variable("status", cel.StringType),
		)
	})
	return celEnvVal, celEnvErr
}

// compiledPolicy is a ready-to-eval CEL program plus its metadata. Cached for
// the lifetime of a bundle version; recompiled when the bundle bumps.
type compiledPolicy struct {
	id     string
	name   string
	action string
	prg    cel.Program
}

func compilePolicy(p bundlePolicyWire) (compiledPolicy, error) {
	env, err := sharedCELEnv()
	if err != nil {
		return compiledPolicy{}, fmt.Errorf("cel env: %w", err)
	}
	ast, iss := env.Compile(p.Expression)
	if iss != nil && iss.Err() != nil {
		return compiledPolicy{}, errors.New(iss.Err().Error())
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return compiledPolicy{}, fmt.Errorf("policy must return bool, got %s", ast.OutputType())
	}
	prg, err := env.Program(ast, cel.CostLimit(evalCostLimit))
	if err != nil {
		return compiledPolicy{}, fmt.Errorf("plan: %w", err)
	}
	return compiledPolicy{
		id:     p.ID,
		name:   p.Name,
		action: p.Action,
		prg:    prg,
	}, nil
}

// evalContext is the snapshot fed to each policy program per fake invocation.
// server is map[string]any (CEL dyn) so we can hold strings (name, address,
// exposure_state, id) without the env rejecting non-string future values.
//
// workload + network (Wave N+9) carry the per-invocation subject and network
// context — keys are the documented sub-fields (workload.host / .cluster /
// .namespace / .agent_class and network.subnet / .vpc / .zone / .caller_ip);
// the emit path pre-fills them with "" when absent so policy programs never
// error on missing keys.
type evalContext struct {
	caller     map[string]any
	server     map[string]any
	capability map[string]string
	workload   map[string]any
	network    map[string]any
	at         time.Time
	status     string
}

// evaluatePolicies runs each program against the given context and returns the
// matched decisions. Programs that error (missing key, cost-limit, etc.) are
// treated as non-matches — same posture as the server-side sweep.
func evaluatePolicies(progs []compiledPolicy, ec evalContext) []obsDecision {
	if len(progs) == 0 {
		return nil
	}
	out := make([]obsDecision, 0, len(progs))
	for _, c := range progs {
		matched, err := evalOne(c, ec)
		if err != nil {
			// Non-match on eval error keeps behavior aligned with the server.
			continue
		}
		if !matched {
			continue
		}
		out = append(out, obsDecision{
			PolicyID: c.id,
			Action:   c.action,
			Matched:  true,
		})
	}
	return out
}

func evalOne(c compiledPolicy, ec evalContext) (bool, error) {
	caller := ec.caller
	if caller == nil {
		caller = map[string]any{}
	}
	srv := ec.server
	if srv == nil {
		srv = map[string]any{}
	}
	cap := ec.capability
	if cap == nil {
		cap = map[string]string{}
	}
	workload := normaliseScope(ec.workload, evalWorkloadKeys)
	network := normaliseScope(ec.network, evalNetworkKeys)
	now := ec.at
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out, _, err := c.prg.ContextEval(context.Background(), map[string]any{
		"caller":     caller,
		"server":     srv,
		"capability": cap,
		"request":    map[string]any{"now": now},
		"workload":   workload,
		"network":    network,
		"at":         ec.at,
		"status":     ec.status,
	})
	if err != nil {
		return false, err
	}
	if out == types.True {
		return true, nil
	}
	if out == types.False {
		return false, nil
	}
	return false, nil
}

// evalWorkloadKeys / evalNetworkKeys are the documented sub-fields for the
// workload and network scopes. The eval path pre-fills each with "" so a
// policy like `workload.cluster == "prod"` resolves to "" instead of
// erroring on rows where the shim didn't synthesise context.
var evalWorkloadKeys = []string{"host", "cluster", "namespace", "agent_class"}
var evalNetworkKeys = []string{"subnet", "vpc", "zone", "caller_ip"}

func normaliseScope(src map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if v, ok := src[k]; ok && v != nil {
			out[k] = v
		} else {
			out[k] = ""
		}
	}
	return out
}
