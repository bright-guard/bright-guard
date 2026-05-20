# CLAUDE.md — Development cycle for Bright Guard

This file is read by Claude on every session. It records how we ship work on
this codebase. **Read this first.** Pair with `CONTRIBUTING.md` (toolchain,
conventions, local-dev loop) and `vision.md` (product north star).

## TL;DR

- **Default to a "wave":** a small set of GH issues that ship together. One
  issue = one agent prompt. Three to four parallel agents is the comfortable
  ceiling; beyond that, merges get painful.
- **Always commit `cloud/` before launching worktree-mode agents.** Worktrees
  spawn from `HEAD`; if `cloud/` is uncommitted the agent's worktree sees only
  the Hugo marketing site and either fabricates code (bad) or smartly bails
  (good). Multiple agents have hit this.
- **After every deploy, QA via a headless agent.** Triage findings, fix the
  P0/P1s in a quick follow-up deploy, file the P2/P3s as new issues. Keep
  going.
- **Deploy is one command:** `cloud/deploy/deploy.sh`. Reuses existing env
  vars by reading them off the running revision; safe to re-run.

## The wave loop

```
file issues → launch agents (worktree) → merge → deploy → QA → fix/file → repeat
```

A "wave" closes 2–5 GH issues and pushes a single Cloud Run revision. Waves
named in commits / chat as Wave N+1, N+2, etc. — not formal milestones, just a
rhythm.

### Picking what goes in a wave

- **Group P0/P1 items with disjoint file footprints.** The single biggest
  cause of merge pain is two agents both editing `cloud/api/internal/api/server.go`
  or `cloud/web/src/main.tsx`. When in doubt, give shared files (server.go,
  main.go, models.go, types.ts, AppShell.tsx) to one agent and instruct the
  others to make additive-only edits.
- **Reserve migration numbers up front.** Tell each agent its number
  (`00010_foo.sql`, `00011_bar.sql`, ...) in the prompt. Goose tolerates gaps
  but collisions are silent disasters.
- **Reserve advisory-lock keys.** Background sweeps each need a unique key
  (`bg-discovery`, `bg-callers`, `bg-exposure-sweep`, `bg-policy-sweep`).
  Putting two on the same key serializes them.
- **One designed thing per wave.** Don't put two design-ambiguous items in the
  same wave — humans need to be in the loop on each.

## Launching agents

### Worktree mode (default)

- Use `isolation: "worktree"` on the Agent tool for parallel work.
- **Pre-flight: commit `cloud/`.** Worktrees are stamped at `HEAD` time; an
  uncommitted `cloud/` tree is invisible to the spawned agent. Symptom:
  agents report "this worktree's branch is at commit X which predates the
  cloud/ directory entirely." Smart agents rebase onto main; dumb ones invent
  files.
- Tell the agent: *"If your worktree base predates `cloud/`, rebase onto
  current main before working."* This catches the recurring footgun.
- Tell the agent: *"DO NOT deploy, DO NOT commit, DO NOT push. Leave changes
  uncommitted in the worktree."*
- Tell the agent: *"The Bash tool's cwd persists between calls. Use absolute
  paths or chain `cd X && cmd`."* This has burned several agents.

### When NOT to use worktrees

- Tightly coupled work where a single agent must reason about the whole vertical
  slice (the device-auth flow, the UC10 distributed-enforcement protocol). One
  agent, one prompt, no worktree.

### Prompts are self-contained

Each agent has zero memory of the conversation. The prompt must contain:

1. The full task scope referenced to a GH issue with the design.
2. Existing-code orientation: which files to read, which to extend, which to
   leave alone.
3. Other agents' file footprints to avoid.
4. Hard rules block: no deploy, no commit, no AI attribution, sparse comments.
5. Verification gates: `go build ./...`, `go vet ./...`, `go test ./...`,
   `npm run build` — every wave passes these.
6. A bounded report-back instruction (`under 300 words`, `paste the JSON
   shape`, etc.) so the response stays useful.

## Merging worktrees back to main

This is the most error-prone step. Be deliberate.

### Order of operations

1. `git -C <worktree> diff HEAD > /tmp/AGENT.diff` for each.
2. `cp -r` every untracked file from each worktree into main first.
3. `git apply --3way /tmp/AGENT.diff` in dependency order (smallest first).
4. **Verify the apply actually applied.** `git apply` sometimes reports "clean"
   when its delta is a no-op because the base mismatched. After every apply:
   `grep -c <new-string-the-agent-claimed-to-add> <file>` to confirm.
5. For files the agent reports as edited but `grep -c` returns 0, copy the
   worktree's version directly: `cp <worktree>/<file> <main>/<file>`. Don't
   trust `git apply` blindly.
6. For shared files (server.go, main.go, models.go, types.ts, main.tsx,
   AppShell.tsx) that two agents touched: hand-merge. Take the diff for each
   agent's view of that file, splice both additions in.
7. After the merge: `go build ./... && go vet ./... && go test ./...` and
   `npm run build`. If anything fails, the agent's report is your map for
   what should be present.

### Agents that leak into main

Multiple agents have written files directly into the main repo even when
launched in worktree mode (via absolute paths in the Edit/Write tools rather
than `cd`'ing into the worktree). This is OK in practice — their work is
already merged. But:

- It defeats isolation: two agents leaking simultaneously can clobber each
  other on shared files. Avoid running parallel agents that would both edit
  the same shared file.
- Verify the agent's reported file paths match what's actually present in main
  before assuming the merge is done.

## Deploying

```bash
cd cloud
./deploy/deploy.sh
```

What it does:
1. `gcloud builds submit` — uploads the `cloud/` tree, runs the multi-stage
   `Dockerfile`, pushes to Artifact Registry (`us-central1-docker.pkg.dev/bright-guard-prod/bright-guard/bright-guard:<ts>`).
2. `gcloud run deploy bright-guard` — rolls out a new revision with secrets
   from Secret Manager and Cloud SQL connector attached. Preserves existing
   env vars by reading them off the running revision.

### Env-var semantics you'll forget

- **Secrets are resolved at container start time.** Bumping a secret in
  Secret Manager (`gcloud secrets versions add ...`) does NOT propagate
  until a new revision is created. To force one: bump any env var
  (`--update-env-vars="OAUTH_BUMP=$(date +%s)"`). `--update-labels` does NOT
  force a new revision.
- **`--set-env-vars` replaces all env vars.** Use `--update-env-vars` to
  preserve existing ones.
- **`--set-env-vars` parses commas as delimiters.** Values containing commas
  need a custom delimiter: `--set-env-vars="^|^FOO=a,b,c"`. We hit this on
  `ALLOWED_HOSTS` and the Cloud-SQL `DATABASE_URL` (which has `@`).
- **`BASE_URL` override:** Set `BASE_URL=https://...` before calling
  `deploy.sh` to override the auto-resolved base URL.

### After every deploy

- `curl -sS https://mcp-governance.infoblox.dev/api/healthz` → expect `{"ok":true}`.
- Check `gcloud logging read ... "successfully migrated"` for new migrations.
- For UI changes, fetch `/assets/index-*.js` and `grep -oE` for the new
  strings you expect. Agents have shipped backend changes whose corresponding
  frontend changes silently failed to land — only the bundle inspection
  catches it.

## QA loop

After every wave deploys, launch a headless QA agent. Pattern:

- Unauthenticated regression checks (healthz, 401s on protected routes,
  redirect URIs on multi-host).
- New-route mount probes (call the new endpoints with bogus auth; expect 401,
  not 404 — that proves they're mounted).
- SPA bundle string presence checks (`grep -oE` for each new feature label,
  e.g. `DISABLED`, `ENFORCED`, `Add gateway`).
- Auth-required checks (`/api/me`, list endpoints with real bearer) are
  deferred to a human; the QA agent emits a device-flow `userCode` for the
  lead to approve, then can drive the rest if it has time.

QA findings get triaged in chat: bug → fix in the next deploy; tech debt →
new GH issue. The wave isn't done until QA is.

## Live testing via the device-code flow

`POST /oauth/device` returns a `userCode` you ask the human to approve at
`/device?code=<userCode>`. After approval, `POST /oauth/device/poll` returns a
bearer the agent uses for any subsequent API calls. This is how end-to-end
verifications get driven without dev-login (which is off in prod).

## Continuous demo data

A `bright-guard-shim-demo` Cloud Run service is always running with
`min-instances=1`, heartbeating against prod every 30s, emitting realistic
fake invocations into the `acme` org. Its credential lives in Secret Manager
as `bg-shim-bg-credential`. Costs ~$5/mo. Stop with
`gcloud run services update bright-guard-shim-demo --min-instances=0`.

If you change the shim's fake config or behavior:
1. Edit `cloud/shim/examples/fake-servers.yaml` (or the Go code).
2. `cd cloud/shim && ./deploy.sh` (multi-arch buildx; pushes
   `bright-guard-shim:latest`).
3. `gcloud run services update bright-guard-shim-demo --image=...latest` to
   roll it.

The one-shot seeder at `cloud/cli/seed/` posts ~575 invocations in a single
run; use it to backfill demo data quickly without waiting for the shim's
30-second cadence.

## What lives where in GCP

- **Project:** `bright-guard-prod` (number `806435112268`)
- **Region:** `us-central1`
- **Control plane:** Cloud Run service `bright-guard` (custom domain
  `mcp-governance.infoblox.dev`)
- **Demo shim:** Cloud Run service `bright-guard-shim-demo`
- **DB:** Cloud SQL Postgres `bright-guard-db` (~$15/mo standing cost; cheapest
  tier; stop with `gcloud sql instances patch ... --activation-policy=NEVER`)
- **Container registry:** Artifact Registry repo `bright-guard` in `us-central1`
- **Secrets:** Secret Manager — `session-secret`, `db-password`,
  `google-client-id`, `google-client-secret`, `bg-shim-bg-credential`
- **DNS:** custom domain mapped via Cloud Run domain mappings (CNAME →
  `ghs.googlehosted.com`)

## Issue / backlog hygiene

- Every shippable thing has a GH issue. `gh issue create` from chat is fine.
- Use the labels we've established: `priority:P0..P3`, `area:visibility`,
  `area:governance`, `area:enforcement`, `area:platform`, `area:simulation`,
  `kind:epic`, `kind:story`, `bug`.
- Milestones map to phases: `Phase 2: Visibility v1`, `Phase 3: Governance &
  Enforcement`, `Phase 4: Advanced Policy`, `Phase 5: Unified Control Plane`.
- Close the issue in the same PR/commit/deploy that lands the work.
- For partial progress on an epic, comment on it with the live revision +
  what's done + what's deferred. Don't close until the epic is whole.
- When a QA agent surfaces something P1+, file it as a new issue and link
  back from the wave's commit.

## Common bug shapes (so you don't have to rediscover them)

1. **`POST /api/orgs/null/gateways` 400.** The SPA renders before
   `activeOrgId` is set in AuthContext. AuthContext auto-promotes a sole
   membership to active, but if the cookie session somehow stayed null, every
   tenant page renders empty and any button-triggered POST sends `null` as
   the orgId. Fix: gate the button on `activeOrgId` non-null; ensure
   AuthContext auto-promote runs on `refresh()`.

2. **Postgres CASE expression infers `text` from NULL branch.** If you write
   `case when $1 then null else $2::sometype end` and `$2` is sent untyped,
   Postgres infers the column expression as `text`. Always cast explicitly:
   `null::uuid`, `null::timestamptz`, etc. Bit us on `mcp_capabilities.disabled_by`.

3. **Cloud Run intercepts `/healthz`.** Use `/api/healthz` instead — Google
   Frontend returns a generic 404 page for `/healthz` directly. Real bug we
   ate once.

4. **`/v1/gateway/register` is bearer-less.** It validates `enrollmentToken`
   from the body. An invalid Bearer header is silently ignored — 400 on
   missing body is correct, not 401.

5. **Background shim's enrollment token gets consumed.** If the shim crashes
   between claim and credential persist, the token is gone and the gateway
   is orphaned. Fixed by token-commit-on-first-heartbeat in
   `Gateways.CommitEnrollmentOnHeartbeat`.

6. **Worktree on stale base.** See above.

7. **`git apply --3way` silently no-ops.** Always grep for the agent's
   claimed new strings after applying; copy file directly if the apply was
   a lie.

## A canonical example wave

For reference, Wave N+5 — distributed enforcement — looked like:

1. File issue #35 with the full spec (heartbeat carries policy bundle,
   shim embeds cel-go, observations carry decisions, sweep skips decided
   rows, ActivityPage chip wording).
2. Launch one agent (the protocol is too coupled to parallelize) with
   `isolation: "worktree"`. Hard-rule: don't deploy.
3. Agent reports back: migration 00014, store + handler + shim changes,
   tests pass, npm build clean.
4. Lead: merge worktree (git apply + cp; verify by grepping); fix any
   missed lines from silent `git apply` no-ops.
5. Lead: `cloud/deploy/deploy.sh`. Confirm new revision serving + migration
   applied. Smoke test the new heartbeat headers + observation decisions.
6. Lead: close #35; comment on the epic (#13); update task list.
7. QA agent runs against the new revision; finds bugs (e.g. the `activeOrgId`
   null 400 on Add Gateway); lead files those as fresh issues, fixes the P0
   in a fast follow-up.

That's the rhythm.
