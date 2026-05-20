package chat

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

func (d *Dispatcher) registerPoliciesTools() {
	d.register(Tool{
		Name:        "list_policies",
		Description: "List CEL policies for the user's org with their action (deny/warn), enabled flag, and recent match counts.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}),
	}, d.handleListPolicies)
}

func (d *Dispatcher) handleListPolicies(ctx context.Context, orgID uuid.UUID, _ json.RawMessage) (any, error) {
	policies, err := d.Policies.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	version, _, err := d.Policies.BundleFor(ctx, orgID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(policies))
	for _, p := range policies {
		items = append(items, map[string]any{
			"id":             p.ID,
			"name":           p.Name,
			"action":         p.Action,
			"enabled":        p.Enabled,
			"last24hMatches": p.Last24hMatches,
		})
	}
	return map[string]any{
		"items":               items,
		"policyBundleVersion": version,
	}, nil
}
