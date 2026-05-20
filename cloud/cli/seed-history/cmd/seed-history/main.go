// seed-history seeds 90 days of synthetic org_daily_metrics rows for a single
// org. Uses a random walk anchored on today's snapshot so the dashboard looks
// meaningful in demos before any natural traffic accumulates.
//
// Usage:
//   seed-history \
//     --database-url=postgres://... \
//     --org-slug=acme \
//     --days=90 [--seed=42] [--reset]
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seed-history: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres connection string (or DATABASE_URL env)")
	orgSlug := flag.String("org-slug", "acme", "Org slug to seed (case-insensitive)")
	orgID := flag.String("org-id", "", "Org UUID. Overrides --org-slug if set.")
	days := flag.Int("days", 90, "Number of trailing days to seed")
	seed := flag.Int64("seed", 42, "RNG seed")
	reset := flag.Bool("reset", false, "Delete existing rows for the org first")
	flag.Parse()

	if strings.TrimSpace(*databaseURL) == "" {
		return fmt.Errorf("--database-url or DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	var id uuid.UUID
	if *orgID != "" {
		id, err = uuid.Parse(*orgID)
		if err != nil {
			return fmt.Errorf("parse org-id: %w", err)
		}
	} else {
		const q = `select id from orgs where lower(slug) = lower($1) or lower(name) = lower($1) limit 1`
		if err := pool.QueryRow(ctx, q, *orgSlug).Scan(&id); err != nil {
			return fmt.Errorf("lookup org %q: %w", *orgSlug, err)
		}
	}
	fmt.Printf("seeding org %s (%d days, seed=%d)\n", id, *days, *seed)

	if *reset {
		tag, err := pool.Exec(ctx, `delete from org_daily_metrics where org_id=$1`, id)
		if err != nil {
			return fmt.Errorf("reset: %w", err)
		}
		fmt.Printf("reset: deleted %d rows\n", tag.RowsAffected())
	}

	// Anchor values: pull today's totals from the live tables so the synthetic
	// history flows into them without a visible discontinuity.
	type anchor struct {
		servers       int64
		capabilities  int64
		publicSrv     int64
		gatewaysOnl   int64
		callersTotal  int64
		callersNew    int64
		policiesEn    int64
	}
	var a anchor
	const snapQ = `
		with
		s as (select count(*) c, count(*) filter (where exposure_state='public') pe from mcp_servers where org_id=$1),
		c as (select count(*) c from mcp_capabilities cap join mcp_servers ms on ms.id=cap.mcp_server_id where ms.org_id=$1),
		g as (select count(*) filter (where status='online') online from gateways where org_id=$1),
		oc as (select count(*) total, count(*) filter (where flagged_new=true) flagged from org_callers where org_id=$1),
		p as (select count(*) c from policies where org_id=$1 and enabled=true)
		select s.c, s.pe, c.c, g.online, oc.total, oc.flagged, p.c from s, c, g, oc, p`
	if err := pool.QueryRow(ctx, snapQ, id).Scan(
		&a.servers, &a.publicSrv, &a.capabilities,
		&a.gatewaysOnl, &a.callersTotal, &a.callersNew, &a.policiesEn,
	); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	fmt.Printf("anchor: servers=%d caps=%d public=%d gateways-online=%d callers=%d policies=%d\n",
		a.servers, a.capabilities, a.publicSrv, a.gatewaysOnl, a.callersTotal, a.policiesEn)

	rng := rand.New(rand.NewSource(*seed))
	// Per-day invocation walk: anchor mean scales with capabilities so empty
	// orgs don't pretend to have traffic.
	meanInv := int64(80 + a.capabilities*4)
	if meanInv < 25 {
		meanInv = 25
	}

	today := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), time.Now().UTC().Day(), 0, 0, 0, 0, time.UTC)

	for i := *days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		// Slight day-of-week bias: weekends are lighter.
		dow := day.Weekday()
		wkMul := 1.0
		if dow == time.Saturday || dow == time.Sunday {
			wkMul = 0.55
		}
		drift := 1.0 + (rng.Float64()-0.5)*0.4 // ±20%
		total := int64(float64(meanInv) * wkMul * drift)
		if total < 0 {
			total = 0
		}
		// 88% allowed / 10% audited / 2% denied with jitter.
		deniedRate := 0.015 + rng.Float64()*0.02
		auditedRate := 0.08 + rng.Float64()*0.05
		denied := int64(float64(total) * deniedRate)
		audited := int64(float64(total) * auditedRate)
		allowed := total - denied - audited
		if allowed < 0 {
			allowed = 0
		}

		newCallers := int64(0)
		if rng.Float64() < 0.4 && a.callersTotal > 0 {
			newCallers = 1 + rng.Int63n(2)
		}
		newServers := int64(0)
		if rng.Float64() < 0.05 && a.servers > 1 {
			newServers = 1
		}

		// Servers grow toward today's anchor as we approach now.
		ratio := float64(*days-i) / float64(*days)
		histServers := int64(float64(a.servers) * (0.6 + 0.4*ratio))
		if histServers < 1 {
			histServers = a.servers
		}
		histPublic := a.publicSrv
		histCaps := a.capabilities
		histGateways := a.gatewaysOnl

		// Posture wanders 70-95.
		posture := 78 + rng.Intn(18)

		const ins = `
			insert into org_daily_metrics(
			  org_id, day,
			  invocations_allowed, invocations_audited, invocations_denied,
			  new_callers, new_servers,
			  total_servers, total_capabilities, public_exposure_count, gateways_online,
			  posture_score, computed_at)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now())
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
		if _, err := pool.Exec(ctx, ins,
			id, day,
			allowed, audited, denied,
			newCallers, newServers,
			histServers, histCaps, histPublic, histGateways,
			posture,
		); err != nil {
			return fmt.Errorf("upsert %s: %w", day.Format("2006-01-02"), err)
		}
	}
	fmt.Printf("seeded %d days for org %s\n", *days, id)
	return nil
}
