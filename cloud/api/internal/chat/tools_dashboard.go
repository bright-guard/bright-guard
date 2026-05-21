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

	d.register(Tool{
		Name:        "dashboard_timeseries",
		Description: "Daily time-series for one dashboard metric over a 7/30/90-day range. Use this to answer 'how has X trended' questions. For 'invocations', the value is the daily total across allowed+audited+denied.",
		InputSchema: schema(map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"metric"},
			"properties": map[string]any{
				"metric": map[string]any{
					"type":        "string",
					"description": "Which metric to plot.",
					"enum":        []string{"invocations", "denials", "new_callers", "new_servers", "public_exposure", "posture"},
				},
				"range": map[string]any{
					"type":        "string",
					"description": "Window length: 7d, 30d, or 90d. Defaults to 30d.",
					"enum":        []string{"7d", "30d", "90d"},
				},
			},
		}),
	}, d.handleDashboardTimeseries)
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

func (d *Dispatcher) handleDashboardTimeseries(ctx context.Context, orgID uuid.UUID, args json.RawMessage) (any, error) {
	var in struct {
		Metric string `json:"metric"`
		Range  string `json:"range"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &in)
	}
	allowed := map[string]bool{
		"invocations": true, "denials": true, "new_callers": true,
		"new_servers": true, "public_exposure": true, "posture": true,
	}
	if !allowed[in.Metric] {
		return map[string]any{"error": "unknown metric"}, nil
	}
	days := 30
	switch in.Range {
	case "7d":
		days = 7
	case "30d", "":
		days = 30
	case "90d":
		days = 90
	}
	now := time.Now().UTC()
	to := store.DayInUTC(now).Add(24 * time.Hour)
	from := to.Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := d.Dashboard.ListDaily(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	index := map[string]store.DailyMetric{}
	for _, r := range rows {
		index[r.Day.UTC().Format("2006-01-02")] = r
	}
	points := make([]map[string]any, 0, days)
	for i := 0; i < days; i++ {
		day := from.Add(time.Duration(i) * 24 * time.Hour).Format("2006-01-02")
		r := index[day]
		var value int64
		switch in.Metric {
		case "invocations":
			value = r.InvocationsAllowed + r.InvocationsAudited + r.InvocationsDenied
		case "denials":
			value = r.InvocationsDenied
		case "new_callers":
			value = r.NewCallers
		case "new_servers":
			value = r.NewServers
		case "public_exposure":
			value = r.PublicExposureCount
		case "posture":
			value = int64(r.PostureScore)
		}
		points = append(points, map[string]any{"day": day, "value": value})
	}
	rangeOut := in.Range
	if rangeOut == "" {
		rangeOut = "30d"
	}
	return map[string]any{
		"metric": in.Metric,
		"range":  rangeOut,
		"from":   from,
		"to":     to,
		"points": points,
	}, nil
}
