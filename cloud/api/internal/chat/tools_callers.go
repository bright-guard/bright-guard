package chat

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

func (d *Dispatcher) registerCallersTools() {
	d.register(Tool{
		Name:        "list_callers",
		Description: "List MCP callers (distinct identities) seen in the org. Use this for 'who is calling X' or 'top callers' questions.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"range": map[string]any{
					"type":        "string",
					"description": "Time window as Nd or Nh (e.g. '7d', '24h'). Defaults to 7d.",
				},
				"flagged_new": map[string]any{
					"type":        "boolean",
					"description": "If true, only return callers flagged as recently-new.",
				},
				"q": map[string]any{
					"type":        "string",
					"description": "Substring match against caller label.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max rows (default 25, max 100).",
				},
			},
		}),
	}, d.handleListCallers)

	d.register(Tool{
		Name:        "get_caller",
		Description: "Get a caller's detail including top servers and recent invocations.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"caller_id"},
			"properties": map[string]any{
				"caller_id": map[string]any{"type": "string"},
			},
		}),
	}, d.handleGetCaller)
}

func (d *Dispatcher) handleListCallers(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		Range      string `json:"range"`
		FlaggedNew bool   `json:"flagged_new"`
		Q          string `json:"q"`
		Limit      int    `json:"limit"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	if in.Limit <= 0 || in.Limit > 100 {
		in.Limit = 25
	}
	from, to := parseRange(in.Range)
	f := store.CallerFilter{
		From:        from,
		To:          to,
		FlaggedOnly: in.FlaggedNew,
		Q:           in.Q,
		Limit:       in.Limit,
	}
	rows, _, totals, err := d.Callers.List(ctx, orgID, f)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, map[string]any{
			"id":              r.ID,
			"label":           r.Label,
			"firstSeenAt":     r.FirstSeenAt,
			"lastSeenAt":      r.LastSeenAt,
			"invocationCount": r.InvocationCount,
			"flaggedNew":      r.FlaggedNew,
		})
	}
	return map[string]any{
		"items":  items,
		"totals": totals,
		"window": map[string]any{"from": from, "to": to},
	}, nil
}

func (d *Dispatcher) handleGetCaller(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		CallerID string `json:"caller_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(in.CallerID)
	if err != nil {
		return nil, err
	}
	det, err := d.Callers.Get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	// Slim the recent invocations to keep the LLM context manageable.
	recent := make([]map[string]any, 0, len(det.RecentInvocations))
	for i, inv := range det.RecentInvocations {
		if i >= 25 {
			break
		}
		recent = append(recent, map[string]any{
			"at":             inv.At,
			"mcpServerId":    inv.MCPServerID,
			"capabilityKind": inv.CapabilityKind,
			"capabilityName": inv.CapabilityName,
			"status":         inv.Status,
		})
	}
	return map[string]any{
		"id":              det.ID,
		"label":           det.Label,
		"firstSeenAt":     det.FirstSeenAt,
		"lastSeenAt":      det.LastSeenAt,
		"invocationCount": det.InvocationCount,
		"flaggedNew":      det.FlaggedNew,
		"topServers":      det.TopServers,
		"recentInvocations": recent,
	}, nil
}
