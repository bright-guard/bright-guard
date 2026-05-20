package chat

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

func (d *Dispatcher) registerActivityTools() {
	d.register(Tool{
		Name:        "query_activity",
		Description: "Search recent MCP invocations matching a set of filters. Use for 'show me denied calls', 'what did caller X do', etc.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"range":        map[string]any{"type": "string", "description": "Time window like '7d', '24h'."},
				"status":       map[string]any{"type": "string", "enum": []string{"ok", "error", "denied"}},
				"kind":         map[string]any{"type": "string", "enum": []string{"tool", "resource", "prompt"}},
				"server_id":    map[string]any{"type": "string", "description": "Restrict to one server (UUID)."},
				"capability":   map[string]any{"type": "string", "description": "Substring match against capability/server name."},
				"caller_id":    map[string]any{"type": "string", "description": "Restrict to one caller (UUID). (Filtered post-query; may be slow on large windows.)"},
				"limit":        map[string]any{"type": "integer", "description": "Max rows (default 50, max 200)."},
			},
		}),
	}, d.handleQueryActivity)

	d.register(Tool{
		Name:        "activity_aggregates",
		Description: "Top capabilities and top callers in a time window (counts).",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"range": map[string]any{"type": "string", "description": "Time window like '7d', '24h'. Defaults to 7d."},
			},
		}),
	}, d.handleActivityAggregates)
}

func (d *Dispatcher) handleQueryActivity(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		Range      string `json:"range"`
		Status     string `json:"status"`
		Kind       string `json:"kind"`
		ServerID   string `json:"server_id"`
		Capability string `json:"capability"`
		CallerID   string `json:"caller_id"`
		Limit      int    `json:"limit"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	if in.Limit <= 0 || in.Limit > 200 {
		in.Limit = 50
	}
	from, to := parseRange(in.Range)
	f := store.ActivityFilter{
		From:  from,
		To:    to,
		Q:     in.Capability,
		Limit: in.Limit,
	}
	if in.Status != "" {
		f.Statuses = []string{in.Status}
	}
	if in.Kind != "" {
		f.CapabilityKinds = []string{in.Kind}
	}
	if in.ServerID != "" {
		sid, err := uuid.Parse(in.ServerID)
		if err == nil {
			f.MCPServerID = &sid
		}
	}
	rows, _, totals, err := d.Activity.List(ctx, orgID, f)
	if err != nil {
		return nil, err
	}
	// Optional in-memory caller_id post-filter. Each org_callers row has a
	// stored caller JSON; we re-fetch + signature-match against the row's
	// signature. Cheap because Activity.List caps at limit.
	if in.CallerID != "" {
		cid, err := uuid.Parse(in.CallerID)
		if err == nil {
			det, err := d.Callers.Get(ctx, orgID, cid)
			if err == nil {
				want := store.SignatureFor(det.Caller)
				kept := rows[:0]
				for _, r := range rows {
					if store.SignatureFor(r.Caller) == want {
						kept = append(kept, r)
					}
				}
				rows = kept
			}
		}
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, map[string]any{
			"id":             r.ID,
			"at":             r.At,
			"serverName":     r.MCPServer.Name,
			"capabilityKind": r.CapabilityKind,
			"capabilityName": r.CapabilityName,
			"status":         r.Status,
			"latencyMs":      r.LatencyMs,
			"caller":         r.Caller,
			"decisions":      r.Decisions,
		})
	}
	return map[string]any{
		"items":  items,
		"totals": totals,
		"window": map[string]any{"from": from, "to": to},
	}, nil
}

func (d *Dispatcher) handleActivityAggregates(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		Range string `json:"range"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	from, to := parseRange(in.Range)
	sum, err := d.Activity.Summary(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"window":           map[string]any{"from": from, "to": to},
		"totalInvocations": sum.TotalInvocations,
		"byStatus":         sum.ByStatus,
		"byCapabilityKind": sum.ByCapabilityKind,
		"topCapabilities":  sum.TopCapabilities,
		"topCallers":       sum.TopCallers,
	}, nil
}
