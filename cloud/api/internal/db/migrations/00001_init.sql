-- +goose Up
-- +goose StatementBegin

create extension if not exists "pgcrypto";

create table users (
  id              uuid primary key default gen_random_uuid(),
  email           text not null unique,
  display_name    text not null default '',
  avatar_url      text not null default '',
  google_subject  text not null unique,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now()
);

create table orgs (
  id          uuid primary key default gen_random_uuid(),
  name        text not null,
  slug        text not null unique,
  created_by  uuid not null references users(id) on delete restrict,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);

create type org_role as enum ('owner', 'admin', 'member');

create table org_members (
  org_id     uuid not null references orgs(id) on delete cascade,
  user_id    uuid not null references users(id) on delete cascade,
  role       org_role not null default 'member',
  created_at timestamptz not null default now(),
  primary key (org_id, user_id)
);

create index org_members_user_idx on org_members(user_id);

create table sessions (
  id             uuid primary key default gen_random_uuid(),
  user_id        uuid not null references users(id) on delete cascade,
  active_org_id  uuid references orgs(id) on delete set null,
  created_at     timestamptz not null default now(),
  last_seen_at   timestamptz not null default now(),
  expires_at     timestamptz not null,
  user_agent     text not null default '',
  ip             inet
);

create index sessions_user_idx on sessions(user_id);
create index sessions_expires_idx on sessions(expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists sessions;
drop table if exists org_members;
drop type if exists org_role;
drop table if exists orgs;
drop table if exists users;
-- +goose StatementEnd
