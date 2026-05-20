package chat

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type gatewayRow struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

func (d *Dispatcher) registerGatewaysTools() {
	d.register(Tool{
		Name:        "list_gateways",
		Description: "List gateways for the user's org. A gateway is the shim/proxy that fronts one or more MCP servers and reports observations back to Bright Guard.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"status": map[string]any{
					"type":        "string",
					"description": "Optional status filter.",
					"enum":        []string{"pending", "online", "offline", "revoked"},
				},
			},
		}),
	}, d.handleListGateways)
}

func (d *Dispatcher) handleListGateways(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		Status string `json:"status"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	gws, err := d.Gateways.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(in.Status))
	out := make([]gatewayRow, 0, len(gws))
	for _, g := range gws {
		if want != "" && g.Status != want {
			continue
		}
		out = append(out, gatewayRow{
			ID:         g.ID,
			Name:       g.Name,
			Status:     g.Status,
			LastSeenAt: g.LastSeenAt,
		})
	}
	return map[string]any{"items": out, "total": len(out)}, nil
}
