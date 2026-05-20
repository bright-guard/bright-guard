-- +goose Up
-- +goose StatementBegin
alter table mcp_servers add column exposure_state text not null default 'unknown';
alter table mcp_servers add column exposure_reason text not null default '';
alter table mcp_servers add column exposure_classified_at timestamptz;
create index mcp_servers_exposure_idx on mcp_servers(org_id, exposure_state) where exposure_state in ('public', 'unknown');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index if exists mcp_servers_exposure_idx;
alter table mcp_servers drop column if exists exposure_classified_at;
alter table mcp_servers drop column if exists exposure_reason;
alter table mcp_servers drop column if exists exposure_state;
-- +goose StatementEnd
