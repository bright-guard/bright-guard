package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const (
	metricsRollupLockKey  = "bg-metrics-rollup"
	metricsBackfillDays   = 90
	metricsGatewayOnlineW = 5 * time.Minute
)

// MetricsRollup periodically computes and upserts today's org_daily_metrics
// row for every org, and on startup backfills any missing days within the
// last 90.
type MetricsRollup struct {
	Connections *store.Connections // advisory lock only
	Dashboard   *store.Dashboard
	Interval    time.Duration

	// backfillOnce guards the startup backfill against double-runs.
	backfilled bool
}

// NewMetricsRollup builds a rollup sweeper with sensible defaults.
func NewMetricsRollup(conns *store.Connections, dash *store.Dashboard, interval time.Duration) *MetricsRollup {
	if interval <= 0 {
		interval = time.Hour
	}
	return &MetricsRollup{Connections: conns, Dashboard: dash, Interval: interval}
}

// Run loops until ctx is cancelled.
func (m *MetricsRollup) Run(ctx context.Context) {
	log.Printf("metrics rollup: starting interval=%s", m.Interval)
	t := time.NewTicker(m.Interval)
	defer t.Stop()
	m.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.tick(ctx)
		}
	}
}

func (m *MetricsRollup) tick(ctx context.Context) {
	start := time.Now()
	ok, err := m.Connections.TryAdvisoryLock(ctx, metricsRollupLockKey)
	if err != nil {
		log.Printf("metrics rollup: advisory lock check failed: %v", err)
		return
	}
	if !ok {
		return
	}
	orgs, err := m.Dashboard.ListOrgIDs(ctx)
	if err != nil {
		log.Printf("metrics rollup: list orgs failed: %v", err)
		return
	}
	now := time.Now().UTC()
	today := store.DayInUTC(now)
	wasBackfill := !m.backfilled
	backfilledDays := 0
	for _, id := range orgs {
		if err := ctx.Err(); err != nil {
			return
		}
		if wasBackfill {
			n, err := m.backfillOrg(ctx, id, today)
			if err != nil {
				log.Printf("metrics rollup: backfill %s: %v", id, err)
			}
			backfilledDays += n
		}
		if err := m.rollupToday(ctx, id, today); err != nil {
			log.Printf("metrics rollup: today %s: %v", id, err)
		}
	}
	if wasBackfill {
		log.Printf("metrics rollup: backfilled %d days for %d orgs", backfilledDays, len(orgs))
	}
	m.backfilled = true
	log.Printf("metrics rollup: tick ok orgs=%d duration=%s", len(orgs), time.Since(start))
}

// rollupToday computes today's metrics row for one org and upserts it.
func (m *MetricsRollup) rollupToday(ctx context.Context, orgID uuid.UUID, today time.Time) error {
	dayStart := today
	dayEnd := dayStart.Add(24 * time.Hour)

	inv, err := m.Dashboard.CountInvocations(ctx, orgID, dayStart, dayEnd)
	if err != nil {
		return err
	}
	_, newCallers, err := m.Dashboard.CountDistinctCallers(ctx, orgID, dayStart, dayEnd)
	if err != nil {
		return err
	}
	newServers, err := m.Dashboard.CountNewServers(ctx, orgID, dayStart, dayEnd)
	if err != nil {
		return err
	}
	snap, err := m.Dashboard.Snapshot(ctx, orgID, time.Now().UTC().Add(-metricsGatewayOnlineW))
	if err != nil {
		return err
	}
	return m.Dashboard.UpsertDaily(ctx, orgID, store.DailyMetric{
		Day:                 dayStart,
		InvocationsAllowed:  inv.Allowed,
		InvocationsAudited:  inv.Audited,
		InvocationsDenied:   inv.Denied,
		NewCallers:          newCallers,
		NewServers:          newServers,
		TotalServers:        snap.TotalServers,
		TotalCapabilities:   snap.TotalCapabilities,
		PublicExposureCount: snap.PublicExposureCount,
		GatewaysOnline:      snap.GatewaysOnline,
		PostureScore:        store.PostureFromSnapshot(snap),
	})
}

// backfillOrg fills in any missing rows in the trailing 90 days. For
// point-in-time fields we use a best-effort historical approximation:
//   - total_servers: count of servers with first_seen_at <= day-end
//   - total_capabilities, gateways_online: today's value (we don't carry
//     history for these; documented in module-level comments)
//   - public_exposure_count: today's value (same reason — exposure history
//     is not retained per-day).
//   - posture_score: recomputed from those proxies; close enough for a chart.
func (m *MetricsRollup) backfillOrg(ctx context.Context, orgID uuid.UUID, today time.Time) (int, error) {
	from := today.AddDate(0, 0, -metricsBackfillDays)
	missing, err := m.Dashboard.MissingDays(ctx, orgID, from, today.AddDate(0, 0, -1))
	if err != nil {
		return 0, err
	}
	if len(missing) == 0 {
		return 0, nil
	}
	// Bucket invocations + new-counter rows across the whole missing range
	// in one go, then index by day. (We may include some non-missing days in
	// the query; the per-day upsert below filters.)
	buckets, err := m.Dashboard.BackfillBuckets(ctx, orgID, from, today)
	if err != nil {
		return 0, err
	}
	byDay := map[string]store.DailyInvocationsBucket{}
	for _, b := range buckets {
		byDay[b.Day.UTC().Format("2006-01-02")] = b
	}
	// One snapshot for the proxy fields — same value applied to every
	// historical day. See module docstring.
	snap, err := m.Dashboard.Snapshot(ctx, orgID, time.Now().UTC().Add(-metricsGatewayOnlineW))
	if err != nil {
		return 0, err
	}
	for _, day := range missing {
		key := day.UTC().Format("2006-01-02")
		b := byDay[key]
		servers, err := m.Dashboard.ServerCountAt(ctx, orgID, day.Add(24*time.Hour))
		if err != nil {
			return 0, err
		}
		// Per-day posture using historical server count + today's other proxies.
		callersClean := snap.CallersTotal - snap.CallersFlaggedNew
		if callersClean < 0 {
			callersClean = 0
		}
		serversNotPublic := servers - snap.PublicExposureCount
		if serversNotPublic < 0 {
			serversNotPublic = 0
		}
		capsWithCover := int64(0)
		if snap.PoliciesEnabled > 0 {
			capsWithCover = snap.TotalCapabilities
		}
		ps := store.PostureScore(
			capsWithCover, snap.TotalCapabilities,
			callersClean, snap.CallersTotal,
			serversNotPublic, servers,
			snap.GatewaysOnline, snap.GatewaysTotal,
		)
		if err := m.Dashboard.UpsertDaily(ctx, orgID, store.DailyMetric{
			Day:                 day,
			InvocationsAllowed:  b.InvocationsAllowed,
			InvocationsAudited:  b.InvocationsAudited,
			InvocationsDenied:   b.InvocationsDenied,
			NewCallers:          b.NewCallers,
			NewServers:          b.NewServers,
			TotalServers:        servers,
			TotalCapabilities:   snap.TotalCapabilities,
			PublicExposureCount: snap.PublicExposureCount,
			GatewaysOnline:      snap.GatewaysOnline,
			PostureScore:        ps,
		}); err != nil {
			return 0, err
		}
	}
	return len(missing), nil
}
