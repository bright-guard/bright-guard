# Bright Guard — cloud control plane

This directory contains the cloud control plane app for Bright Guard:
the **API** (Go + chi + pgx + goose) and the **web** SPA (React + Vite + Tailwind).

The Hugo marketing site at the repo root is unrelated to this app.

## Prerequisites

- Go 1.25+
- Node 20+ and npm
- Docker (Desktop, Rancher, OrbStack — anything that exposes `docker compose`)
- Postgres is provided via `docker compose`; you do not need a local install

## Quick start (dev login, no Google needed)

Five commands from a fresh checkout:

```bash
# 1. Bring up Postgres (binds host port 5433 to avoid colliding with any
#    other local postgres you might already run on 5432).
cd cloud
docker compose up -d postgres

# 2. Configure the API. The default .env.example already points at
#    localhost:5433 and turns on the dev-login bypass.
cd api
cp .env.example .env
# edit .env if you want — at minimum the default SESSION_SECRET works
# for local dev; rotate it for anything real.
# Make sure DEV_LOGIN_ENABLED=true if you don't have Google creds.

# 3. Run the API. Migrations run automatically on boot.
go run ./cmd/api
# -> listening on :8080

# 4. In another terminal, run the web SPA.
cd cloud/web
npm install
npm run dev
# -> http://localhost:5173

# 5. Open http://localhost:5173, click "Dev sign in" (only shown when
#    DEV_LOGIN_ENABLED=true), enter any email, then name your first org.
```

You can also drive the API directly with curl:

```bash
curl -i -X POST -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com"}' \
  -c /tmp/bg.cookies http://localhost:8080/auth/dev/login

curl -s -b /tmp/bg.cookies http://localhost:8080/api/me

curl -s -b /tmp/bg.cookies -c /tmp/bg.cookies \
  -X POST -H 'Content-Type: application/json' \
  -d '{"name":"Acme"}' http://localhost:8080/api/orgs
```

## Setting up real Google OAuth

1. Open <https://console.cloud.google.com/apis/credentials>.
2. Create OAuth client → application type **Web application**.
3. Add an **Authorized redirect URI**:
   `http://localhost:8080/auth/google/callback` (plus any deployed URLs).
4. Copy the client ID and secret into `cloud/api/.env`:
   ```
   GOOGLE_CLIENT_ID=...
   GOOGLE_CLIENT_SECRET=...
   ```
5. Set `DEV_LOGIN_ENABLED=false` (or remove it). Google credentials are
   required whenever dev login is off; the API will refuse to start otherwise.
6. Restart the API. The login page's "Continue with Google" button will now work.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz` | liveness |
| GET | `/api/dev/enabled` | reports whether dev login is on (used by the SPA) |
| GET | `/auth/google/start` | begin OAuth (503 if Google isn't configured) |
| GET | `/auth/google/callback` | OAuth callback |
| POST | `/auth/dev/login` | dev-only; only registered when `DEV_LOGIN_ENABLED=true` |
| POST | `/auth/logout` | clears session |
| GET | `/api/me` | current user + memberships + active org |
| GET | `/api/orgs` | list caller's memberships |
| POST | `/api/orgs` | create an org; caller becomes owner; sets active org |
| POST | `/api/sessions/active-org` | switch active org (must be a member) |

## Project layout

```
cloud/
  docker-compose.yml        # postgres 16; host port 5433 -> container 5432
  api/                      # Go API
    cmd/api/main.go         # entrypoint: config, migrate, http server, shutdown
    internal/
      api/server.go         # chi router, handlers, CORS
      auth/session.go       # session cookie + middleware
      auth/google.go        # Google OIDC start / callback
      auth/dev.go           # dev-login bypass (gated by env)
      config/config.go      # env loading + validation
      db/db.go              # pgxpool open
      db/migrate.go         # embedded goose migrations
      db/migrations/        # SQL migrations
      models/models.go      # User, Org, Membership, Session, OrgRole
      store/                # users, orgs, sessions data access
  web/                      # React + Vite + Tailwind SPA
    vite.config.ts          # dev server on :5173; proxies /auth + /api -> :8080
    src/
      main.tsx              # router setup
      api/client.ts         # fetch wrapper (credentials: include)
      auth/AuthContext.tsx  # /api/me state, refresh, logout, setActiveOrg
      auth/ProtectedRoute.tsx
      pages/LoginPage.tsx
      pages/OnboardingPage.tsx
      pages/AppShell.tsx
```

## Notes / caveats

- Host port **5433** is used for Postgres so the compose file doesn't
  collide with other local pg instances. The API connection string in
  `.env.example` already reflects this.
- `DEV_LOGIN_ENABLED=true` exposes `POST /auth/dev/login` which signs in
  any email with no verification. The API logs a loud warning at startup
  when this is on. **Never enable this in production.**
- Sessions are DB-backed (`sessions` table) and identified by an
  HTTP-only cookie `bg_session`. The cookie is not Secure in local dev
  (`SESSION_COOKIE_SECURE=false`); set this to `true` in production.
- The Vite dev server proxies `/auth/*` and `/api/*` to the API on
  `localhost:8080`, so the SPA and API share an origin from the
  browser's perspective and cookies just work.
