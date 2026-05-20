-- +goose Up
-- +goose StatementBegin
alter table mcp_capabilities add column enabled boolean not null default true;
alter table mcp_capabilities add column disabled_at timestamptz;
alter table mcp_capabilities add column disabled_by uuid references users(id) on delete set null;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table mcp_capabilities drop column if exists disabled_by;
alter table mcp_capabilities drop column if exists disabled_at;
alter table mcp_capabilities drop column if exists enabled;
-- +goose StatementEnd
