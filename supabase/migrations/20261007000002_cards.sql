-- Virtual dollar cards. A card's balance lives at the issuer (through Bitnob)
-- and is funded from our USDC there, so users load it from their USDC
-- balance. The ledger records money moving to and from the card; the card's
-- own balance is read live.

-- Card KYC is a second, fuller check with its own Bitnob customer id.
alter table users add column card_customer_id text;
alter table users add column card_kyc_status text
  check (card_kyc_status in ('pending', 'approved', 'rejected'));

create table cards (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users (id),
  bitnob_card_id text unique,
  -- pending | active | frozen | terminated | failed
  status      text not null default 'pending'
              check (status in ('pending', 'active', 'frozen', 'terminated', 'failed')),
  brand       text,
  last4       text,
  name        text not null,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
-- One live card per user for now.
create unique index cards_one_live_idx on cards (user_id) where status in ('pending', 'active', 'frozen');

-- Money moving between a user's USDC balance and their card. Amounts are
-- micro-dollars (the same unit as USDC and as Bitnob's card API).
create table card_transfers (
  id          uuid primary key,
  user_id     uuid not null references users (id),
  card_id     uuid not null references cards (id),
  kind        text not null check (kind in ('create', 'fund', 'withdraw')),
  amount      bigint not null check (amount > 0),
  -- What we charge the user, and what Bitnob took, for this movement.
  fee         bigint not null default 0 check (fee >= 0),
  cost        bigint not null default 0 check (cost >= 0),
  status      text not null default 'pending' check (status in ('pending', 'success', 'failed')),
  hold_entry_id   uuid references journal_entries (id),
  settle_entry_id uuid references journal_entries (id),
  failure_reason text,
  raw         jsonb,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index card_transfers_user_idx on card_transfers (user_id, created_at desc);
create index card_transfers_open_idx on card_transfers (created_at) where status = 'pending';

create trigger cards_touch before update on cards
  for each row execute function touch_updated_at();
create trigger card_transfers_touch before update on card_transfers
  for each row execute function touch_updated_at();

alter table cards enable row level security;
alter table card_transfers enable row level security;
