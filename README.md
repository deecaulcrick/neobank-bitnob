# Neobank on Bitnob

A Cash App-style money app for Nigerians, built on [Bitnob](https://bitnob.com)'s
API. Users get a naira account number, hold NGN, USDT, USDC and BTC, swap
between them, send to each other by tag, pay out to banks and mobile money
across Africa, send and receive crypto, and load a virtual dollar card.

Bitnob supplies the rails. This project owns the user, the ledger and the app.
The full product and technical spec is in [docs/spec.md](docs/spec.md).

> **Status: a working prototype, tested against Bitnob's sandbox only.** It has
> not been audited, has never handled real money, and is not a licensed
> financial product. Moving money for other people is regulated; if you build
> on this, the licensing, KYC/AML, fraud controls and security review are yours
> to do. Fees and limits in the config are placeholders.

```
mobile/     React Native app (Expo SDK 57, Expo Router, TypeScript)
backend/    Go API and worker (stdlib net/http, pgx)
supabase/   Postgres migrations
docs/       Product and technical spec
```

## What it does

- **Sign up**: phone number and one-time code, then name, email, date of birth,
  BVN and a tag. Onboarding creates the Bitnob customer and a naira account
  number.
- **Add money**: bank transfer to the account number; deposits arrive by
  webhook, with a poll as a fallback.
- **Balances**: NGN, USDT, USDC and BTC, with a total in naira or dollars.
- **Swap**: any pair, typing either the amount you pay or the amount you want
  to receive. The quote is locked for a short time and funds are held while the
  order is out.
- **Send by tag**: instant transfers between users, inside the ledger.
- **Pay out**: to bank accounts and mobile money in the currencies listed in
  `PAYOUT_CURRENCIES`, funded from any balance. Recipient forms come from
  Bitnob's country details.
- **Crypto**: a deposit address per network, and withdrawals with a fee preview.
- **Virtual dollar card**: card KYC, create, load from USDT or USDC, withdraw,
  show details, lock. The card number and CVV are fetched on demand and never
  stored.
- **Activity**: one feed across everything, with a detail view per transaction.
- **Controls**: transaction PIN with lockout (Face ID or fingerprint in the
  app), daily and per-transaction limits, a lower cap for new accounts, an
  invite list for a closed beta, a recipient blocklist, push notifications, and
  a daily reconciliation against Bitnob.

## How the pieces fit

- **App -> Supabase**: sign-in only (phone and one-time code). The app holds a
  Supabase session.
- **App -> Go API**: everything else, with the Supabase access token as a bearer
  token. The API verifies it against the project's JWKS.
- **Go -> Postgres**: the backend is the only reader and writer. Every table has
  row-level security on with no policies, so nothing is reachable through
  Supabase's client API.
- **Go -> Bitnob**: HMAC-signed requests from `backend/internal/bitnob`. The app
  never calls Bitnob.
- **Bitnob -> Go**: `POST /webhooks/bitnob` verifies the signature, stores the
  raw event and returns 200. The worker processes stored events with retries,
  and also sweeps for anything a webhook missed.

### Ledger

Money is tracked in a double-entry ledger in Postgres, in whole numbers of the
smallest unit (kobo, micro-dollars, satoshis); floats never touch an amount.

- Every entry sums to zero per asset, enforced in Go and by a database trigger.
- The journal is append-only, and user accounts cannot go negative.
- Every posting carries an idempotency key, so retries are safe.
- Anything that leaves the system (swap, payout, crypto send, card load) is
  held first, then settled or released once Bitnob reports the outcome.

## Running it

You need:

- Go 1.26+ and a recent Node LTS
- The [Supabase CLI](https://supabase.com/docs/guides/cli) and a Supabase project
- A Bitnob account with sandbox API credentials
- Xcode or Android Studio for a simulator, or Expo Go on a phone

### 1. Supabase

Create a project and enable Phone sign-in. For development you can add test
phone numbers with fixed codes under Auth -> Phone, so no SMS provider is
needed.

### 2. Backend

```bash
cp backend/.env.example backend/.env
```

Fill in `backend/.env`; each value is explained in the example file. The ones
that trip people up:

- `DATABASE_URL` is the **Transaction pooler** connection string (port 6543),
  with any special characters in the password percent-encoded.
- `SUPABASE_URL` is the project URL (`https://<ref>.supabase.co`), not the
  database host.
- `KYC_HASH_KEY` is any long random string. Do not change it once you have
  users; it is what detects a BVN being used twice.

Create the tables:

```bash
make migrate
```

Then start the API and the worker, each in its own terminal:

```bash
make api
```

```bash
make worker
```

`make whoami` checks that your Bitnob credentials work.

### 3. Mobile app

```bash
cp mobile/.env.example mobile/.env
```

Fill in the Supabase URL, the anon key and the API address (use your machine's
LAN IP rather than `localhost` on a physical device), then install and start:

```bash
cd mobile && npm install
```

```bash
make mobile
```

Press `i` for the iOS simulator or `a` for Android.

### 4. Webhooks

Bitnob cannot reach `localhost`. To receive webhooks while developing, expose
the API with a tunnel such as ngrok, register `https://<your-tunnel>/webhooks/bitnob`
in the Bitnob dashboard, and set `BITNOB_WEBHOOK_SECRET`. Without a tunnel the
worker's polling still picks up deposits, and outside production the Add money
screen has a button that simulates one.

### Useful commands

| Command | What it does |
| --- | --- |
| `make test` | `go vet` and unit tests |
| `make test-db` | All tests, including the ledger tests, against `TEST_DATABASE_URL` |
| `make invite PHONE=2348012345678` | Let a phone number sign up when `BETA_INVITE_ONLY=true` |
| `make block NAME="Full Name"` | Refuse payouts to a recipient name |
| `cd mobile && npx tsc --noEmit` | Type-check the app |

## Things to know before relying on it

- **Sandbox only.** Every flow was exercised against Bitnob's sandbox or a mock
  of it. Card issuing in particular has only run against a mock.
- **Fees and limits are placeholders.** `SWAP_FEE_BPS`, `PAYOUT_FEE_BPS`, the
  `CARD_*` values, the flat crypto withdrawal fee and the `LIMIT_*` values are
  starting guesses, not prices.
- **Screening is a local list.** The payout blocklist is a hook, not sanctions
  screening.
- **Reconciliation is partial.** It compares against Bitnob's most recent page
  of transactions, not full history.
- **Not built yet:** changing or resetting a PIN, closing a card, and
  international rails that need sender and beneficiary details (SWIFT, wire,
  ACH, SEPA).
- **Push notifications** need an EAS project and a real device.

### Notes on Bitnob's sandbox

Behaviour observed while building, which may differ from the docs or from
production:

- A duplicate BVN or email returns the existing customer with a success status.
- Webhook signatures are a hex HMAC-SHA512 of the raw body in
  `x-bitnob-signature`.
- Quotes last about 30 seconds on naira pairs and 5 minutes on crypto-only
  pairs. Bitnob checks the company balance when quoting, so swapping out of an
  asset the company account does not hold fails.
- Naira-funded payouts stay in "processing" in the sandbox.
- There is no Tron in the sandbox, and Bitcoin address creation fails there.
- Crypto withdrawals have no fee-estimate or status endpoint; the outcome
  arrives only by webhook. A repeated reference is refused with 409.
- Cards are funded from the company's USDC balance, and issuing one needed
  about $2 more than the amount loaded.

## Secrets

`.env` files are ignored by git; only the `.env.example` files are tracked.
Never commit Bitnob credentials, the database URL or `KYC_HASH_KEY`. The
Supabase anon key in the mobile app is designed to be public.

## License

[MIT](LICENSE). Use it, change it, ship it; it comes with no warranty.
