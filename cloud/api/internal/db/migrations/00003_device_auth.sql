-- +goose Up
-- +goose StatementBegin

alter table sessions add column kind  text not null default 'cookie';
alter table sessions add column label text not null default '';
alter table sessions add column token_hash bytea;

create index sessions_token_hash_idx on sessions(token_hash) where token_hash is not null;

create table device_authorizations (
  id            uuid primary key default gen_random_uuid(),
  user_code     text not null unique,
  device_hash   bytea not null,
  client_label  text not null default '',
  status        text not null default 'pending',
  user_id       uuid references users(id) on delete set null,
  session_id    uuid references sessions(id) on delete set null,
  -- Bearer secret stashed at approve time; removed when the CLI polls
  -- successfully (single-use), or naturally when the row is deleted on consume.
  bearer_secret text,
  approved_at   timestamptz,
  expires_at    timestamptz not null,
  created_at    timestamptz not null default now()
);

create index device_auth_status_idx on device_authorizations(status, expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists device_authorizations;
drop index if exists sessions_token_hash_idx;
alter table sessions drop column if exists token_hash;
alter table sessions drop column if exists label;
alter table sessions drop column if exists kind;
-- +goose StatementEnd
