# Repository Layout

The application code lives under `cloud/`. The repo root is also a Hugo marketing site (`hugo.toml`, `content/`, `layouts/`, `static/`); that side is unrelated to runtime behavior and is not covered here. The product lives in `cloud/`.

## Top-level

```
cloud/
  api/         -- Go control-plane binary + embedded SPA assets
  web/         -- React SPA source (Vite + TS), built into api binary
  shim/        -- Customer-side Go shim (separate binary, separate image)
  deploy/      -- deploy.sh for the API service
  cli/         -- One-shot tools (e.g. demo seeder)
  Dockerfile   -- Multi-stage build: web -> api -> distroless runtime
  docker-compose.yml
  README.md
```

## `cloud/api`

| Path | Purpose |
|---|---|
| `cmd/api/main.go` | Process entrypoint. Wires config, db, stores, schedulers, http server. |
| `cmd/gen-docs/` | Generates `cloud/web/docs/reference/routes.md` and `cel-env.md` from the registered chi router. |
| `internal/api/` | HTTP handlers and middleware. `server.go` registers every route. |
| `internal/auth/` | Cookie + bearer session middleware, Google OIDC, dev-login, role checks, platform-admin gate. |
| `internal/config/` | Env-var resolution + validation. |
| `internal/db/` | `pgxpool` wrapper + embedded goose migrations. |
| `internal/db/migrations/` | `0000N_*.sql` files. Applied at process boot. |
| `internal/email/` | `Sender` interface; stub for dev, GCP Cloud Email for prod. |
| `internal/exposure/` | URL-based exposure classifier (no DNS, no probes). |
| `internal/mcp/` | MCP client (streamable-http, sse, http), DCR (RFC 7591), OAuth2 round-tripper, types. |
| `internal/models/` | Shared structs surfaced as JSON. |
| `internal/policy/` | CEL engine (compile + cost-limited eval). Mirrors the shim's `policy.go`. |
| `internal/scheduler/` | Discovery, exposure-sweep, caller-sweep, policy-sweep goroutines. |
| `internal/spa/` | `go:embed` of the built React bundle. |
| `internal/store/` | Postgres data access. One file per domain (orgs, users, gateways, connections, …). |

`internal/api/server.go` is the canonical route map (`cloud/api/internal/api/server.go:47-202`). The auto-generated `cloud/web/docs/reference/routes.md` is the readable counterpart.

## `cloud/web`

| Path | Purpose |
|---|---|
| `src/main.tsx` | React Router setup; route tree. |
| `src/auth/` | `AuthContext`, `ProtectedRoute`. |
| `src/pages/` | Tenant pages (Gateways, MCP Servers, Activity, Policies, …). |
| `src/admin/` | Platform-admin pages (mounted at `/admin`). |
| `src/api/` | Typed fetch client + shared types. |
| `docs/` | Markdown source for the in-app `/docs` site (Wave N+6a, epic #38). |
| `dist/` | Vite build output. Copied into `cloud/api/internal/spa/dist/` by the Dockerfile. |

The in-app docs route tree mounts at `/docs` and is served from the same SPA bundle (`cloud/web/src/main.tsx:86-96`). Markdown is rendered client-side via `marked`.

## `cloud/shim`

| Path | Purpose |
|---|---|
| `cmd/shim/main.go` | The ticker loop: heartbeat, apply bundle, emit fake invocations. |
| `cmd/shim/policy.go` | CEL env declaration that **must** byte-for-byte match `cloud/api/internal/policy/policy.go`. |
| `examples/fake-servers.yaml` | Demo shim config. |
| `Dockerfile`, `deploy.sh` | Multi-arch buildx, pushes `bright-guard-shim:latest`. |

The shim is intentionally small (one main, one policy file, one config loader). It has no database, no scheduler — just a 30s ticker, a credential file, a policy cache.

## `cloud/deploy`

- `deploy.sh` — builds via Cloud Build, deploys via `gcloud run deploy`, preserves existing env vars from the running revision so re-deploys don't clobber `ALLOWED_HOSTS` / `APP_BASE_URL`. See [08-deployment.md](./08-deployment.md).

## Where new code typically lands

- **New route** → handler file under `internal/api/` + a line in `server.go` Router(). Re-run `go run ./cmd/gen-docs` to update routes reference.
- **New table** → `internal/db/migrations/000NN_<topic>.sql` (next free integer; goose tolerates gaps but collisions are silent disasters — see project `CLAUDE.md`).
- **New scheduled work** → a new file under `internal/scheduler/` with its own advisory-lock key, launched as a goroutine from `cmd/api/main.go`.
- **New SPA page** → file under `cloud/web/src/pages/`, route entry in `main.tsx`, nav entry in `AppShell.tsx`.
