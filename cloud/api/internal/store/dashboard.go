package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dashboard owns reads against org_daily_metrics + a couple of point-in-time
// rollups used by the executive landing page.
type Dashboard struct {
	Pool *pgxpool.Pool
}

// DailyMetric is one row of org_daily_metrics.
type DailyMetric struct {
	Day                 time.Time `json:"day"`
	InvocationsAllowed  int64     `json:"invocationsAllowed"`
	InvocationsAudited  int64     `json:"invocationsAudited"`
	InvocationsDenied   int64     `json:"invocationsDenied"`
	NewCallers          int64     `json:"newCallers"`
	NewServers          int64     `json:"newServers"`
	TotalServers        int64     `json:"totalServers"`
	TotalCapabilities   int64     `json:"totalCapabilities"`
	PublicExposureCount int64     `json:"publicExposureCount"`
	GatewaysOnline      int64     `json:"gatewaysOnline"`
	PostureScore        int       `json:"postureScore"`
}

// ListDaily returns daily metrics rows for the org in [from, to) ordered by day asc.
func (d *Dashboard) ListDaily(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]DailyMetric, error) {
	const q = `
		select day, invocations_allowed, invocations_audited, invocations_denied,
		       new_callers, new_servers, total_servers, total_capabilities,
		       public_exposure_count, gateways_online, posture_score
		from org_daily_metrics
		where org_id = $1 and day >= $2 and day < $3
		order by day asc`
	rows, err := d.Pool.Query(ctx, q, orgID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DailyMetric, 0, 32)
	for rows.Next() {
		var m DailyMetric
		if err := rows.Scan(&m.Day,
			&m.InvocationsAllowed, &m.InvocationsAudited, &m.InvocationsDenied,
			&m.NewCallers, &m.NewServers,
			&m.TotalServers, &m.TotalCapabilities,
			&m.PublicExposureCount, &m.GatewaysOnline, &m.PostureScore,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CalloutCounts is the payload behind the "risk callouts" section: each count
// links the UI to a filtered list page.
type CalloutCounts struct {
	PublicExposureServers int64 `json:"publicExposureServers"`
	FlaggedNewCallers     int64 `json:"flaggedNewCallers"`
	CapabilitiesNoPolicy  int64 `json:"capabilitiesNoPolicy"`
}

// Callouts gathers the three risk indicators that drive the dashboard
// callouts. Each query is fast and indexed; we run them serially since each
// org's totals are small.
func (d *Dashboard) Callouts(ctx context.Context, orgID uuid.UUID) (CalloutCounts, error) {
	var c CalloutCounts
	row := d.Pool.QueryRow(ctx, `select count(*) from mcp_servers where org_id=$1 and exposure_state='public'`, orgID)
	if err := row.Scan(&c.PublicExposureServers); err != nil {
		return c, err
	}
	row = d.Pool.QueryRow(ctx, `select count(*) from org_callers where org_id=$1 and flagged_new=true`, orgID)
	if err := row.Scan(&c.FlaggedNewCallers); err != nil {
		return c, err
	}
	// "No policy coverage" = enabled capabilities whose server has no enabled
	// policy in the org. Cheap approximation: when the org has zero enabled
	// policies, every enabled capability counts. Otherwise zero — once a
	// single policy exists every capability is at least nominally evaluated
	// by it. The frontend already disambiguates per-capability via /policies.
	var policyCount int64
	if err := d.Pool.QueryRow(ctx, `select count(*) from policies where org_id=$1 and enabled=true`, orgID).Scan(&policyCount); err != nil {
		return c, err
	}
	if policyCount == 0 {
		if err := d.Pool.QueryRow(ctx, `
			select count(*) from mcp_capabilities c
			join mcp_servers s on s.id = c.mcp_server_id
			where s.org_id=$1 and c.enabled=true`, orgID).Scan(&c.CapabilitiesNoPolicy); err != nil {
			return c, err
		}
	}
	return c, nil
}

// DashTopCapability is one capability-name+count pair for the highlights section.
type DashTopCapability struct {
	CapabilityKind string `json:"capabilityKind"`
	CapabilityName string `json:"capabilityName"`
	ServerName     string `json:"serverName"`
	Count          int64  `json:"count"`
}

// DashTopCaller is one caller signature + count + caller json for the highlights.
type DashTopCaller struct {
	Signature string          `json:"signature"`
	Label     string          `json:"label"`
	Caller    json.RawMessage `json:"caller"`
	Count     int64           `json:"count"`
}

// RecentDenied is one denied invocation summarized for the highlights list.
type RecentDenied struct {
	ID             uuid.UUID       `json:"id"`
	At             time.Time       `json:"at"`
	ServerName     string          `json:"serverName"`
	CapabilityKind string          `json:"capabilityKind"`
	CapabilityName string          `json:"capabilityName"`
	Caller         json.RawMessage `json:"caller"`
}

// Highlights returns top-5 capabilities, top-5 callers, and the 5 most-recent
// denied invocations in the [from, to) range.
func (d *Dashboard) Highlights(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]DashTopCapability, []DashTopCaller, []RecentDenied, error) {
	caps, err := d.topCapabilities(ctx, orgID, from, to)
	if err != nil {
		return nil, nil, nil, err
	}
	callers, err := d.topCallers(ctx, orgID, from, to)
	if err != nil {
		return nil, nil, nil, err
	}
	denied, err := d.recentDenied(ctx, orgID, from, to)
	if err != nil {
		return nil, nil, nil, err
	}
	return caps, callers, denied, nil
}

func (d *Dashboard) topCapabilities(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]DashTopCapability, error) {
	const q = `
		select i.capability_kind, i.capability_name, s.name, count(*) c
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where i.org_id=$1 and i.at >= $2 and i.at < $3
		group by i.capability_kind, i.capability_name, s.name
		order by c desc, i.capability_kind, i.capability_name
		limit 5`
	rows, err := d.Pool.Query(ctx, q, orgID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DashTopCapability{}
	for rows.Next() {
		var tc DashTopCapability
		if err := rows.Scan(&tc.CapabilityKind, &tc.CapabilityName, &tc.ServerName, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (d *Dashboard) topCallers(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]DashTopCaller, error) {
	const q = `
		select i.caller, count(*) c
		from mcp_invocations i
		where i.org_id=$1 and i.at >= $2 and i.at < $3
		group by i.caller::text, i.caller
		order by c desc
		limit 5`
	rows, err := d.Pool.Query(ctx, q, orgID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DashTopCaller{}
	for rows.Next() {
		var raw json.RawMessage
		var c int64
		if err := rows.Scan(&raw, &c); err != nil {
			return nil, err
		}
		sig := SignatureFor(raw)
		out = append(out, DashTopCaller{
			Signature: sig,
			Label:     LabelFor(raw, sig),
			Caller:    raw,
			Count:     c,
		})
	}
	return out, rows.Err()
}

func (d *Dashboard) recentDenied(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]RecentDenied, error) {
	const q = `
		select i.id, i.at, s.name, i.capability_kind, i.capability_name, i.caller
		from mcp_invocations i
		join mcp_servers s on s.id = i.mcp_server_id
		where i.org_id=$1 and i.at >= $2 and i.at < $3 and i.status='denied'
		order by i.at desc
		limit 5`
	rows, err := d.Pool.Query(ctx, q, orgID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecentDenied{}
	for rows.Next() {
		var r RecentDenied
		if err := rows.Scan(&r.ID, &r.At, &r.ServerName, &r.CapabilityKind, &r.CapabilityName, &r.Caller); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CurrentSnapshot is the "today" point-in-time values needed both by the rollup
// (for upserting today's row) and by the KPI handler (so the live tile values
// reflect right-now state, not the most recent rollup which may be hours old).
type CurrentSnapshot struct {
	TotalServers          int64 `json:"totalServers"`
	TotalCapabilities     int64 `json:"totalCapabilities"`
	PublicExposureCount   int64 `json:"publicExposureCount"`
	GatewaysOnline        int64 `json:"gatewaysOnline"`
	CapabilitiesWithCover int64 `json:"capabilitiesWithCover"`
	CallersFlaggedNew     int64 `json:"callersFlaggedNew"`
	CallersTotal          int64 `json:"callersTotal"`
	GatewaysTotal         int64 `json:"gatewaysTotal"`
	PoliciesEnabled       int64 `json:"policiesEnabled"`
}

// Snapshot computes the org's current point-in-time counts in a single
// roundtrip via a UNION of cheap aggregates.
func (d *Dashboard) Snapshot(ctx context.Context, orgID uuid.UUID, onlineSince time.Time) (CurrentSnapshot, error) {
	var s CurrentSnapshot
	const q = `
		with
		s as (select count(*) c, count(*) filter (where exposure_state='public') pe from mcp_servers where org_id=$1),
		c as (select count(*) c from mcp_capabilities cap join mcp_servers ms on ms.id=cap.mcp_server_id where ms.org_id=$1),
		g as (
		  select count(*) c,
		         count(*) filter (where last_seen_at is not null and last_seen_at > $2 and status='online') as online
		    from gateways where org_id=$1
		),
		oc as (
		  select count(*) c, count(*) filter (where flagged_new=true) flagged
		    from org_callers where org_id=$1
		),
		p as (select count(*) c from policies where org_id=$1 and enabled=true)
		select s.c, s.pe, c.c, g.c, g.online, oc.c, oc.flagged, p.c from s, c, g, oc, p`
	if err := d.Pool.QueryRow(ctx, q, orgID, onlineSince).Scan(
		&s.TotalServers, &s.PublicExposureCount,
		&s.TotalCapabilities,
		&s.GatewaysTotal, &s.GatewaysOnline,
		&s.CallersTotal, &s.CallersFlaggedNew,
		&s.PoliciesEnabled,
	); err != nil {
		return s, err
	}
	return s, nil
}

// InvocationCounts is the (allowed, audited, denied) breakdown for a window.
type InvocationCounts struct {
	Allowed int64 `json:"allowed"`
	Audited int64 `json:"audited"`
	Denied  int64 `json:"denied"`
}

// Total returns sum of the three counters; convenient for KPI math.
func (c InvocationCounts) Total() int64 { return c.Allowed + c.Audited + c.Denied }

// CountInvocations returns the bucketed invocation count in [from, to) for the
// org. "audited" = status='ok' with any matched warn-action policy decision.
// "allowed" = status='ok' without any such decision. "denied" = status='denied'.
func (d *Dashboard) CountInvocations(ctx context.Context, orgID uuid.UUID, from, to time.Time) (InvocationCounts, error) {
	var c InvocationCounts
	const q = `
		with audited_ids as (
		  select distinct d.invocation_id
		  from mcp_invocation_decisions d
		  join policies p on p.id = d.policy_id
		  where p.org_id=$1 and d.matched=true and p.action='warn'
		)
		select
		  count(*) filter (where i.status='ok' and i.id not in (select invocation_id from audited_ids)) as allowed,
		  count(*) filter (where i.status='ok' and i.id     in (select invocation_id from audited_ids)) as audited,
		  count(*) filter (where i.status='denied')                                                     as denied
		from mcp_invocations i
		where i.org_id=$1 and i.at >= $2 and i.at < $3`
	if err := d.Pool.QueryRow(ctx, q, orgID, from.UTC(), to.UTC()).Scan(&c.Allowed, &c.Audited, &c.Denied); err != nil {
		return c, err
	}
	return c, nil
}

// CountDistinctCallers returns (distinct, new) callers active in [from, to).
// "new" means signatures whose first_seen_at is also inside the window — using
// org_callers if present, falling back to invocations for orgs without any
// caller rows yet.
func (d *Dashboard) CountDistinctCallers(ctx context.Context, orgID uuid.UUID, from, to time.Time) (distinct int64, newCount int64, _ error) {
	const q = `
		select
		  count(*) filter (where last_seen_at >= $2 and last_seen_at < $3) as distinct_callers,
		  count(*) filter (where first_seen_at >= $2 and first_seen_at < $3) as new_callers
		from org_callers where org_id=$1`
	if err := d.Pool.QueryRow(ctx, q, orgID, from.UTC(), to.UTC()).Scan(&distinct, &newCount); err != nil {
		return 0, 0, err
	}
	return distinct, newCount, nil
}

// CountNewServers returns count of servers whose first_seen_at is in [from, to).
func (d *Dashboard) CountNewServers(ctx context.Context, orgID uuid.UUID, from, to time.Time) (int64, error) {
	var c int64
	const q = `select count(*) from mcp_servers where org_id=$1 and first_seen_at >= $2 and first_seen_at < $3`
	err := d.Pool.QueryRow(ctx, q, orgID, from.UTC(), to.UTC()).Scan(&c)
	return c, err
}

// ListOrgIDs returns every org id. Used by the rollup sweep, which iterates
// all orgs per tick.
func (d *Dashboard) ListOrgIDs(ctx context.Context) ([]uuid.UUID, error) {
	const q = `select id from orgs order by id`
	rows, err := d.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MissingDays returns the set of dates in [from, to] (inclusive on both ends)
// for which the org does not yet have a metrics row. Used by backfill.
func (d *Dashboard) MissingDays(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]time.Time, error) {
	const q = `
		with all_days as (
		  select generate_series($2::date, $3::date, '1 day'::interval)::date as day
		)
		select all_days.day from all_days
		left join org_daily_metrics m on m.org_id=$1 and m.day = all_days.day
		where m.day is null
		order by all_days.day`
	rows, err := d.Pool.Query(ctx, q, orgID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []time.Time{}
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DailyInvocationsBucket is one day's pre-aggregated invocation counters.
type DailyInvocationsBucket struct {
	Day                time.Time
	InvocationsAllowed int64
	InvocationsAudited int64
	InvocationsDenied  int64
	NewCallers         int64
	NewServers         int64
}

// BackfillBuckets pulls aggregated counters per UTC day in [from, to] from the
// raw tables. Used by the rollup's startup backfill.
func (d *Dashboard) BackfillBuckets(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]DailyInvocationsBucket, error) {
	const q = `
		with audited as (
		  select distinct d.invocation_id from mcp_invocation_decisions d
		  join policies p on p.id = d.policy_id
		  where p.org_id=$1 and d.matched=true and p.action='warn'
		),
		inv as (
		  select (i.at at time zone 'UTC')::date as day,
		         count(*) filter (where i.status='ok'     and i.id not in (select invocation_id from audited)) as allowed,
		         count(*) filter (where i.status='ok'     and i.id     in (select invocation_id from audited)) as audited,
		         count(*) filter (where i.status='denied')                                                     as denied
		  from mcp_invocations i
		  where i.org_id=$1 and i.at >= $2 and i.at < $3
		  group by 1
		),
		c as (
		  select (first_seen_at at time zone 'UTC')::date as day, count(*) n
		  from org_callers where org_id=$1 and first_seen_at >= $2 and first_seen_at < $3
		  group by 1
		),
		srv as (
		  select (first_seen_at at time zone 'UTC')::date as day, count(*) n
		  from mcp_servers where org_id=$1 and first_seen_at >= $2 and first_seen_at < $3
		  group by 1
		),
		all_days as (
		  select generate_series($2::date, ($3::date - 1), '1 day'::interval)::date as day
		)
		select all_days.day,
		       coalesce(inv.allowed,0), coalesce(inv.audited,0), coalesce(inv.denied,0),
		       coalesce(c.n,0), coalesce(srv.n,0)
		from all_days
		left join inv on inv.day = all_days.day
		left join c   on c.day   = all_days.day
		left join srv on srv.day = all_days.day
		order by all_days.day`
	rows, err := d.Pool.Query(ctx, q, orgID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DailyInvocationsBucket{}
	for rows.Next() {
		var b DailyInvocationsBucket
		if err := rows.Scan(&b.Day, &b.InvocationsAllowed, &b.InvocationsAudited, &b.InvocationsDenied,
			&b.NewCallers, &b.NewServers); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ServerCountAt returns the count of servers that existed at the org by the
// given instant (server.first_seen_at <= t). Best-effort proxy for "total
// servers at end of day t".
func (d *Dashboard) ServerCountAt(ctx context.Context, orgID uuid.UUID, t time.Time) (int64, error) {
	var c int64
	const q = `select count(*) from mcp_servers where org_id=$1 and first_seen_at <= $2`
	err := d.Pool.QueryRow(ctx, q, orgID, t.UTC()).Scan(&c)
	return c, err
}

// UpsertDaily writes (or replaces) one org_daily_metrics row.
func (d *Dashboard) UpsertDaily(ctx context.Context, orgID uuid.UUID, m DailyMetric) error {
	const q = `
		insert into org_daily_metrics(
		  org_id, day,
		  invocations_allowed, invocations_audited, invocations_denied,
		  new_callers, new_servers,
		  total_servers, total_capabilities, public_exposure_count, gateways_online,
		  posture_score, computed_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now())
		on conflict (org_id, day) do update set
		  invocations_allowed   = excluded.invocations_allowed,
		  invocations_audited   = excluded.invocations_audited,
		  invocations_denied    = excluded.invocations_denied,
		  new_callers           = excluded.new_callers,
		  new_servers           = excluded.new_servers,
		  total_servers         = excluded.total_servers,
		  total_capabilities    = excluded.total_capabilities,
		  public_exposure_count = excluded.public_exposure_count,
		  gateways_online       = excluded.gateways_online,
		  posture_score         = excluded.posture_score,
		  computed_at           = now()`
	_, err := d.Pool.Exec(ctx, q,
		orgID, m.Day,
		m.InvocationsAllowed, m.InvocationsAudited, m.InvocationsDenied,
		m.NewCallers, m.NewServers,
		m.TotalServers, m.TotalCapabilities, m.PublicExposureCount, m.GatewaysOnline,
		m.PostureScore,
	)
	return err
}

// PostureScore implements the composite formula. Inputs are taken as counts
// to avoid float drift in callers — function does its own ratio math.
// Weights: 40 caps-with-policy, 30 callers-not-flagged-new, 20 servers-not-public,
// 10 gateways-online-with-bundle. With no inputs the score defaults to 100.
func PostureScore(
	capsWithCover, capsTotal,
	callersClean, callersTotal,
	serversNotPublic, serversTotal,
	gatewaysOnline, gatewaysTotal int64,
) int {
	score := 0.0
	add := func(weight float64, num, denom int64) {
		if denom <= 0 {
			score += weight
			return
		}
		r := float64(num) / float64(denom)
		if r > 1 {
			r = 1
		}
		if r < 0 {
			r = 0
		}
		score += weight * r
	}
	add(40, capsWithCover, capsTotal)
	add(30, callersClean, callersTotal)
	add(20, serversNotPublic, serversTotal)
	add(10, gatewaysOnline, gatewaysTotal)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return int(score + 0.5)
}

// PostureInputs computes the four ratios used by the posture score from a
// snapshot. Returns the assembled score.
func PostureFromSnapshot(s CurrentSnapshot) int {
	capsWithCover := s.CapabilitiesWithCover
	if s.PoliciesEnabled == 0 {
		capsWithCover = 0
	} else if capsWithCover == 0 {
		// Cheap proxy: when any policies exist, treat all capabilities as
		// "covered". The frontend distinguishes finer detail via /policies.
		capsWithCover = s.TotalCapabilities
	}
	callersClean := s.CallersTotal - s.CallersFlaggedNew
	if callersClean < 0 {
		callersClean = 0
	}
	serversNotPublic := s.TotalServers - s.PublicExposureCount
	if serversNotPublic < 0 {
		serversNotPublic = 0
	}
	return PostureScore(
		capsWithCover, s.TotalCapabilities,
		callersClean, s.CallersTotal,
		serversNotPublic, s.TotalServers,
		s.GatewaysOnline, s.GatewaysTotal,
	)
}

// ----- helpers shared with the seeder -----

// DayInLocation rounds t down to the start of the UTC day.
func DayInUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// FormatDay returns YYYY-MM-DD for the given time, in UTC.
func FormatDay(t time.Time) string {
	return DayInUTC(t).Format("2006-01-02")
}

// dayString is convenience for SQL diagnostics in tests; not used in prod.
func dayString(d time.Time) string { return fmt.Sprintf("%d-%02d-%02d", d.Year(), d.Month(), d.Day()) }

var _ = dayString // silence unused linter when not referenced
