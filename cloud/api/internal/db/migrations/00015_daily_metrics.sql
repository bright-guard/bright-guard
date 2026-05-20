-- +goose Up
-- +goose StatementBegin
create table org_daily_metrics (
  org_id                 uuid not null references orgs(id) on delete cascade,
  day                    date not null,
  invocations_allowed    bigint not null default 0,
  invocations_audited    bigint not null default 0,
  invocations_denied     bigint not null default 0,
  new_callers            bigint not null default 0,
  new_servers            bigint not null default 0,
  total_servers          bigint not null default 0,
  total_capabilities     bigint not null default 0,
  public_exposure_count  bigint not null default 0,
  gateways_online        bigint not null default 0,
  posture_score          int    not null default 0,
  computed_at            timestamptz not null default now(),
  primary key (org_id, day)
);
create index org_daily_metrics_day_idx on org_daily_metrics(day);
create index org_daily_metrics_org_day_idx on org_daily_metrics(org_id, day desc);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists org_daily_metrics;
-- +goose StatementEnd
