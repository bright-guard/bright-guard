package chat

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
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

	d.register(Tool{
		Name:        "get_policy",
		Description: "Get one policy in detail: expression, action, enabled flag, current bundle version, and up to 10 of the most recent matched enforcement decisions (server, capability, caller, action).",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"policy_id"},
			"properties": map[string]any{
				"policy_id": map[string]any{
					"type":        "string",
					"description": "UUID of the policy.",
				},
			},
		}),
	}, d.handleGetPolicy)
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

func (d *Dispatcher) handleGetPolicy(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		PolicyID string `json:"policy_id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	pid, err := uuid.Parse(in.PolicyID)
	if err != nil {
		return nil, err
	}
	pol, err := d.Policies.Get(ctx, orgID, pid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return map[string]any{"error": "policy not found"}, nil
		}
		return nil, err
	}
	version, _, err := d.Policies.BundleFor(ctx, orgID)
	if err != nil {
		return nil, err
	}
	decisions, err := d.Policies.RecentDecisionsForPolicy(ctx, orgID, pid, 10)
	if err != nil {
		return nil, err
	}
	recent := make([]map[string]any, 0, len(decisions))
	for _, dr := range decisions {
		recent = append(recent, map[string]any{
			"invocation_id": dr.InvocationID,
			"decided_at":    dr.At,
			"server_id":     dr.ServerID,
			"server_name":   dr.ServerName,
			"capability":    dr.Capability,
			"caller_label":  dr.CallerLabel,
			"decision":      dr.Decision,
		})
	}
	return map[string]any{
		"id":                    pol.ID,
		"name":                  pol.Name,
		"description":           pol.Description,
		"action":                pol.Action,
		"expression":            pol.Expression,
		"enabled":               pol.Enabled,
		"current_bundle_version": version,
		"last_updated_at":       pol.UpdatedAt,
		"last24h_matches":       pol.Last24hMatches,
		"recent_decisions":      recent,
	}, nil
}
