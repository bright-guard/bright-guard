package policy

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
)

// SimulationMaxRows caps how many invocations a single Simulate call evaluates.
// Picked so a 30d range over the busiest demo orgs (~few thousand inv/day) still
// returns within ~1s. The handler logs + truncates if the range would exceed it.
const SimulationMaxRows = 50_000

// SimulationSampleSize is the number of "would-be-blocked" rows returned in the
// samples list for an admin to spot-check. Small enough to render in the UI,
// large enough to show meaningful variety.
const SimulationSampleSize = 10

// SimulationBreakdownTop is the cardinality of each breakdown list (by server,
// by capability, by caller).
const SimulationBreakdownTop = 10

// SimulationInput is one invocation projected for the simulator. Wraps the
// engine's InvocationContext with the identifiers + display fields the result
// payload needs.
//
// We keep this separate from the store's InvocationContext so the simulator
// remains free of any DB or store-package dependency.
type SimulationInput struct {
	InvocationID  uuid.UUID
	At            time.Time
	ServerName    string
	CapabilityKey string // e.g. "tool/create_issue" — what we group capability breakdowns by
	CallerKey     string // signature || label || agent — what we group caller breakdowns by
	IC            InvocationContext
}

// SimulationSample is one representative invocation that the policy would have
// blocked.
type SimulationSample struct {
	InvocationID   uuid.UUID `json:"invocationId"`
	Capability     string    `json:"capability"`
	Server         string    `json:"server"`
	Caller         string    `json:"caller"`
	Timestamp      time.Time `json:"timestamp"`
}

// SimulationBucket is one row in a breakdown list.
type SimulationBucket struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// SimulationResult is what the simulator returns to the handler. Mirrors the
// wire shape declared in the UC5 spec — JSON tags keep the API stable.
type SimulationResult struct {
	TotalInvocations      int                `json:"totalInvocations"`
	WouldBlockCount       int                `json:"wouldBlockCount"`
	WouldWarnCount        int                `json:"wouldWarnCount"`
	BreakdownByServer     []SimulationBucket `json:"breakdownByServer"`
	BreakdownByCapability []SimulationBucket `json:"breakdownByCapability"`
	BreakdownByCaller     []SimulationBucket `json:"breakdownByCaller"`
	Samples               []SimulationSample `json:"samples"`
	// Truncated reports whether the loader hit SimulationMaxRows and stopped
	// short of the full requested range. The UI uses this to render a banner.
	Truncated bool `json:"truncated"`
	// DurationMs measures end-to-end simulator time for a given expression so
	// the UI can warn when a large org's range pushes against the budget.
	DurationMs int64 `json:"durationMs"`
}

// SimulationAction enumerates what an admin is proposing to do with the policy
// — used to decide whether matches go in WouldBlockCount or WouldWarnCount.
type SimulationAction string

const (
	SimulationActionDeny SimulationAction = "deny"
	SimulationActionWarn SimulationAction = "warn"
)

// Simulate evaluates `program` against `inputs` and aggregates the results.
//
// Eval errors per row are treated as non-matches (mirrors the sweep semantics:
// missing keys + cost-limit-exceeded shouldn't cause the whole simulation to
// fail loudly).
func Simulate(
	ctx context.Context,
	program *PolicyProgram,
	action SimulationAction,
	inputs []SimulationInput,
) SimulationResult {
	start := time.Now()
	res := SimulationResult{
		TotalInvocations:      len(inputs),
		BreakdownByServer:     []SimulationBucket{},
		BreakdownByCapability: []SimulationBucket{},
		BreakdownByCaller:     []SimulationBucket{},
		Samples:               []SimulationSample{},
	}
	if program == nil {
		res.DurationMs = time.Since(start).Milliseconds()
		return res
	}
	byServer := map[string]int{}
	byCapability := map[string]int{}
	byCaller := map[string]int{}
	for _, in := range inputs {
		if err := ctx.Err(); err != nil {
			break
		}
		matched, err := program.Evaluate(ctx, in.IC)
		if err != nil || !matched {
			continue
		}
		switch action {
		case SimulationActionWarn:
			res.WouldWarnCount++
		default:
			res.WouldBlockCount++
		}
		byServer[in.ServerName]++
		byCapability[in.CapabilityKey]++
		caller := in.CallerKey
		if caller == "" {
			caller = "(unknown)"
		}
		byCaller[caller]++
		if len(res.Samples) < SimulationSampleSize {
			res.Samples = append(res.Samples, SimulationSample{
				InvocationID: in.InvocationID,
				Capability:   in.CapabilityKey,
				Server:       in.ServerName,
				Caller:       caller,
				Timestamp:    in.At,
			})
		}
	}
	res.BreakdownByServer = topBuckets(byServer, SimulationBreakdownTop)
	res.BreakdownByCapability = topBuckets(byCapability, SimulationBreakdownTop)
	res.BreakdownByCaller = topBuckets(byCaller, SimulationBreakdownTop)
	res.DurationMs = time.Since(start).Milliseconds()
	return res
}

// topBuckets reduces a histogram map to the top `n` buckets by count, ties
// broken alphabetically so output is deterministic across runs.
func topBuckets(m map[string]int, n int) []SimulationBucket {
	out := make([]SimulationBucket, 0, len(m))
	for k, v := range m {
		out = append(out, SimulationBucket{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
