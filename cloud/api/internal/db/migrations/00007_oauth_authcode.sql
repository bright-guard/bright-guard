-- +goose Up
-- +goose StatementBegin

-- Ephemeral PKCE state for the OAuth2 authorization-code dance. Rows live
-- between /authorize and /callback (a few minutes); a periodic sweep can
-- delete expired rows but the lookup also checks expires_at on every call.
create table oauth_authcode_states (
  state         text primary key,
  connection_id uuid not null references mcp_connections(id) on delete cascade,
  org_id        uuid not null references orgs(id) on delete cascade,
  user_id       uuid not null references users(id) on delete cascade,
  code_verifier bytea not null,
  return_to     text not null default '',
  created_at    timestamptz not null default now(),
  expires_at    timestamptz not null
);
create index oauth_authcode_expires_idx on oauth_authcode_states(expires_at);

-- '' (n/a), 'pending_authorize', 'authorized', 'expired_refresh', 'needs_reauth'.
alter table mcp_connections add column oauth_status text not null default '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table mcp_connections drop column if exists oauth_status;
drop table if exists oauth_authcode_states;
-- +goose StatementEnd
