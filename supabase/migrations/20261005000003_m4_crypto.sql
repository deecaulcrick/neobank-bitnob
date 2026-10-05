-- M4: crypto in/out. One Bitnob address per user per network receives every
-- asset on that network (an EVM address takes both USDT and USDC), so an
-- address is no longer tied to a single asset.

alter table crypto_addresses alter column asset drop not null;
create unique index crypto_addresses_user_network_idx on crypto_addresses (user_id, network);

-- Why a withdrawal did not complete.
alter table crypto_transfers add column failure_reason text;
