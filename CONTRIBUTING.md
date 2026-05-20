# Contributing to Bright Guard

A practical, Mac-focused guide for getting productive on this codebase fast. Written so a coding agent (or new engineer) can read it once and start shipping.

## What this repo is

Two products living together:

- **`/` (root)** — Hugo marketing site for [mcp-governance.infoblox.dev](https://mcp-governance.infoblox.dev) is served by Cloud Run; product vision in [`vision.md`](vision.md).
- **`cloud/`** — the multi-tenant SaaS control plane. Go backend + React SPA + Postgres, deployed to GCP Cloud Run + Cloud SQL. This is where 99% of feature work happens.

If you're reading this because you've been asked to add a feature, you're almost certainly working in `cloud/`.

## Layout

```
cloud/
├── api/               Go control plane (chi, pgxpool, goose, go-oidc)
│   ├── cmd/api/       Entrypoint
│   ├── internal/
│   │   ├── api/         HTTP handlers (chi router; one file per feature: phase2.go, connections.go, device.go, ...)
│   │   ├── auth/        Session/cookie + Bearer middleware, Google OIDC, dev-login, device-code grant
│   │   ├── config/      Env-driven config
│   │   ├── db/          pgxpool + goose; migrations under db/migrations/
│   │   ├── exposure/    Address-to-exposure classifier (pure)
│   │   ├── mcp/         JSON-RPC MCP client (streamable-http / sse / http), AuthRoundTripper, OAuth2RoundTripper
│   │   ├── models/      JSON-tagged structs shared by store + api
│   │   ├── scheduler/   Discovery + sweep goroutines (per-feature advisory locks)
│   │   ├── spa/         //go:embed of the built SPA — served as a catch-all in prod
│   │   └── store/       Data access (Pool *pgxpool.Pool + plain SQL via pgx)
│   └── migrations/    (lives under internal/db/migrations because //go:embed forbids ..)
├── web/               Vite + React 18 + TypeScript + Tailwind v3 SPA
│   ├── src/api/         fetch wrapper (credentials: include; ApiError)
│   ├── src/auth/        AuthContext + ProtectedRoute (react-router v6)
│   └── src/pages/       One file per page, named *Page.tsx
├── shim/              Go binary that pretends to be agentgateway (fake observations)
├── cli/               bg-auth.sh (device-code helper), seed/ (Acme demo seeder), README
├── deploy/            deploy.sh — gcloud builds submit + gcloud run deploy
├── Dockerfile         Multi-stage: node builds SPA → Go embeds + builds → distroless final
├── docker-compose.yml Local Postgres
└── README.md          Dev quick-start
```

## Toolchain (Mac)

Install via Homebrew. The `--cask` lines are optional but the rest are required.

```bash
# Mandatory
brew install go              # >=1.25
brew install node            # >=20
brew install gcloud-cli      # or: brew install --cask google-cloud-sdk
brew install cloud-sql-proxy
brew install postgresql@16   # only if you want a `psql` client; Postgres itself is via Docker
brew install gh              # GitHub CLI — used by issue automation
brew install hugo            # only if you'll touch the marketing site

# Recommended
brew install --cask rancher  # Docker runtime; or Docker Desktop
brew install jq              # JSON wrangling in scripts
brew install just            # if we ever add a Justfile
brew install golangci-lint   # local lint
```

After installing gcloud:

```bash
gcloud auth login
gcloud auth application-default login
gcloud config set project bright-guard-prod
gcloud auth configure-docker us-central1-docker.pkg.dev
```

You also need the Linux Foundation [agentgateway](https://github.com/agentgateway/agentgateway) for real gateway integration eventually, but the demo shim covers anything you'd want to test locally.

## Repo conventions

- **No AI attribution anywhere** — not in code comments, commit messages, PR descriptions. Don't add `Co-Authored-By` lines or "generated with" preambles.
- **Sparse comments** — explain *why*, not *what*. Don't write multi-paragraph docstrings that restate code. Skip Java-style banner comments.
- **One feature, one handler file** — `api/connections.go`, `api/phase2.go`, `api/device.go`, etc. Don't grow `server.go` to thousands of lines.
- **One store struct per concern** — `Users`, `Orgs`, `Sessions`, `Connections`, `Activity`, `Callers`. Same pattern: `type X struct { Pool *pgxpool.Pool }` + plain SQL via pgx. No ORM.
- **Migrations are append-only and goose-format** — `cloud/api/internal/db/migrations/NNNNN_name.sql` with `-- +goose Up` / `-- +goose Down` blocks. They run automatically on container boot. **Pick the next free number**: list them, take max+1. Don't reuse / renumber.
- **`org_id` on every tenant row** — multi-tenancy is enforced in the data layer. There's no "shared global state".
- **Secrets via Secret Manager** in prod, `.env` locally. Never commit `.env`. `cloud/api/.env.example` is the source of truth for what's needed.
- **Front-end paths are relative** — `fetch("/api/me")`, not `https://...`. The API and SPA share an origin in prod.

## Local dev loop

### 1. One-time setup

```bash
cd cloud
docker compose up -d postgres                # Postgres 16 on host port 5433 (5432 is often taken)
cd api
cp .env.example .env
# Edit .env: set SESSION_SECRET to 32+ chars random; DEV_LOGIN_ENABLED=true
# Google OAuth creds can stay blank when DEV_LOGIN_ENABLED=true.
```

For ALLOWED_HOSTS in local dev, leave it empty — config falls back to the host from APP_BASE_URL.

### 2. Run the API + the SPA

Two terminals:

```bash
# Terminal 1 — API
cd cloud/api
go run ./cmd/api
# Listens on :8080. Migrations apply on boot. /api/healthz returns {"ok":true}.

# Terminal 2 — SPA
cd cloud/web
npm install            # once
npm run dev
# Listens on :5173. Proxies /api and /auth to :8080.
```

Open <http://localhost:5173/login>. Use the "Dev login" panel (only shown when `DEV_LOGIN_ENABLED=true`) to sign in as any email. First login lands on `/onboarding` to create an org.

### 3. Seed a realistic demo

```bash
# in another terminal, while the API is running
cd cloud/cli/seed
# Get a session cookie via dev-login:
curl -c /tmp/c -X POST -H 'Content-Type: application/json' \
  -d '{"email":"demo@example.com"}' \
  http://localhost:8080/auth/dev/login
SESSION=$(awk '/bg_session/{print $7}' /tmp/c)

go run ./cmd/seed \
  --control-plane=http://localhost:8080 \
  --cookie="$SESSION" \
  --fixture=fixtures/acme.yaml \
  --org-name="Acme Corp"
```

After ~30s you'll see 2 gateways, 5 MCP servers, 35 capabilities, 575 invocations in the dashboards.

### 4. Run the shim against your local API

```bash
# From the UI: /app/gateways → Add Gateway → copy install command
# Or pass the bootstrap token to local shim directly:
cd cloud/shim
BG_CONTROL_PLANE=http://localhost:8080 \
BG_ENROLLMENT_TOKEN=<from-UI> \
BG_CONFIG=examples/fake-servers.yaml \
BG_STATE_DIR=/tmp/bg-shim-state \
go run ./cmd/shim
```

### 5. CLI auth (real bearer tokens, no cookie copying)

```bash
cd cloud/cli
./bg-auth.sh
# Opens browser to approve a device code; saves bearer to ~/.config/brightguard/credentials.
export BG_TOKEN=$(cat ~/.config/brightguard/credentials)
curl -H "Authorization: Bearer $BG_TOKEN" http://localhost:8080/api/me
```

## Builds & tests

```bash
# Go modules
cd cloud/api && go build ./... && go vet ./... && go test ./...
cd cloud/shim && go build ./...
cd cloud/cli/seed && go build ./...

# SPA
cd cloud/web && npm run build
```

Tests:
- **Unit tests** live next to the code (`*_test.go` in Go; nothing for the SPA yet).
- **Integration tests** that need Postgres are gated on `TEST_DATABASE_URL` — they skip in normal `go test ./...` runs. To run them: `TEST_DATABASE_URL='postgres://brightguard:brightguard@localhost:5433/brightguard?sslmode=disable' go test ./...`.
- **No CI yet** — `gh actions` for this repo only deploys the Hugo site. Adding `cloud/` CI is on the backlog.

## Working with parallel agents (worktrees)

If you're a coordinating agent dispatching sub-agents, use `isolation: "worktree"` mode on the Agent tool. Heuristics learned the hard way:

- **The `cloud/` tree must be committed before spawning worktrees.** A worktree branches from the latest commit, not the dirty working tree. Untracked changes won't be visible to the spawned agent.
- **Each agent owns disjoint files.** Decide upfront which agent owns each `*.go`/`*.tsx` file, especially `server.go`, `cmd/api/main.go`, `models.go`, `api/types.ts`, `main.tsx`, `AppShell.tsx`. Shared additive lines (one new route, one new sidebar entry) are OK; large refactors will collide.
- **Each agent gets its own migration number** — pick a contiguous range and tell each agent which number to use. Goose tolerates gaps but collisions are silent merge disasters.
- **Each background sweep gets its own advisory-lock key** (`bg-discovery`, `bg-exposure-sweep`, `bg-callers`) so leaders don't block each other.
- **Don't ask agents in worktree mode to deploy** — Cloud Run is a singleton in prod; only the coordinator deploys.
- **The Bash tool's cwd persists between calls.** Use absolute paths or chain `cd X && cmd`. This has burned multiple agents.

## Deploying

```bash
cd cloud
./deploy/deploy.sh
```

What it does:
1. `gcloud builds submit` — uploads the cloud/ tree, builds the multi-stage Dockerfile, pushes to Artifact Registry.
2. `gcloud run deploy` — rolls out a new revision with secrets from Secret Manager (`session-secret`, `google-client-id`, `google-client-secret`, `db-password`) and Cloud SQL connector attached.

Env vars preserved across deploys:
- `APP_BASE_URL`, `WEB_BASE_URL` — once set, deploy keeps them. Override with `BASE_URL=https://...`.
- `ALLOWED_HOSTS` — list of hostnames the OAuth flow accepts (multi-domain support). Defaults to the canonical custom domain + the `*.run.app` URL.

Secrets are loaded at container start; **`gcloud run services update --update-labels=…` does NOT force a new revision**. To pick up new secret versions, change an env var (e.g. `OAUTH_BUMP=$(date +%s)`).

## Observability

For now: `gcloud logging read 'resource.type="cloud_run_revision" resource.labels.service_name="bright-guard"'`. There's no structured-log query layer, no metrics dashboard. This is operational debt — see issue tracker.

## Common patterns

### Adding an org-scoped endpoint

1. Add the SQL store function in `cloud/api/internal/store/<feature>.go`.
2. Add the handler in `cloud/api/internal/api/<feature>.go` using the existing `orgMember` middleware pattern.
3. Mount under the existing `r.Route("/api/orgs/{orgId}", ...)` block in `server.go` — additive lines only.
4. Wire any new stores into `cmd/api/main.go` and the `Server` struct.
5. Add the new types to `cloud/web/src/api/types.ts` and consume via `api<T>("/api/orgs/...")`.

### Adding a migration

```bash
ls cloud/api/internal/db/migrations/      # find next free number NNNNN
cat > cloud/api/internal/db/migrations/NNNNN_short_name.sql <<'EOF'
-- +goose Up
-- +goose StatementBegin
-- your DDL here
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- your reverse DDL here
-- +goose StatementEnd
EOF
```

Migrations run automatically on container boot — both locally and in Cloud Run.

### Adding an SPA page

1. New file `cloud/web/src/pages/<Name>Page.tsx`.
2. Add the route in `cloud/web/src/main.tsx` under the existing `/app/*` group.
3. If it should appear in the sidebar, add a `<SideLink>` in `AppShell.tsx`.
4. Pages use `useAuth()` from `auth/AuthContext.tsx` for the current user + active org; use `api<T>(path)` from `api/client.ts` for fetches.

## Don't do this

- **Commit `.env`** — it's gitignored for a reason.
- **Run `gcloud sql instances delete`** — Cloud SQL is the floor cost; recreate is painful.
- **Skip tests with `-count=0`** — if a test is slow, fix it.
- **Add an ORM** — pgx + hand-written SQL is the convention. We're not at a scale where the ergonomics tradeoff makes sense.
- **Treat the shim as the real gateway** — it's a fixture for the dashboards. Real agentgateway integration is its own work item.

## Where to look when you're stuck

- **A handler doesn't exist** — `git grep "func (s \*Server) handle" cloud/api/internal/api/` lists every one.
- **A field appears in JSON but not the DB** — check `cloud/api/internal/models/models.go` and the corresponding `Scan(...)` in the store; SELECT lists are hand-written.
- **Frontend can't see a new endpoint** — `cloud/web/src/api/types.ts` is the type contract; if it isn't there, `api<T>(...)` won't infer.
- **Cloud Run env vars** — `gcloud run services describe bright-guard --region=us-central1 --format='value(spec.template.spec.containers[0].env)' | tr ';' '\n'`.
- **What's deployed right now** — `gcloud run revisions list --service=bright-guard --region=us-central1 --limit=5`.

## Picking the next thing to do

The product backlog is on GitHub Issues, prioritized P0–P3 with `priority:Pn` labels and grouped under milestones (Phase 2 / Phase 3 / Phase 4 / Phase 5). The vision the backlog ladders up to is in [`vision.md`](vision.md).
