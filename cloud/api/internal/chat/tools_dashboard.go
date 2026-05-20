package chat

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

func (d *Dispatcher) registerDashboardTools() {
	d.register(Tool{
		Name:        "dashboard_kpis",
		Description: "Executive-dashboard KPIs (posture, footprint, invocations, denials, public exposure, active callers) for a time window vs. the prior equal-length window.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"range": map[string]any{
					"type":        "string",
					"description": "Time window like '7d', '30d', '90d'. Defaults to 30d.",
				},
			},
		}),
	}, d.handleDashboardKPIs)
}

func (d *Dispatcher) handleDashboardKPIs(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		Range string `json:"range"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	from, to := parseRange(in.Range)
	if in.Range == "" {
		from = to.Add(-30 * 24 * time.Hour)
	}
	priorFrom := from.Add(-(to.Sub(from)))
	priorTo := from

	curInv, err := d.Dashboard.CountInvocations(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	priorInv, err := d.Dashboard.CountInvocations(ctx, orgID, priorFrom, priorTo)
	if err != nil {
		return nil, err
	}
	curCallers, curNewCallers, err := d.Dashboard.CountDistinctCallers(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	priorCallers, _, err := d.Dashboard.CountDistinctCallers(ctx, orgID, priorFrom, priorTo)
	if err != nil {
		return nil, err
	}
	snap, err := d.Dashboard.Snapshot(ctx, orgID, time.Now().UTC().Add(-5*time.Minute))
	if err != nil {
		return nil, err
	}
	posture := store.PostureFromSnapshot(snap)
	return map[string]any{
		"window": map[string]any{"from": from, "to": to},
		"posture": map[string]any{
			"score": posture,
			"max":   100,
		},
		"footprint": map[string]any{
			"totalServers":      snap.TotalServers,
			"totalCapabilities": snap.TotalCapabilities,
		},
		"invocations": map[string]any{
			"allowed":      curInv.Allowed,
			"audited":      curInv.Audited,
			"denied":       curInv.Denied,
			"total":        curInv.Total(),
			"priorTotal":   priorInv.Total(),
			"priorDenied":  priorInv.Denied,
		},
		"publicExposure": map[string]any{
			"current": snap.PublicExposureCount,
		},
		"activeCallers": map[string]any{
			"current":    curCallers,
			"prior":      priorCallers,
			"newInRange": curNewCallers,
		},
		"gateways": map[string]any{
			"total":  snap.GatewaysTotal,
			"online": snap.GatewaysOnline,
		},
		"policiesEnabled": snap.PoliciesEnabled,
	}, nil
}
