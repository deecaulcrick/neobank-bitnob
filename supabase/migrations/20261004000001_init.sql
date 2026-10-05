-- Neobank v1 schema. See docs/spec.md ("Ledger and data model").
--
-- Access model: the mobile app talks to Supabase for auth only. Every table
-- here has RLS enabled with no policies, so the anon/authenticated roles can
-- read nothing through PostgREST; the Go backend connects with the database
-- connection string and is the only reader and writer.
--
-- Money: every amount is a bigint in the asset's smallest unit
-- (kobo, micro-USDT/USDC, sats). Never floats, never numeric with a scale.

create extension if not exists citext with schema extensions;

-- ---------------------------------------------------------------------------
-- Enums
-- ---------------------------------------------------------------------------

create type asset as enum ('NGN', 'USDT', 'USDC', 'BTC');

create type ledger_account_kind as enum (
  'user',            -- user:{id}:{asset}
  'user_pending',    -- user:{id}:{asset}:pending
  'omnibus',         -- omnibus:{asset}
  'revenue_fees',    -- revenue:fees:{asset}
  'revenue_spread',  -- revenue:spread:{asset}
  'suspense'         -- suspense:{asset}
);

create type kyc_status as enum ('pending', 'approved', 'rejected');
create type quote_kind as enum ('swap', 'payout');
create type trade_status as enum ('pending', 'completed', 'failed');
create type payout_status as enum ('quoted', 'initialized', 'processing', 'success', 'expired', 'failed');
create type payout_rail as enum ('bank', 'mobile_money');
create type crypto_direction as enum ('deposit', 'withdrawal');
create type crypto_transfer_status as enum ('pending', 'success', 'failed');

-- ---------------------------------------------------------------------------
-- Users and identity
-- ---------------------------------------------------------------------------

create table users (
  id          uuid primary key references auth.users (id) on delete restrict,
  phone       text not null unique,
  tag         extensions.citext unique check (tag ~ '^[a-z0-9_]{3,20}$'),
  first_name  text,
  last_name   text,
  date_of_birth date,
  kyc_tier    smallint not null default 0 check (kyc_tier between 0 and 3),
  display_currency text not null default 'NGN' check (display_currency in ('NGN', 'USD')),
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);

create table kyc_records (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users (id),
  tier        smallint not null check (tier between 1 and 3),
  id_type     text not null check (id_type in ('bvn', 'nin', 'id_document', 'address', 'selfie')),
  -- Store a provider reference or a hash, never the raw BVN/NIN.
  id_reference text,
  status      kyc_status not null default 'pending',
  provider_payload jsonb,
  created_at  timestamptz not null default now(),
  reviewed_at timestamptz
);
create index kyc_records_user_idx on kyc_records (user_id);

create table bitnob_customers (
  user_id     uuid primary key references users (id),
  bitnob_customer_id text not null unique,
  raw         jsonb,
  created_at  timestamptz not null default now()
);

create table virtual_accounts (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null unique references users (id),
  bitnob_account_id text unique,
  account_number text not null unique,
  account_name text not null,
  bank_name   text not null,
  currency    asset not null default 'NGN',
  raw         jsonb,
  created_at  timestamptz not null default now()
);

-- ---------------------------------------------------------------------------
-- Ledger
--
-- Sign convention: postings.amount > 0 is a credit, < 0 is a debit, and every
-- journal entry sums to zero per asset. User, pending, revenue and suspense
-- accounts are credit-normal (positive balance = we owe / we earned).
-- omnibus:{asset} is debit-normal, so its balance is negative and
-- -balance is what Bitnob should be holding for us.
-- ---------------------------------------------------------------------------

create table ledger_accounts (
  id          uuid primary key default gen_random_uuid(),
  code        text not null unique,
  kind        ledger_account_kind not null,
  asset       asset not null,
  user_id     uuid references users (id),
  balance     bigint not null default 0,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now(),
  unique (id, asset),
  check ((kind in ('user', 'user_pending')) = (user_id is not null)),
  -- Users can never be overdrawn; system accounts may go either way.
  check (kind not in ('user', 'user_pending') or balance >= 0)
);
create unique index ledger_accounts_owner_idx
  on ledger_accounts (user_id, asset, kind) where user_id is not null;
create unique index ledger_accounts_system_idx
  on ledger_accounts (kind, asset) where user_id is null;

create table journal_entries (
  id          uuid primary key default gen_random_uuid(),
  idempotency_key text not null unique,
  kind        text not null,  -- ngn_deposit, swap, p2p_send, payout_hold, payout_success, ...
  description text,
  metadata    jsonb not null default '{}',
  created_at  timestamptz not null default now()
);
create index journal_entries_created_idx on journal_entries (created_at desc);

create table postings (
  id          bigint generated always as identity primary key,
  entry_id    uuid not null references journal_entries (id),
  account_id  uuid not null,
  asset       asset not null,
  amount      bigint not null check (amount <> 0),
  created_at  timestamptz not null default now(),
  foreign key (account_id, asset) references ledger_accounts (id, asset)
);
create index postings_entry_idx on postings (entry_id);
create index postings_account_idx on postings (account_id, id desc);

-- Postings must sum to zero per entry and asset. Deferred so all lines of an
-- entry can be inserted before the check runs at commit.
create function check_entry_balanced() returns trigger
language plpgsql as $$
begin
  if exists (
    select 1 from postings
    where entry_id = new.entry_id
    group by asset
    having sum(amount) <> 0
  ) then
    raise exception 'journal entry % is not balanced', new.entry_id;
  end if;
  return null;
end;
$$;

create constraint trigger postings_balanced
  after insert on postings
  deferrable initially deferred
  for each row execute function check_entry_balanced();

-- The journal is append-only: corrections are new reversing entries.
create function forbid_mutation() returns trigger
language plpgsql as $$
begin
  raise exception '% on % is not allowed; post a reversing entry', tg_op, tg_table_name;
end;
$$;

create trigger postings_immutable
  before update or delete on postings
  for each row execute function forbid_mutation();

create trigger journal_entries_immutable
  before update or delete on journal_entries
  for each row execute function forbid_mutation();

-- System accounts exist from day one.
insert into ledger_accounts (code, kind, asset)
select
  case k
    when 'omnibus' then 'omnibus:' || a
    when 'revenue_fees' then 'revenue:fees:' || a
    when 'revenue_spread' then 'revenue:spread:' || a
    when 'suspense' then 'suspense:' || a
  end,
  k::ledger_account_kind,
  a::asset
from unnest(array['omnibus', 'revenue_fees', 'revenue_spread', 'suspense']) as k,
     unnest(enum_range(null::asset)::text[]) as a;

-- ---------------------------------------------------------------------------
-- Money flows
-- ---------------------------------------------------------------------------

create table quotes (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users (id),
  kind        quote_kind not null,
  bitnob_quote_id text unique,
  from_asset  asset not null,
  from_amount bigint not null check (from_amount > 0),
  -- Swaps settle into one of our assets; payouts into a destination currency
  -- (GHS, KES, ...) that we never hold, hence text + minor units.
  to_currency text not null,
  to_amount   bigint not null check (to_amount > 0),
  rate        numeric not null,          -- display only; amounts are authoritative
  fee_asset   asset not null,
  fee_amount  bigint not null default 0 check (fee_amount >= 0),
  expires_at  timestamptz not null,
  consumed_at timestamptz,
  raw         jsonb,
  created_at  timestamptz not null default now()
);
create index quotes_user_idx on quotes (user_id, created_at desc);

create table trades (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users (id),
  quote_id    uuid not null unique references quotes (id),
  bitnob_order_id text unique,
  status      trade_status not null default 'pending',
  entry_id    uuid references journal_entries (id),
  raw         jsonb,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index trades_open_idx on trades (created_at) where status = 'pending';

create table beneficiaries (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users (id),
  country     text not null,             -- ISO 3166-1 alpha-2
  currency    text not null,
  rail        payout_rail not null,
  display_name text not null,
  -- Shape comes from Bitnob's Get Country Details, so it stays schemaless.
  details     jsonb not null,
  screened_at timestamptz,               -- sanctions / name screening
  created_at  timestamptz not null default now()
);
create index beneficiaries_user_idx on beneficiaries (user_id);

create table payouts (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users (id),
  quote_id    uuid not null unique references quotes (id),
  beneficiary_id uuid not null references beneficiaries (id),
  -- Set when a swap is chained in front of the payout (spec flow 5).
  pre_swap_trade_id uuid references trades (id),
  bitnob_payout_id text unique,
  reference   text not null unique,
  status      payout_status not null default 'quoted',
  hold_entry_id   uuid references journal_entries (id),
  settle_entry_id uuid references journal_entries (id),
  raw         jsonb,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index payouts_user_idx on payouts (user_id, created_at desc);
create index payouts_open_idx on payouts (updated_at)
  where status in ('initialized', 'processing');

create table crypto_addresses (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users (id),
  asset       asset not null check (asset <> 'NGN'),
  network     text not null,             -- tron, polygon, solana, ethereum, bitcoin, lightning
  address     text not null,
  bitnob_address_id text unique,
  created_at  timestamptz not null default now(),
  unique (network, address)
);
create index crypto_addresses_user_idx on crypto_addresses (user_id, asset, network);

create table crypto_transfers (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid references users (id), -- null until an unknown deposit is matched
  direction   crypto_direction not null,
  asset       asset not null check (asset <> 'NGN'),
  network     text not null,
  address     text not null,
  amount      bigint not null check (amount > 0),
  network_fee bigint not null default 0 check (network_fee >= 0),
  service_fee bigint not null default 0 check (service_fee >= 0),
  tx_hash     text,
  bitnob_transaction_id text unique,
  status      crypto_transfer_status not null default 'pending',
  entry_id    uuid references journal_entries (id),
  raw         jsonb,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index crypto_transfers_user_idx on crypto_transfers (user_id, created_at desc);

-- Not in the spec's table list, but the webhook worker needs a row to update
-- for an NGN deposit, and in-app sends need one for the Activity feed.
create table deposits (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid references users (id), -- null => unmatched, sits in suspense
  virtual_account_id uuid references virtual_accounts (id),
  amount      bigint not null check (amount > 0),
  asset       asset not null default 'NGN',
  bitnob_transaction_id text not null unique,
  sender_name text,
  sender_bank text,
  entry_id    uuid references journal_entries (id),
  raw         jsonb,
  created_at  timestamptz not null default now()
);
create index deposits_user_idx on deposits (user_id, created_at desc);

create table p2p_transfers (
  id          uuid primary key default gen_random_uuid(),
  sender_id   uuid not null references users (id),
  receiver_id uuid not null references users (id),
  asset       asset not null,
  amount      bigint not null check (amount > 0),
  note        text,
  entry_id    uuid not null references journal_entries (id),
  created_at  timestamptz not null default now(),
  check (sender_id <> receiver_id)
);
create index p2p_transfers_sender_idx on p2p_transfers (sender_id, created_at desc);
create index p2p_transfers_receiver_idx on p2p_transfers (receiver_id, created_at desc);

-- ---------------------------------------------------------------------------
-- Webhooks: store first, process from the table (it doubles as the queue).
-- ---------------------------------------------------------------------------

create table webhook_events (
  id          bigint generated always as identity primary key,
  event_id    text not null unique,
  event       text not null,
  payload     jsonb not null,
  received_at timestamptz not null default now(),
  processed_at timestamptz,
  attempts    integer not null default 0,
  next_attempt_at timestamptz not null default now(),
  last_error  text
);
create index webhook_events_queue_idx
  on webhook_events (next_attempt_at) where processed_at is null;

-- ---------------------------------------------------------------------------
-- Housekeeping
-- ---------------------------------------------------------------------------

create function touch_updated_at() returns trigger
language plpgsql as $$
begin
  new.updated_at = now();
  return new;
end;
$$;

create trigger users_touch before update on users
  for each row execute function touch_updated_at();
create trigger ledger_accounts_touch before update on ledger_accounts
  for each row execute function touch_updated_at();
create trigger trades_touch before update on trades
  for each row execute function touch_updated_at();
create trigger payouts_touch before update on payouts
  for each row execute function touch_updated_at();
create trigger crypto_transfers_touch before update on crypto_transfers
  for each row execute function touch_updated_at();

alter table users enable row level security;
alter table kyc_records enable row level security;
alter table bitnob_customers enable row level security;
alter table virtual_accounts enable row level security;
alter table ledger_accounts enable row level security;
alter table journal_entries enable row level security;
alter table postings enable row level security;
alter table quotes enable row level security;
alter table trades enable row level security;
alter table beneficiaries enable row level security;
alter table payouts enable row level security;
alter table crypto_addresses enable row level security;
alter table crypto_transfers enable row level security;
alter table deposits enable row level security;
alter table p2p_transfers enable row level security;
alter table webhook_events enable row level security;
