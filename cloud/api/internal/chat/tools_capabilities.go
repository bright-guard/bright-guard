package chat

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

func (d *Dispatcher) registerCapabilitiesTools() {
	d.register(Tool{
		Name:        "list_capabilities",
		Description: "List capabilities (tools / resources / prompts) exposed by the org's MCP servers. Use this to answer questions like 'which servers have a delete_repo tool' or 'how many resources does jira-mcp expose'.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"server_id": map[string]any{
					"type":        "string",
					"description": "Optional MCP server UUID. Without this, capabilities across all of the org's servers are returned.",
				},
				"kind": map[string]any{
					"type":        "string",
					"description": "Optional capability kind filter.",
					"enum":        []string{"tool", "resource", "prompt"},
				},
				"name_contains": map[string]any{
					"type":        "string",
					"description": "Optional case-insensitive substring match against the capability name.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max rows to return (default 50, max 200).",
				},
			},
		}),
	}, d.handleListCapabilities)
}

func (d *Dispatcher) handleListCapabilities(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		ServerID     string `json:"server_id"`
		Kind         string `json:"kind"`
		NameContains string `json:"name_contains"`
		Limit        int    `json:"limit"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 200 {
		in.Limit = 200
	}
	f := store.CapabilityFilter{
		Kind:         in.Kind,
		NameContains: in.NameContains,
		Limit:        in.Limit,
	}
	if in.ServerID != "" {
		sid, err := uuid.Parse(in.ServerID)
		if err == nil {
			f.ServerID = &sid
		}
	}
	rows, err := d.Discovery.ListCapabilitiesForOrg(ctx, orgID, f)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, map[string]any{
			"id":          r.ID,
			"server_id":   r.ServerID,
			"server_name": r.ServerName,
			"kind":        r.Kind,
			"name":        r.Name,
			"description": r.Description,
			"disabled":    !r.Enabled,
		})
	}
	return map[string]any{
		"items": items,
		"total": len(items),
	}, nil
}
