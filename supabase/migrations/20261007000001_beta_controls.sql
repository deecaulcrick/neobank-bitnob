-- Pre-beta controls: transaction PIN, push notifications, invite list,
-- recipient screening and per-transaction reconciliation.

-- Transaction PIN. Only a bcrypt hash is stored; repeated wrong attempts
-- lock the PIN for a while.
alter table users add column pin_hash text;
alter table users add column pin_failed_attempts integer not null default 0;
alter table users add column pin_locked_until timestamptz;

-- Where to push to. A token belongs to whoever registered it last.
create table device_tokens (
  token       text primary key,
  user_id     uuid not null references users (id),
  platform    text not null,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index device_tokens_user_idx on device_tokens (user_id);

-- Outbox: a notification is written in the same transaction as the money
-- movement it reports, and sent afterwards by the worker.
create table notifications (
  id          bigint generated always as identity primary key,
  user_id     uuid not null references users (id),
  title       text not null,
  body        text not null,
  data        jsonb not null default '{}',
  created_at  timestamptz not null default now(),
  sent_at     timestamptz,
  attempts    integer not null default 0,
  last_error  text
);
create index notifications_unsent_idx on notifications (id) where sent_at is null;

-- Closed beta: phone numbers (digits only, with country code) allowed to
-- create an account while BETA_INVITE_ONLY is on.
create table beta_invites (
  phone       text primary key check (phone ~ '^[0-9]{8,15}$'),
  note        text,
  created_at  timestamptz not null default now(),
  claimed_at  timestamptz
);

-- Names we refuse to pay out to. This is our own list, checked on every
-- payout; it is not a substitute for a sanctions-screening provider.
create table screening_blocklist (
  id          bigint generated always as identity primary key,
  name        text not null,
  -- Upper-cased, letters and digits only, words sorted: "ADA OBI" = "Obi, Ada".
  name_key    text not null unique,
  reason      text,
  created_at  timestamptz not null default now()
);

-- What the screen said about a recipient when a payout was sent.
alter table beneficiaries add column screening_result text;

-- One row per Bitnob transaction per reconciliation run, with the ledger
-- record it was matched to (if any), plus one row per recent ledger record
-- Bitnob showed nothing for.
create table reconciliation_transactions (
  id          bigint generated always as identity primary key,
  ran_at      timestamptz not null,
  -- matched | unmatched_bitnob | unmatched_ledger | duplicate
  result      text not null,
  bitnob_transaction_id text,
  bitnob_type text,
  currency    text,
  amount      bigint,
  reference   text,
  ledger_kind text,   -- deposit | payout | trade | crypto_transfer
  ledger_id   uuid
);
create index reconciliation_transactions_run_idx on reconciliation_transactions (ran_at desc, result);

alter table device_tokens enable row level security;
alter table notifications enable row level security;
alter table beta_invites enable row level security;
alter table screening_blocklist enable row level security;
alter table reconciliation_transactions enable row level security;
