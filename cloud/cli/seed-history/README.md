# seed-history

Backfills 90 days of plausible `org_daily_metrics` rows for one org so the
executive dashboard at `/app` has something to show in demos before natural
traffic accumulates.

Unlike `cloud/cli/seed`, this tool writes **directly to Postgres** (it needs
control over historical `day` values, which the API does not expose). It is
demo tooling only — never run it against an org that already has authentic
metrics history unless you also pass `--reset`.

## Build

```
cd cloud/cli/seed-history
go build -o seed-history ./cmd/seed-history
```

## Run

```
# Against local dev
DATABASE_URL=postgres://brightguard:brightguard@localhost:5433/brightguard?sslmode=disable \
  ./seed-history --org-slug=acme --days=90

# Against Cloud SQL via cloud-sql-proxy (recommended for prod demo data)
cloud-sql-proxy --port 5432 bright-guard-prod:us-central1:bright-guard-db &
DATABASE_URL=postgres://brightguard:PASS@localhost:5432/brightguard?sslmode=disable \
  ./seed-history --org-slug=acme --days=90
```

Flags:

| Flag             | Purpose                                          |
| ---------------- | ------------------------------------------------ |
| `--database-url` | Postgres connection string (or `DATABASE_URL`).  |
| `--org-slug`     | Org slug or name (case-insensitive). Default `acme`. |
| `--org-id`       | Org UUID. Overrides `--org-slug`.                |
| `--days`         | Trailing days to seed. Default 90.               |
| `--seed`         | RNG seed (reproducible). Default 42.             |
| `--reset`        | Delete existing metrics rows for the org first.  |

## What it produces

Per day:

- `invocations_allowed/audited/denied` follow a weekday-skewed random walk
  anchored on a per-org mean (derived from current capability count).
- `new_callers` / `new_servers` are sparse positive integers.
- `total_servers` ramps from 60% of today's count up to today's value over
  the window.
- `total_capabilities`, `public_exposure_count`, `gateways_online` use
  today's snapshot for every historical day. Real history is not retained
  in the existing schema; the dashboard documents this approximation.
- `posture_score` wanders in 78–95.

The seeder is idempotent (per-day UPSERT). Re-running with a new `--seed`
shuffles the trajectory; re-running with the same `--seed` is a no-op.
