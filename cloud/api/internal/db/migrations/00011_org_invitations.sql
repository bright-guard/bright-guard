-- +goose Up
-- +goose StatementBegin

create table org_invitations (
  id           uuid primary key default gen_random_uuid(),
  org_id       uuid not null references orgs(id) on delete cascade,
  email        text not null,
  invited_by   uuid not null references users(id) on delete restrict,
  role         org_role not null default 'member',
  status       text not null default 'pending',
  accepted_at  timestamptz,
  declined_at  timestamptz,
  created_at   timestamptz not null default now(),
  expires_at   timestamptz not null
);
create unique index org_invitations_pending_unique
  on org_invitations(org_id, lower(email)) where status = 'pending';
create index org_invitations_email_pending_idx
  on org_invitations(lower(email)) where status = 'pending';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists org_invitations;
-- +goose StatementEnd
