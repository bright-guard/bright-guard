# bg-seed

`bg-seed` is a demo-data seeding tool for the Bright Guard control plane. Given
a YAML fixture, it ensures an org exists, creates gateways, enrolls them, and
posts a procedurally-generated batch of observations (servers, capabilities,
invocations) so the dashboards at `/app/gateways` and `/app/mcp-servers` show
realistic data for demos and screenshots.

## Build

```
go build ./...
go build -o bg-seed ./cmd/seed
```

The module is self-contained (no dependency on `cloud/api`). The only external
dependency is `gopkg.in/yaml.v3`.

## Run

```
bg-seed --help
bg-seed --control-plane=https://mcp-governance.infoblox.dev \
        --token=bg_cli_<uuid>.<secret> \
        --fixture=fixtures/acme.yaml \
        --org-name="Acme Corp"
```

For local dev against a `DEV_LOGIN_ENABLED=true` instance, pass a session
cookie value instead of a CLI token:

```
bg-seed --control-plane=http://localhost:8080 \
        --cookie="$BG_SESSION" \
        --fixture=fixtures/acme.yaml
# or set BG_COOKIE in the env and omit --cookie
```

Flags:

| Flag              | Purpose                                                |
| ----------------- | ------------------------------------------------------ |
| `--control-plane` | Base URL of the Bright Guard API. Required.            |
| `--token`         | CLI bearer (`bg_cli_<uuid>.<secret>`). Sent as Bearer. |
| `--cookie`        | Value of `bg_session` cookie. Local dev fallback.      |
| `--fixture`       | Path to fixture YAML. Default `fixtures/acme.yaml`.    |
| `--org-name`      | Override `fixture.org.name`.                           |
| `--seed`          | RNG seed (default 42, reproducible output).            |
| `--batch-size`    | Max invocations per observations POST (default 200).   |

`BG_COOKIE` env var is consulted if `--cookie` is omitted.

## Fixture shape

```yaml
org:
  name: Acme Corp

# Logical server definitions. The seeder references them by `key`.
servers:
  - key: github
    name: github-mcp          # name as it will appear in the dashboard
    address: stdio:///...      # arbitrary string
    transport: stdio | streamable-http | sse
    version: 1.2.3
    metadata:                  # opaque, surfaces in server detail
      vendor: github
    callers:                   # weighted pool used for invocation callers
      - {agent: copilot-agent, userEmail: alice@acme.example, weight: 5}
    capabilities:              # what the server exposes
      - {kind: tool,     name: create_issue, description: "..."}
      - {kind: resource, name: "repo://README", description: "..."}
      - {kind: prompt,   name: pr_review, description: "..."}

# Gateways and the servers they observe (with traffic profile per server).
gateways:
  - name: acme-prod-cluster-gateway
    description: "Production cluster gateway"
    observes:
      - server: github                 # references servers[].key
        traffic:
          total_invocations: 180       # how many invocations to generate
          hours_back: 24               # spread oldest events this far back
          recent_hour_weight: 0.45     # fraction of events in the last hour
          error_rate: 0.03             # 0..1
          denied_rate: 0.01            # 0..1; rest are "ok"
          latency_median_ms: 90        # median latency in ms
          latency_sigma: 0.55          # log-normal sigma
          capability_weights:          # optional weighted draw
            - {name: search_code, weight: 30}
            - {name: list_repos,  weight: 18}
```

The seeder generates invocations in-process from these knobs — the YAML stays
short while the data set stays demoable. Invocations are sorted by timestamp
and pushed in batches via `POST /v1/gateway/observations`. The RNG is seeded
deterministically (`--seed`, default 42), so the same fixture produces the
same data set on every run.

## What it does

1. `GET /api/orgs` — find an org by name (case-insensitive).
2. If missing: `POST /api/orgs` to create it.
3. `POST /api/sessions/active-org` to make it the active org for this session.
4. For each gateway in the fixture:
   - `POST /api/orgs/{id}/gateways` — get enrollment token.
   - `POST /v1/gateway/register` — exchange for a long-lived credential.
   - `POST /v1/gateway/observations` — declare the servers/capabilities, then
     push generated invocations in batches.
   - `POST /v1/gateway/heartbeat` — mark the gateway online.
5. Print a summary table.

The seeder is **append-only** for invocations and **idempotent** for the org
(re-running won't create duplicate orgs). It currently skips gateways that
already exist by name; delete the org or pick a fresh `--org-name` to start
clean.

## Local verify recipe

```
cd cloud
docker compose up -d postgres
cd api
DEV_LOGIN_ENABLED=true APP_BASE_URL=http://localhost:8080 \
WEB_BASE_URL=http://localhost:8080 PORT=8080 \
DATABASE_URL=postgres://brightguard:brightguard@localhost:5433/brightguard?sslmode=disable \
SESSION_SECRET=$(openssl rand -base64 48 | tr -d '\n') \
go run ./cmd/api &

curl -c /tmp/c -X POST -H 'Content-Type: application/json' \
  -d '{"email":"seed@acme.example"}' http://localhost:8080/auth/dev/login
SESSION=$(awk '/bg_session/{print $7}' /tmp/c)

cd ../cli/seed
go run ./cmd/seed --control-plane=http://localhost:8080 \
  --cookie="$SESSION" --fixture=fixtures/acme.yaml --org-name="Acme Corp"

# Verify:
curl -s -b /tmp/c http://localhost:8080/api/orgs | jq
ORG=$(curl -s -b /tmp/c http://localhost:8080/api/orgs | jq -r '.[0].org.id')
curl -s -b /tmp/c http://localhost:8080/api/orgs/$ORG/gateways  | jq
curl -s -b /tmp/c http://localhost:8080/api/orgs/$ORG/mcp-servers | jq
```
