-- +goose Up
-- +goose StatementBegin
alter table gateway_enrollment_tokens add column issued_credential_id uuid references gateway_credentials(id) on delete set null;
alter table gateway_enrollment_tokens add column commit_pending boolean not null default false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
alter table gateway_enrollment_tokens drop column if exists commit_pending;
alter table gateway_enrollment_tokens drop column if exists issued_credential_id;
-- +goose StatementEnd
