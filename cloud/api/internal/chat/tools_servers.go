package chat

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type serverRow struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Address         string    `json:"address"`
	ExposureState   string    `json:"exposureState"`
	LastSeenAt      time.Time `json:"lastSeenAt"`
	CapabilityCount int       `json:"capabilityCount"`
	GatewayName     string    `json:"gatewayName,omitempty"`
	ConnectionName  string    `json:"connectionName,omitempty"`
}

func (d *Dispatcher) registerServersTools() {
	d.register(Tool{
		Name:        "list_mcp_servers",
		Description: "List MCP servers in the user's org. Use this to answer questions about what servers exist, which are publicly exposed, capability counts, and last-seen times.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"filter": map[string]any{
					"type":        "string",
					"description": "Optional case-insensitive substring match against server name.",
				},
				"exposure_state": map[string]any{
					"type":        "string",
					"description": "Optional exposure-state filter.",
					"enum":        []string{"public", "cloud_internal", "internal", "unreachable", "unknown"},
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Max rows to return (default 25, max 100).",
				},
			},
		}),
	}, d.handleListServers)

	d.register(Tool{
		Name:        "get_mcp_server",
		Description: "Get one MCP server in detail: capabilities + recent invocations. Use this after list_mcp_servers when the user asks about a specific server.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"server_id"},
			"properties": map[string]any{
				"server_id": map[string]any{
					"type":        "string",
					"description": "UUID of the MCP server.",
				},
			},
		}),
	}, d.handleGetServer)

	d.register(Tool{
		Name:        "exposure_summary",
		Description: "Counts of MCP servers by exposure state (public, cloud_internal, internal, unreachable, unknown).",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}),
	}, d.handleExposureSummary)
}

func (d *Dispatcher) handleListServers(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		Filter        string `json:"filter"`
		ExposureState string `json:"exposure_state"`
		Limit         int    `json:"limit"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	if in.Limit <= 0 || in.Limit > 100 {
		in.Limit = 25
	}
	servers, err := d.Discovery.ListServers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	filter := strings.ToLower(strings.TrimSpace(in.Filter))
	out := make([]serverRow, 0, len(servers))
	for _, s := range servers {
		if filter != "" && !strings.Contains(strings.ToLower(s.Name), filter) {
			continue
		}
		if in.ExposureState != "" && s.ExposureState != in.ExposureState {
			continue
		}
		out = append(out, serverRow{
			ID:              s.ID,
			Name:            s.Name,
			Address:         s.Address,
			ExposureState:   s.ExposureState,
			LastSeenAt:      s.LastSeenAt,
			CapabilityCount: s.CapabilityCount,
			GatewayName:     s.GatewayName,
			ConnectionName:  s.ConnectionName,
		})
		if len(out) >= in.Limit {
			break
		}
	}
	return map[string]any{
		"items": out,
		"total": len(out),
	}, nil
}

func (d *Dispatcher) handleGetServer(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		ServerID string `json:"server_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	sid, err := uuid.Parse(in.ServerID)
	if err != nil {
		return nil, err
	}
	det, err := d.Discovery.GetServerDetail(ctx, orgID, sid)
	if err != nil {
		return nil, err
	}
	caps := make([]map[string]any, 0, len(det.Capabilities))
	for _, c := range det.Capabilities {
		caps = append(caps, map[string]any{
			"id":          c.ID,
			"kind":        c.Kind,
			"name":        c.Name,
			"description": c.Description,
			"enabled":     c.Enabled,
		})
	}
	// Recent invocations: trim the heavy fields for the LLM context.
	recent := make([]map[string]any, 0, len(det.Invocations))
	for i, inv := range det.Invocations {
		if i >= 20 {
			break
		}
		recent = append(recent, map[string]any{
			"at":             inv.At,
			"capabilityKind": inv.CapabilityKind,
			"capabilityName": inv.CapabilityName,
			"status":         inv.Status,
			"latencyMs":      inv.LatencyMs,
			"caller":         inv.Caller,
		})
	}
	return map[string]any{
		"id":              det.ID,
		"name":            det.Name,
		"address":         det.Address,
		"transport":       det.Transport,
		"version":         det.Version,
		"exposureState":   det.ExposureState,
		"exposureReason":  det.ExposureReason,
		"gatewayName":     det.GatewayName,
		"connectionName":  det.ConnectionName,
		"firstSeenAt":     det.FirstSeenAt,
		"lastSeenAt":      det.LastSeenAt,
		"capabilityCount": len(caps),
		"capabilities":    caps,
		"recentInvocations": recent,
	}, nil
}

func (d *Dispatcher) handleExposureSummary(ctx context.Context, orgID uuid.UUID, _ json.RawMessage) (any, error) {
	counts, err := d.Discovery.CountExposuresByState(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"counts": counts}, nil
}
