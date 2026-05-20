-- +goose Up
-- +goose StatementBegin

-- Wave N+9 (UC6 + UC7): per-invocation workload / network context. All
-- columns nullable — older invocations from pre-N+9 shims have no such
-- context, and the CEL env treats missing values as empty strings so any
-- policy referencing workload.* or network.* on those rows is a non-match.
alter table mcp_invocations
  add column workload_host      text,
  add column workload_cluster   text,
  add column workload_namespace text,
  add column agent_class        text,
  add column network_subnet     text,
  add column network_vpc        text,
  add column network_zone       text,
  add column caller_ip          text;

-- Partial indexes (ignore rows where the column is null) so policy/audit
-- queries that filter on cluster or subnet stay cheap as the table grows.
create index mcp_invocations_workload_cluster_idx
  on mcp_invocations(org_id, workload_cluster)
  where workload_cluster is not null;

create index mcp_invocations_network_subnet_idx
  on mcp_invocations(org_id, network_subnet)
  where network_subnet is not null;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop index if exists mcp_invocations_network_subnet_idx;
drop index if exists mcp_invocations_workload_cluster_idx;
alter table mcp_invocations
  drop column if exists caller_ip,
  drop column if exists network_zone,
  drop column if exists network_vpc,
  drop column if exists network_subnet,
  drop column if exists agent_class,
  drop column if exists workload_namespace,
  drop column if exists workload_cluster,
  drop column if exists workload_host;
-- +goose StatementEnd
