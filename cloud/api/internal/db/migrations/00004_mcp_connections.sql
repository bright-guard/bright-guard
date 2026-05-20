-- +goose Up
-- +goose StatementBegin

create table mcp_connections (
  id                 uuid primary key default gen_random_uuid(),
  org_id             uuid not null references orgs(id) on delete cascade,
  name               text not null,
  endpoint_url       text not null,
  transport          text not null,                       -- streamable-http | sse | http
  auth_method        text not null,                       -- api_key_header | bearer | basic | oauth2_authcode
  auth_state         bytea,                               -- AEAD-encrypted auth secret JSON; oauth2 not implemented yet
  status             text not null default 'pending',     -- pending | healthy | error | unauthorized
  last_error         text not null default '',
  last_discovered_at timestamptz,
  mcp_server_id      uuid references mcp_servers(id) on delete set null,
  created_by         uuid not null references users(id) on delete restrict,
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now(),
  unique (org_id, name)
);
create index mcp_connections_org_idx on mcp_connections(org_id);
create index mcp_connections_status_idx on mcp_connections(status, last_discovered_at);

-- mcp_servers can now originate from a direct connection (no gateway). Each row
-- has exactly one source: either gateway_id or connection_id.
alter table mcp_servers alter column gateway_id drop not null;
alter table mcp_servers add column connection_id uuid references mcp_connections(id) on delete cascade;
alter table mcp_servers add constraint mcp_servers_source_chk
  check ((gateway_id is null) <> (connection_id is null));

-- The old (gateway_id, name) unique constraint can't represent connection-sourced
-- servers. Replace with two partial unique indexes, one per source.
alter table mcp_servers drop constraint mcp_servers_gateway_id_name_key;
create unique index mcp_servers_gateway_unique
  on mcp_servers(gateway_id, name) where gateway_id is not null;
create unique index mcp_servers_connection_unique
  on mcp_servers(connection_id, name) where connection_id is not null;

create index mcp_servers_connection_idx on mcp_servers(connection_id) where connection_id is not null;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

drop index if exists mcp_servers_connection_idx;
drop index if exists mcp_servers_connection_unique;
drop index if exists mcp_servers_gateway_unique;
alter table mcp_servers drop constraint if exists mcp_servers_source_chk;
alter table mcp_servers drop column if exists connection_id;
-- Restore gateway_id as NOT NULL only if no NULL rows remain; otherwise the
-- migration can't be cleanly reversed without losing direct-discovered servers.
alter table mcp_servers alter column gateway_id set not null;
alter table mcp_servers add constraint mcp_servers_gateway_id_name_key unique (gateway_id, name);
drop table if exists mcp_connections;

-- +goose StatementEnd
