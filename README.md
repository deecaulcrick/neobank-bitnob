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
   supabase init      # generates supabase/config.toml; keeps migrations/
   supabase link --project-ref YOUR-PROJECT
   supabase db push
   ```

2. **Backend**: `cp backend/.env.example backend/.env`, fill it in, then
   `make whoami` (Bitnob credentials check), `make api`, `make worker`.

3. **Mobile**: `cp mobile/.env.example mobile/.env`, fill it in, then `make mobile`.

## What works today

| Area | State |
| --- | --- |
| Bitnob client: HMAC signing, `whoami`, `balances` | done, tested |
| Webhook receiver (verify, store-first, dedupe) and queue worker with backoff | done; handlers are stubs |
| Ledger: accounts, idempotent posting, overdraft protection | done, tested against Postgres |
| `GET /v1/me`, `PUT /v1/me/tag`, `GET /v1/balances`, `POST /v1/transfers` | done |
| `POST /v1/dev/fund` (non-production) | done, for local testing |
| Reconciler: ledger nets to zero, cached balances match postings | done; Bitnob comparison is a TODO |
| KYC, virtual account, swaps, payouts, crypto, activity | routes return 501; screens are placeholders |
| App: phone + OTP, Home, Assets, Send by tag, Keypad, Profile | done |

Bitnob endpoint paths marked `UNVERIFIED` in
[backend/internal/bitnob/endpoints.go](backend/internal/bitnob/endpoints.go)
come from the brief and must be checked against the API reference. The webhook
signature is assumed to be hex HMAC-SHA512 of the raw body in
`x-bitnob-signature`; confirm with a sandbox event.
