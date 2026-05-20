-- +goose Up
-- +goose StatementBegin

create table gateways (
  id           uuid primary key default gen_random_uuid(),
  org_id       uuid not null references orgs(id) on delete cascade,
  name         text not null,
  description  text not null default '',
  status       text not null default 'pending',
  last_seen_at timestamptz,
  created_by   uuid not null references users(id) on delete restrict,
  created_at   timestamptz not null default now(),
  unique (org_id, name)
);
create index gateways_org_idx on gateways(org_id);

create table gateway_credentials (
  id          uuid primary key default gen_random_uuid(),
  gateway_id  uuid not null references gateways(id) on delete cascade,
  secret_hash bytea not null,
  revoked_at  timestamptz,
  created_at  timestamptz not null default now()
);
create index gateway_creds_gw_idx on gateway_credentials(gateway_id);

create table gateway_enrollment_tokens (
  id           uuid primary key default gen_random_uuid(),
  org_id       uuid not null references orgs(id) on delete cascade,
  gateway_id   uuid not null references gateways(id) on delete cascade,
  token_hash   bytea not null,
  expires_at   timestamptz not null,
  claimed_at   timestamptz,
  created_by   uuid not null references users(id) on delete restrict,
  created_at   timestamptz not null default now()
);
create index gw_enroll_gw_idx on gateway_enrollment_tokens(gateway_id);

create table mcp_servers (
  id            uuid primary key default gen_random_uuid(),
  org_id        uuid not null references orgs(id) on delete cascade,
  gateway_id    uuid not null references gateways(id) on delete cascade,
  name          text not null,
  address       text not null default '',
  transport     text not null default '',
  version       text not null default '',
  metadata      jsonb not null default '{}'::jsonb,
  first_seen_at timestamptz not null default now(),
  last_seen_at  timestamptz not null default now(),
  unique (gateway_id, name)
);
create index mcp_servers_org_idx on mcp_servers(org_id, last_seen_at desc);

create table mcp_capabilities (
  id            uuid primary key default gen_random_uuid(),
  mcp_server_id uuid not null references mcp_servers(id) on delete cascade,
  kind          text not null,
  name          text not null,
  description   text not null default '',
  schema        jsonb not null default '{}'::jsonb,
  first_seen_at timestamptz not null default now(),
  last_seen_at  timestamptz not null default now(),
  unique (mcp_server_id, kind, name)
);
create index caps_server_idx on mcp_capabilities(mcp_server_id);

create table mcp_invocations (
  id              uuid primary key default gen_random_uuid(),
  org_id          uuid not null references orgs(id) on delete cascade,
  mcp_server_id   uuid not null references mcp_servers(id) on delete cascade,
  capability_id   uuid references mcp_capabilities(id) on delete set null,
  capability_kind text not null default '',
  capability_name text not null default '',
  caller          jsonb not null default '{}'::jsonb,
  status          text not null default 'ok',
  latency_ms      integer not null default 0,
  at              timestamptz not null default now()
);
create index inv_org_at_idx on mcp_invocations(org_id, at desc);
create index inv_server_at_idx on mcp_invocations(mcp_server_id, at desc);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists mcp_invocations;
drop table if exists mcp_capabilities;
drop table if exists mcp_servers;
drop table if exists gateway_enrollment_tokens;
drop table if exists gateway_credentials;
drop table if exists gateways;
-- +goose StatementEnd
