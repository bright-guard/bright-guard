-- +goose Up
-- +goose StatementBegin

create table org_callers (
  id                uuid primary key default gen_random_uuid(),
  org_id            uuid not null references orgs(id) on delete cascade,
  signature         text not null,
  label             text not null default '',
  caller            jsonb not null default '{}'::jsonb,
  first_seen_at     timestamptz not null,
  last_seen_at      timestamptz not null,
  invocation_count  bigint not null default 0,
  flagged_new       boolean not null default true,
  unique (org_id, signature)
);
create index org_callers_org_idx on org_callers(org_id, last_seen_at desc);
create index org_callers_flagged_idx on org_callers(org_id, flagged_new) where flagged_new = true;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists org_callers;
-- +goose StatementEnd
