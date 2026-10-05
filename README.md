# Neobank on Bitnob

A money app for Nigerians: NGN account number, hold NGN / USDT / USDC / BTC,
swap, send by tag, pay out across Africa and USD rails. Bitnob supplies the
rails; we own the user, the ledger and the experience. Full spec:
[docs/spec.md](docs/spec.md).

```
mobile/     React Native (Expo, Expo Router, TypeScript)
backend/    Go API + worker
supabase/   Postgres migrations (Supabase)
```

## How the pieces fit

- **Mobile -> Supabase**: auth only (phone + OTP). The app holds a Supabase session.
- **Mobile -> Go API**: everything else, with the Supabase access token as a
  bearer token. The API verifies it against the project's JWKS.
- **Go -> Postgres**: the backend is the only reader and writer. Every table has
  RLS enabled with no policies, so nothing is reachable through Supabase's
  client API.
- **Go -> Bitnob**: HMAC-signed requests from `internal/bitnob`. The app never
  calls Bitnob.
- **Bitnob -> Go**: `POST /webhooks/bitnob` verifies the signature, stores the
  raw event in `webhook_events`, and returns 200. `cmd/worker` processes the
  table (it doubles as the queue, claimed with `FOR UPDATE SKIP LOCKED`).

## Ledger

Double-entry, integers in the smallest unit (kobo, micro, sat). `postings.amount`
is signed: credit > 0, debit < 0, and each entry sums to zero per asset
(enforced in Go and by a deferred trigger). User accounts cannot go negative
(check constraint), the journal is append-only (triggers), and
`journal_entries.idempotency_key` makes every posting safe to replay.
`omnibus:{asset}` is debit-normal, so `-balance` is what Bitnob should hold.

## Setup

1. **Supabase**: create a project, enable Phone auth (add a test number + OTP
   under Auth -> Phone for development), then:

   ```bash
   brew install supabase/tap/supabase
   supabase login
   supabase init      # generates supabase/config.toml; keeps migrations/
   supabase link --project-ref YOUR-PROJECT
   ```

2. **Backend**: `cp backend/.env.example backend/.env` and fill it in.
   `DATABASE_URL` is the Transaction pooler string (port 6543) and
   `SUPABASE_URL` is the project URL (`https://<ref>.supabase.co`), not the
   database host. Bitnob keys can stay empty in development. Then push the
   schema through that same connection and start the services:

   ```bash
   set -a && . backend/.env && set +a && supabase db push --db-url "$DATABASE_URL"
   make api
   make worker
   ```

   Plain `supabase db push` uses port 5432, which some networks drop
   ("Connection terminated unexpectedly"); `--db-url` avoids that.
   `make whoami` checks Bitnob credentials once you have them.

3. **Mobile**: `cp mobile/.env.example mobile/.env`, fill it in, then `make mobile`.

## What works today

| Area | State |
| --- | --- |
| Bitnob client: HMAC signing, `whoami`, `balances` | done, verified against the sandbox |
| Webhook receiver (verify, store-first, dedupe) and queue worker with backoff | done |
| Ledger: accounts, idempotent posting, overdraft protection | done, tested against Postgres |
| Onboarding: `POST /v1/onboarding/kyc` creates the Bitnob customer and NGN account number | done, verified against the sandbox |
| Deposits: `virtual_account.deposit.success` webhook, plus a once-a-minute poll for missed ones | done; the webhook path is tested with a hand-built payload only |
| `GET /v1/me`, `PUT /v1/me/tag`, `GET /v1/balances`, `GET /v1/virtual-account`, `POST /v1/transfers` | done |
| `POST /v1/dev/simulate-deposit`, `POST /v1/dev/fund` (non-production) | done, for local testing |
| Reconciler: ledger nets to zero, cached balances match postings | done; Bitnob comparison is a TODO |
| Swaps: `POST /v1/swaps/quotes`, `POST /v1/swaps`; funds held while the order is out, settled or released after | done, verified with real sandbox trades |
| Prices: `GET /v1/prices` (indicative, display only) and per-user display currency | done |
| Payouts: countries, per-rail recipient forms, account lookup, quote, send, status, saved recipients; funds held until the rail confirms | done, verified with sandbox payouts to a Nigerian bank and Ghanaian mobile money |
| Crypto: networks, per-user deposit address per network, deposits by webhook and sweep, withdrawals with fee preview | done; payload shapes taken from real sandbox webhooks, flows tested against a mock |
| Activity | routes return 501; screen is a placeholder |
| App: phone + OTP, onboarding (name, email, date of birth, BVN, tag), Home, Add money, Swap review, Send by tag, Send to bank or mobile money, Receive and send crypto, Profile | done |

Notes from the sandbox:

- Bitnob answers a duplicate BVN or email with the existing customer and a
  success status, so onboarding compares the returned email with the one sent.
- We never store the BVN: `kyc_records.id_reference` is an HMAC of it under
  `KYC_HASH_KEY`, which is enough to refuse the same BVN twice.
- Every swap direction is one Bitnob "sell" quote with the amount the user
  gives up as `quantity`. Quotes last about 30 seconds on NGN pairs and 5
  minutes on crypto-only pairs. Orders filled immediately in every test.
- Bitnob checks our own balance with them when quoting, so a swap out of an
  asset we are not pre-funded in fails with "insufficient balance".
- Our swap margin is `SWAP_FEE_BPS` (default 50 = 0.5%), taken from what the
  user receives and posted to `revenue:spread:{asset}`.
- Payout quotes accept NGN, USDT, USDC and BTC as the source, so no swap is
  chained in front of a payout. A payout is quote, initialize (with the
  beneficiary), finalize; the record then goes PENDING/PROCESSING to SUCCESS.
- The recipient form is rendered from Get Country Details. Rails with nested
  sender/beneficiary blocks (SWIFT, wire, ACH, SEPA) are filtered out for now,
  which leaves bank, mobile money, paybill and till.
- Our payout fee is `PAYOUT_FEE_BPS` (default 100 = 1%), charged on top in the
  source asset and posted to `revenue:fees:{asset}` on success.
- Webhook signatures are hex HMAC-SHA512 of the raw body in
  `x-bitnob-signature`; real `payouts.*` deliveries verify with this.
- Crypto: one Bitnob address per user per network takes every asset on that
  network. The sandbox offers no Tron and fails to mint Bitcoin addresses;
  Stellar is skipped because it shares one address and separates users by memo.
- Withdrawals have no fee-estimate or status endpoint. Bitnob adds its own fee
  on top (1 USDC on our one sandbox withdrawal) and reports the outcome only by
  `transfer.success` / `transfer.failed`. We charge a flat placeholder fee per
  network (`feeFor` in `internal/crypto`) and book the difference as revenue.
- A repeated withdrawal reference is refused with 409, not answered with the
  original as the docs say.
- Bitnob cannot deliver webhooks to localhost, so locally deposits arrive via
  the worker's poll (or instantly via the simulate-deposit button in the app).

Bitnob endpoint paths marked `UNVERIFIED` in
[backend/internal/bitnob/endpoints.go](backend/internal/bitnob/endpoints.go)
came from the brief; none remain.
