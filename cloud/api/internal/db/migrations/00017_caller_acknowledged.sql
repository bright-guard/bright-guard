-- +goose Up
-- +goose StatementBegin

-- Distinguishes "explicitly acknowledged by a human" from "aged out of the
-- flagged_new window". Existing callers row that already has flagged_new=false
-- left as null so the API can tell "unknown / pre-migration" from "acknowledged
-- after this date". Bundle delivery exposes acknowledged_at IS NOT NULL as
-- caller.acknowledged in the shim CEL env.
alter table org_callers add column acknowledged_at timestamptz;

-- Bump bundle version on any caller acknowledgement so the heartbeat-delivered
-- caller snapshot stays fresh. Without this an acknowledged caller would still
-- appear flagged in the shim's local cache until the next caller insert.
create or replace function bump_org_policy_bundle_version_from_callers() returns trigger
language plpgsql as $$
begin
  if (TG_OP = 'UPDATE'
      and (OLD.acknowledged_at is distinct from NEW.acknowledged_at
        or OLD.flagged_new is distinct from NEW.flagged_new))
     or TG_OP = 'INSERT' or TG_OP = 'DELETE' then
    update orgs set policy_bundle_version = policy_bundle_version + 1
     where id = coalesce(NEW.org_id, OLD.org_id);
  end if;
  return coalesce(NEW, OLD);
end;
$$;

create trigger callers_bump_bundle_version
after insert or update or delete on org_callers
for each row execute function bump_org_policy_bundle_version_from_callers();

-- Server reclassification (UC8) must also force a new bundle so the shim's
-- cached exposure_state stays correct.
create or replace function bump_org_policy_bundle_version_from_servers() returns trigger
language plpgsql as $$
begin
  if TG_OP = 'INSERT' or TG_OP = 'DELETE'
     or OLD.exposure_state is distinct from NEW.exposure_state
     or OLD.name is distinct from NEW.name
     or OLD.address is distinct from NEW.address then
    update orgs set policy_bundle_version = policy_bundle_version + 1
     where id = coalesce(NEW.org_id, OLD.org_id);
  end if;
  return coalesce(NEW, OLD);
end;
$$;

create trigger servers_bump_bundle_version
after insert or update or delete on mcp_servers
for each row execute function bump_org_policy_bundle_version_from_servers();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop trigger if exists servers_bump_bundle_version on mcp_servers;
drop function if exists bump_org_policy_bundle_version_from_servers();
drop trigger if exists callers_bump_bundle_version on org_callers;
drop function if exists bump_org_policy_bundle_version_from_callers();
alter table org_callers drop column if exists acknowledged_at;
-- +goose StatementEnd
