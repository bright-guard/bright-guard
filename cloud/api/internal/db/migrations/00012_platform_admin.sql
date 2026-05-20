-- +goose Up
-- +goose StatementBegin

create table platform_admins (
  user_id     uuid primary key references users(id) on delete cascade,
  added_by    uuid references users(id) on delete set null,
  added_at    timestamptz not null default now(),
  revoked_at  timestamptz
);
create index platform_admins_active_idx on platform_admins(user_id) where revoked_at is null;

create table platform_audit_log (
  id          uuid primary key default gen_random_uuid(),
  actor_id    uuid not null references users(id),
  action      text not null,
  target_kind text not null,
  target_id   uuid not null,
  details     jsonb not null default '{}'::jsonb,
  at          timestamptz not null default now()
);
create index platform_audit_at_idx on platform_audit_log(at desc);
create index platform_audit_actor_idx on platform_audit_log(actor_id, at desc);

-- For suspending users
alter table users add column suspended_at timestamptz;
alter table users add column suspended_by uuid references users(id) on delete set null;

-- For suspending orgs
alter table orgs add column suspended_at timestamptz;
alter table orgs add column suspended_by uuid references users(id) on delete set null;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table orgs drop column if exists suspended_by;
alter table orgs drop column if exists suspended_at;
alter table users drop column if exists suspended_by;
alter table users drop column if exists suspended_at;
drop table if exists platform_audit_log;
drop table if exists platform_admins;
-- +goose StatementEnd
