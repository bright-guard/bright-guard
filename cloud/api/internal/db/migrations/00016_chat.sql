-- +goose Up
-- +goose StatementBegin
create table chat_sessions (
  id              uuid primary key default gen_random_uuid(),
  org_id          uuid not null references orgs(id) on delete cascade,
  user_id         uuid not null references users(id) on delete cascade,
  title           text,
  total_tokens    bigint not null default 0,
  created_at      timestamptz not null default now(),
  last_message_at timestamptz not null default now()
);
create index chat_sessions_org_user_idx on chat_sessions(org_id, user_id, last_message_at desc);

create table chat_messages (
  id              uuid primary key default gen_random_uuid(),
  session_id      uuid not null references chat_sessions(id) on delete cascade,
  role            text not null,
  content         jsonb not null,
  input_tokens    int  not null default 0,
  output_tokens   int  not null default 0,
  tool_calls      jsonb,
  created_at      timestamptz not null default now()
);
create index chat_messages_session_idx on chat_messages(session_id, created_at);

-- Per-org per-day token usage for budget enforcement. Updated per-message;
-- separate from org_daily_metrics because the rollup there is hourly and chat
-- needs immediate writes.
create table chat_daily_usage (
  org_id        uuid not null references orgs(id) on delete cascade,
  day           date not null,
  input_tokens  bigint not null default 0,
  output_tokens bigint not null default 0,
  primary key (org_id, day)
);
create index chat_daily_usage_day_idx on chat_daily_usage(day);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists chat_daily_usage;
drop table if exists chat_messages;
drop table if exists chat_sessions;
-- +goose StatementEnd
