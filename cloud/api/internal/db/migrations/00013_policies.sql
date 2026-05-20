-- +goose Up
-- +goose StatementBegin

create table policies (
  id           uuid primary key default gen_random_uuid(),
  org_id       uuid not null references orgs(id) on delete cascade,
  name         text not null,
  description  text not null default '',
  expression   text not null,                  -- CEL expression returning bool
  action       text not null default 'deny',   -- deny | warn
  enabled      boolean not null default true,
  created_by   uuid not null references users(id) on delete restrict,
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now(),
  unique (org_id, name)
);
create index policies_org_idx on policies(org_id) where enabled = true;

create table mcp_invocation_decisions (
  invocation_id uuid not null references mcp_invocations(id) on delete cascade,
  policy_id     uuid not null references policies(id) on delete cascade,
  matched       boolean not null,
  action        text not null,
  at            timestamptz not null default now(),
  primary key (invocation_id, policy_id)
);
create index inv_decisions_policy_idx on mcp_invocation_decisions(policy_id, at desc);

-- Per-org sweep watermark: the maximum invocations.at this org has finished
-- evaluating. Lets the sweep advance forward without re-scanning history.
create table policy_sweep_state (
  org_id      uuid primary key references orgs(id) on delete cascade,
  watermark   timestamptz not null default 'epoch'::timestamptz,
  updated_at  timestamptz not null default now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists policy_sweep_state;
drop index if exists inv_decisions_policy_idx;
drop table if exists mcp_invocation_decisions;
drop index if exists policies_org_idx;
drop table if exists policies;
-- +goose StatementEnd
