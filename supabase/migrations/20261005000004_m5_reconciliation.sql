-- M5: daily reconciliation. One row per asset per run compares what our
-- ledger says Bitnob holds for us with what Bitnob reports. Differences are
-- recorded for ops; nothing is auto-corrected.

create table reconciliation_reports (
  id          bigint generated always as identity primary key,
  ran_at      timestamptz not null default now(),
  asset       asset not null,
  -- -balance(omnibus:{asset}): what the ledger expects Bitnob to hold.
  ledger_amount bigint not null,
  -- Null when Bitnob did not report this asset in that run.
  bitnob_amount bigint,
  difference  bigint generated always as (bitnob_amount - ledger_amount) stored,
  -- User, pending, revenue and suspense balances, for the spec's identity.
  liabilities bigint not null,
  notes       text
);
create index reconciliation_reports_ran_idx on reconciliation_reports (ran_at desc);
alter table reconciliation_reports enable row level security;

-- Lets the send screen find people by tag prefix quickly.
create index users_tag_prefix_idx on users ((tag::text) text_pattern_ops);
