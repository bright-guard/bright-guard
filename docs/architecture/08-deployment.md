# Deployment

One command, one image, one Cloud Run service.

```bash
cd cloud
./deploy/deploy.sh
```

## What `deploy.sh` does

`cloud/deploy/deploy.sh` (62 lines, no magic):

1. `gcloud builds submit --tag=<image>` — uploads the `cloud/` tree to Cloud Build, which runs `cloud/Dockerfile` and pushes the resulting image to Artifact Registry at `us-central1-docker.pkg.dev/bright-guard-prod/bright-guard/bright-guard:<ts>`. Tag is a UTC timestamp.
2. Resolves a base URL — preferring `BASE_URL` override, then the existing revision's `APP_BASE_URL`, then `status.url` of the existing service, then a fresh `*.run.app` placeholder (`cloud/deploy/deploy.sh:25-31`). This is how custom-domain config survives a redeploy.
3. Reads `db-password` secret and substitutes it into a Cloud SQL unix-socket DSN (`postgres://brightguard:<pwd>@/brightguard?host=/cloudsql/<conn>&sslmode=disable`).
4. `gcloud run deploy` with secrets attached (`SESSION_SECRET`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` via `--update-secrets`) and env vars set (`APP_BASE_URL`, `WEB_BASE_URL`, `SERVE_SPA=true`, `DEV_LOGIN_ENABLED=false`, `EMAIL_PROVIDER`, `EMAIL_FROM`, `ALLOWED_HOSTS`, `DATABASE_URL`).

The script is safe to re-run — it preserves what was already set on the running revision.

## Image build

`cloud/Dockerfile` is three stages:

```
node:20-alpine  → npm ci && npm run build      (cloud/web → dist/)
golang:1.25-alpine → COPY web/dist → internal/spa/dist/ && go build
gcr.io/distroless/static-debian12:nonroot       (final runtime)
```

The SPA is **embedded** into the Go binary via `go:embed` of `cloud/api/internal/spa/dist/`. The runtime image is `distroless` — no shell, no package manager, no busybox; only the static-linked binary and CA certs. Listens on `:8080` as `nonroot:nonroot`.

## Cloud Run flags worth knowing

From `cloud/deploy/deploy.sh:43-58`:

| Flag | Value | Why |
|---|---|---|
| `--allow-unauthenticated` | yes | The whole control plane is internet-facing; auth is application-level. |
| `--port` | 8080 | Matches `Dockerfile:ENV PORT=8080`. |
| `--memory` | 512Mi | Plenty for the current load. |
| `--cpu` | 1 | One vCPU per instance. |
| `--min-instances` | 0 | Cold starts happen. There is no read-only replica. |
| `--max-instances` | 4 | Caps the scheduler-replica count to 4. |
| `--add-cloudsql-instances` | `<project>:us-central1:bright-guard-db` | Mounts the Cloud SQL unix socket at `/cloudsql/<conn>`. |

The demo shim is a separate Cloud Run service (`bright-guard-shim-demo`) with `min-instances=1` so it heartbeats continuously. Deploy via `cd cloud/shim && ./deploy.sh` (multi-arch buildx).

## Environment variables

| Var | Required | Notes |
|---|---|---|
| `APP_BASE_URL` | yes | The public URL the API is served at. Used in OAuth redirect URIs, install commands, device-flow verification URI. |
| `WEB_BASE_URL` | yes | Currently the same as `APP_BASE_URL`. Separates browser-facing URLs from API base in case they ever diverge. |
| `DATABASE_URL` | yes | Postgres DSN. In Cloud Run this is the unix-socket form. |
| `SESSION_SECRET` | yes, ≥32 chars | HMAC key for AEAD-encrypted secrets, session-cookie HMAC, gateway secret hashing. |
| `SESSION_COOKIE_SECURE` | prod=true | Set `Secure` flag on `bg_session`. |
| `DEV_LOGIN_ENABLED` | prod=false | When true, opens `/auth/dev/login` (no credential). Refuses to start with Google creds missing in dev mode. |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | yes if dev-login off | Google OIDC. |
| `ALLOWED_HOSTS` | recommended | Comma-separated list. Defaults to host of `APP_BASE_URL`. Multi-host support hinges on this. |
| `SERVE_SPA` | yes (`true`) | Mounts the embedded SPA as a chi catch-all. |
| `EMAIL_PROVIDER` | `stub` or `gcp_email` | Stub logs to stdout. GCP path uses Cloud Email API. |
| `EMAIL_FROM` | yes | Outbound mail "from". |
| `PLATFORM_ADMIN_SEED_EMAILS` | optional | Comma-separated emails auto-promoted to `platform_admins` on sign-in. Default seed in `cloud/api/internal/config/config.go:33-36`. |
| `DISCOVERY_INTERVAL_MINUTES` | optional | Discovery sweep interval. Default 60. |
| `EXPOSURE_SWEEP_INTERVAL_MINUTES` | optional | Exposure sweep interval. Default 10. |
| `PORT` | inherited | Set by Cloud Run; the binary reads it. |

## Env-var gotchas

1. **Secrets propagate only on a new revision.** Bumping a secret in Secret Manager does NOT roll forward to running revisions. To force one, bump any env var: `gcloud run services update bright-guard --update-env-vars=OAUTH_BUMP=$(date +%s)`. `--update-labels` does NOT trigger a new revision.
2. **`--set-env-vars` parses commas as delimiters.** Values containing commas need a custom delimiter. `cloud/deploy/deploy.sh:56` uses `^|^` for `ALLOWED_HOSTS` and `^~^` for `DATABASE_URL` (because the DSN contains `@`).
3. **Use `--update-env-vars`, not `--set-env-vars`, to preserve existing vars.** `deploy.sh` uses `--set-env-vars` deliberately because it re-derives every var from the current revision's existing values up front; if you edit this script, preserve that invariant.

## Migrations

Goose-managed, applied at process boot by `db.Migrate` (`cloud/api/internal/db/migrate.go:16-27`). Files live at `cloud/api/internal/db/migrations/*.sql` and are bundled into the binary via `embed.FS`. Down migrations exist but the convention is forward-only in production.

Migration numbers must be chosen up-front when waves are planned — goose tolerates gaps but **silent collisions** if two agents pick the same number. See `CLAUDE.md` for the wave-time reservation rule.

## Health & verification

- `GET /api/healthz` returns `{"ok":true}` unconditionally. Cloud Run uses this as the startup-probe path. **`GET /healthz` (no `/api/` prefix) hits the Google Frontend's generic 404 — use `/api/healthz`.**
- New migration log: `gcloud logging read 'resource.labels.service_name=bright-guard textPayload:"migrations applied"' --limit=5`.
- After UI changes: fetch `/assets/index-*.js` and `grep -oE` for new strings — agents have shipped backend changes whose frontend changes silently failed to land, and only bundle inspection catches it.

## Where things live in GCP

| Resource | Identifier |
|---|---|
| Project | `bright-guard-prod` (806435112268) |
| Region | `us-central1` |
| Service | Cloud Run `bright-guard`, domain `mcp-governance.infoblox.dev` |
| Demo shim | Cloud Run `bright-guard-shim-demo` |
| Database | Cloud SQL Postgres `bright-guard-db` (cheapest tier) |
| Image registry | Artifact Registry repo `bright-guard` |
| Secrets | Secret Manager: `session-secret`, `db-password`, `google-client-id`, `google-client-secret`, `bg-shim-bg-credential` |
| DNS | Cloud Run domain mapping → `ghs.googlehosted.com` CNAME |

Standing infra cost ~$25/month: Cloud SQL ($15) + Cloud Run service ($5–10) + demo shim ($5). Stop the database with `gcloud sql instances patch bright-guard-db --activation-policy=NEVER`.
