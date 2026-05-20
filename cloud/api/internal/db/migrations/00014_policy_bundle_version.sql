-- +goose Up
-- +goose StatementBegin
alter table orgs add column policy_bundle_version bigint not null default 0;

create or replace function bump_org_policy_bundle_version() returns trigger
language plpgsql as $$
begin
  update orgs set policy_bundle_version = policy_bundle_version + 1
   where id = coalesce(NEW.org_id, OLD.org_id);
  return coalesce(NEW, OLD);
end;
$$;

create trigger policies_bump_bundle_version
after insert or update or delete on policies
for each row execute function bump_org_policy_bundle_version();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop trigger if exists policies_bump_bundle_version on policies;
drop function if exists bump_org_policy_bundle_version();
alter table orgs drop column if exists policy_bundle_version;
-- +goose StatementEnd
