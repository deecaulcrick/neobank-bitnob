# Neobank on Bitnob — Product & Tech Spec

Oct 4, 2026 · @Deborah caulcrick

## Overview

A Cash App-style money app for Nigerians: every user gets a personal NGN bank account number, can swap into and hold USDT, USDC and BTC, and can send money out to 9 payout currencies across Africa and USD rails worldwide. Bitnob supplies the rails; we own the user, the ledger and the experience.

**Target user.** Nigerians paid in naira who want to (a) protect savings in dollars, (b) send money to family or suppliers in Ghana, Kenya, francophone West Africa or abroad, and (c) hold some bitcoin, without juggling an exchange, a bank app and a remittance app. The NGN virtual account is Nigeria-only, so v1 launches in Nigeria; other countries are payout destinations, not user markets.

**In v1**

- Personal NGN account number per user (fund by bank transfer)
- Hold four assets: NGN, USDT, USDC, BTC
- Instant swaps between any two of them
- Payouts to bank and mobile money in all supported corridors
- Free, instant in-app transfers to other users by tag (the Cash App core)
- Receive and send USDT, USDC and BTC on-chain

**Out of scope for v1:** virtual cards (Bitnob card issuing is the v2 candidate), bill pay, savings yield, multi-country user onboarding, USSD channel.

## Core user flows

Six flows make up v1; every one ends with a ledger posting and a push notification, and every money movement shows a locked rate and fee before the user confirms.

1. **Onboard.** Phone number + OTP, then name, date of birth, BVN or NIN, selfie. We create the Bitnob customer, then issue the NGN virtual account. User picks a unique tag (e.g. `@dee`). Tier 1 limits apply until higher-tier KYC is done.
2. **Add money (NGN).** User copies their account number and transfers from any Nigerian bank. Bitnob's deposit webhook arrives, we credit the NGN balance and notify.
3. **Swap.** User picks from/to asset and enters an amount on the keypad. We fetch a quote and show the rate, fee and expiry countdown. On confirm we execute against the quote; on `trade.completed` we post the swap.
4. **Send to a person (in-app).** Pick a tag, amount, asset. Pure ledger move between two users, no Bitnob call, free and instant.
5. **Send out (payout).** Pick country, then rail (bank or mobile money), enter beneficiary details (fields from the country-details endpoint), enter amount in the destination currency or the source asset. We quote, initialize and finalize. Funds sit in a pending state until `payouts.withdrawal.success`, or are returned on `payouts.withdrawal.expired`.
   - If the user pays from NGN or BTC and the payout needs a stablecoin source, we chain a swap first and show it as one action with one combined rate.
6. **Crypto in/out.** User taps Receive on USDT, USDC or BTC, picks a network, gets an address (or a Lightning invoice for BTC). Send works the reverse way with an address and network-fee preview.

## Assets, swaps and corridors

Bitnob quotes all six pairs between NGN, USDT, USDC and BTC directly, so every swap is a single trade with no multi-hop routing. Payouts cover 9 currencies; outside Nigeria and USD, every African corridor is mobile money only.

**Assets held in-app**

| Asset | Use in app | Smallest unit we store | Funding in |
| --- | --- | --- | --- |
| NGN | Spending, funding | kobo (1/100) | Virtual account (bank transfer) |
| USDT | Dollar savings, payouts | micro (1/1,000,000) | Swap, on-chain deposit |
| USDC | Dollar savings, payouts | micro (1/1,000,000) | Swap, on-chain deposit |
| BTC | Long-term holding | sat (1/100,000,000) | Swap, on-chain or Lightning deposit |

Swap pairs supported by the Trading API: USDT↔BTC, USDC↔BTC, USDC↔USDT, NGN↔USDT, NGN↔USDC, NGN↔BTC. Stablecoin networks per the docs include Tron, Polygon, Solana and Ethereum; we show Tron first for USDT because fees are lowest for small amounts.

**Payout corridors** (snapshot; the app reads the live list from Get Supported Countries, never a hardcoded table)

| Currency | Countries | Rail |
| --- | --- | --- |
| NGN | Nigeria | NIP instant bank transfer |
| GHS | Ghana | MTN MoMo |
| KES | Kenya | M-Pesa (number, Paybill, Till) |
| UGX | Uganda | MTN MoMo, Airtel Money |
| RWF | Rwanda | MTN MoMo, Airtel Money |
| GMD | The Gambia | Afrimoney, Qmoney |
| XOF | Senegal, Ivory Coast, Niger | Orange Money, Wave, MTN MoMo, Moov, Free, Expresso, Airtel |
| XAF | Cameroon, Gabon | MTN, Airtel Money, Moov Money |
| USD | US + 43 local-transfer countries + \~100 SWIFT | ACH, wire, local transfer, SWIFT |

Source: [Bitnob supported currencies](https://bitnob.dev/docs/payouts/supported-currencies), [Trading overview](https://bitnob.dev/docs/trading/overview).

## UX principles and screens

The Cash App lesson is restraint: one big number, a keypad, and three verbs. We borrow that interaction model, not its branding; the visual identity is our own.

**Principles**

- **One balance, one tap to switch.** Home shows total value in the user's chosen display currency (NGN or USD), large and centred. Tapping cycles currency; asset breakdown sits one swipe down.
- **Keypad first.** Every money action opens on a full-screen keypad. Asset and destination are chips above the number, not form fields.
- **Three verbs on Home:** Add, Swap, Send. Everything else lives in the tab bar.
- **Locked rates are visible.** Any quote shows the rate, our fee, what arrives, and a countdown ring. Expired quotes refresh silently once, then ask.
- **Honest states.** "Sending" until the rail confirms, never "Sent" early. Failed or expired payouts say the money is back and where.
- **Tags over account numbers.** In-app sends use `@tag`; the NGN account number is for funding only.

**Screen list (v1)**

| Screen | Purpose | Key elements |
| --- | --- | --- |
| Home | Balance and the three verbs | Big balance, currency toggle, Add / Swap / Send, recent activity |
| Assets | Per-asset holdings | NGN, USDT, USDC, BTC rows with value and 24h change for BTC |
| Add money | Fund NGN | Account number, bank name, copy and share buttons |
| Keypad | Amount entry for every action | Amount, asset chip, max button, live conversion line |
| Swap review | Confirm a trade | From / to, rate, fee, countdown, slide to confirm |
| Send picker | Choose destination | Recent tags, search, "Send abroad", "Send crypto" |
| Payout setup | Beneficiary details | Country, rail, dynamic fields per country, save beneficiary |
| Payout review | Confirm a payout | Amount out, fee, rate, arrival estimate, countdown |
| Receive crypto | Show an address | Asset, network selector, QR, address, network warning |
| Activity | Full history | Filter by asset and type, status chips |
| Transaction detail | One movement | Timeline (created, processing, done), reference, receipt share |
| Profile and limits | Identity and tiers | Tag, KYC tier, limits used, upgrade KYC |

## System architecture

Three parts: the mobile app, our backend and Bitnob. The app never calls Bitnob, and Bitnob reaches us only through webhooks, so every balance the user sees comes from our ledger.

&#91;embedded content: system architecture · app, backend, Bitnob\]

Money services call Bitnob with signed requests; webhooks come back through a store-first receiver, and the reconciler checks the ledger against Bitnob balances daily.

Suggested stack: React Native app, a TypeScript or Go backend, Postgres for the ledger, a durable queue for webhook processing, and a managed secret store for the Bitnob client secret.

## API mapping per flow

Every flow except in-app transfers is a quote-then-commit sequence, and the backend, never the phone, calls Bitnob. Endpoint names follow the [API reference](https://bitnob.dev/api-reference/); paths marked † are from the original brief and must be checked against the reference before build.

| Flow | Bitnob calls, in order | Webhook that completes it |
| --- | --- | --- |
| Health check | `GET /api/whoami` | — |
| Onboard | `POST /api/customers` † → create virtual account (`POST /api/virtual-accounts/create` †) | — |
| Add money (NGN) | none (user transfers in) | Virtual-account deposit event (name to confirm) |
| Show balances | `GET` wallet balances (Get Balances) — for reconciliation only; the app reads our ledger | — |
| Swap | Get Prices (indicative) → `POST /api/trading/quotes` → Create Order with the quote | `trade.completed` |
| In-app send | none | — |
| Payout | Get Supported Countries → Get Country Details → `POST /api/payouts/quotes` → Initialize Payout → Finalize Payout | `payouts.initialized`, `payouts.processing`, then `payouts.withdrawal.success` or `payouts.withdrawal.expired` |
| Receive crypto | Generate Addresses (per asset and chain) | Stablecoin deposit event; BTC deposit event |
| Send crypto | Create Withdrawal | `transfer.success` (carries `transaction_id`) |

**Auth.** Every request is HMAC-signed with four headers: `X-Auth-Client`, `X-Auth-Timestamp`, `X-Auth-Nonce`, `X-Auth-Signature` (HMAC-SHA256). Sandbox and production share the base URL `https://api.bitnob.com` and the client ID; only the secret changes. The secret lives in a KMS-backed secret store, never in the mobile app.

**Quote expiry.** Payout quotes in the sample response expire about 15 minutes after creation. The review screen counts down from the quote's `expires_at`, not a client-side timer.

## Ledger and data model

Our double-entry ledger is the source of truth for every balance a user sees; Bitnob balances are what we reconcile against. Amounts are integers in each asset's smallest unit (kobo, micro-USDT/USDC, sats), never floats.

**Ledger accounts**

- One `user:{id}:{asset}` account per user per asset (4 per user)
- `user:{id}:{asset}:pending` holds funds reserved by an in-flight payout or withdrawal
- `omnibus:{asset}` mirrors what Bitnob holds for us
- `revenue:fees:{asset}` and `revenue:spread:{asset}` collect our margin
- `suspense:{asset}` for anything that doesn't match (unknown deposit, amount mismatch) until ops resolves it

**How each flow posts**

| Event | Debit | Credit |
| --- | --- | --- |
| NGN deposit | omnibus:NGN | user:NGN |
| Swap NGN→USDT | user:NGN (full amount) ; omnibus:USDT | omnibus:NGN ; user:USDT (net) ; revenue:spread:USDT |
| In-app send | sender:asset | receiver:asset |
| Payout initiated | user:asset | user:asset:pending |
| Payout success | user:asset:pending | omnibus:asset ; revenue:fees:asset |
| Payout expired | user:asset:pending | user:asset |

**Core tables:** `users`, `kyc_records`, `bitnob_customers`, `virtual_accounts`, `ledger_accounts`, `journal_entries` (one per business event, unique `idempotency_key`), `postings` (lines; must sum to zero per entry and asset), `quotes`, `trades`, `payouts`, `beneficiaries`, `crypto_addresses`, `crypto_transfers`, `webhook_events` (raw payload, unique `event_id`, processed\_at).

Balances are derived by summing postings, with a cached `balance` column on `ledger_accounts` updated in the same transaction and checked nightly.

## Webhooks, idempotency and reconciliation

Webhooks drive every state change, and each one is stored before it is processed so nothing is lost and nothing posts twice.

**Handling a webhook**

1. Verify the signature; reject and alert on failure.
2. Insert the raw payload into `webhook_events` keyed on `event_id`; a duplicate insert means "already seen", return 200.
3. Return 200 fast; process from a queue.
4. The worker posts the journal entry in one DB transaction with `idempotency_key = event_id`, updates the payout/trade/deposit row, then sends the push notification.

**Payout states.** Use the event name, not `data.status`: the event's lower-case status (`initiated`, `processing`, `success`, `expired`) differs from the payout record's upper-case lifecycle status. `payouts.processing` is in-flight only. The legacy `payout.transfer.success/failed` events are retired as of the July 2026 changelog.

**Missed webhooks.** A sweeper polls Get Payout by ID and trade status for anything in a non-final state older than its expected window, and applies the same posting path.

**Daily reconciliation**

- Per asset: sum of all `user:*` + `pending` + `revenue:*` balances must equal Bitnob Get Balances; differences go to an ops report, not auto-correction.
- Per transaction: every Bitnob transaction ID maps to exactly one journal entry, and vice versa.
- Unknown deposits (no matching virtual account or address) land in `suspense` and open an ops ticket.

## Fees and revenue

Revenue comes from swap spread and payout fees, not a monthly fee: free NGN wallets already exist in Nigeria, so holding money here must cost nothing. The numbers below are starting proposals to test against Bitnob's own costs, not settled prices.

| Line | Proposed charge | Notes |
| --- | --- | --- |
| Holding any asset | Free | Table stakes against OPay, PalmPay, Moniepoint |
| NGN deposit | Free |  |
| In-app send | Free | Growth loop; costs us nothing |
| Swap | Spread on top of Bitnob's quote | Shown as one all-in rate plus a stated fee line |
| Payout | Flat fee + small % by corridor | Price per corridor once Bitnob costs are known |
| Crypto withdrawal | Network fee passed through + small flat fee | Preview before confirm |
| Premium (v2) | Monthly plan | Virtual card, higher limits, lower payout fees |

Margin = our charge minus Bitnob's fee and spread on each quote. Track it per transaction in `revenue:*` accounts and review per corridor monthly. Bitnob's docs include a section on how integrators earn on trading; read it before setting the spread.

## Compliance, risk and limits

We run on Bitnob's and its partners' licences, so legal sign-off on the product's structure and naming comes before public launch. This section lists what to settle, not legal advice.

**Regulatory questions for counsel**

- Can we call it a "bank" or "neobank" in Nigeria without a CBN licence? (Likely not; plan copy around "money app".)
- Which entity is the customer's counterparty for NGN, and for crypto holdings?
- Does holding and swapping crypto for users require our own registration under Nigeria's SEC digital-asset rules, or does Bitnob's cover it?
- Travel-rule obligations on crypto sends above thresholds.

**KYC tiers.** Follow CBN tiered KYC: tier 1 (BVN or NIN + phone) with low limits; higher tiers add ID document, address and selfie liveness. Exact tier limits and what Bitnob's customer endpoint requires are open questions below.

**Fraud and risk controls**

- New-account hold: cap payouts and crypto sends for the first days after first deposit (stolen-funds mule risk).
- Velocity limits per user per day on payouts, crypto sends and in-app sends.
- Sanctions and name screening on every payout beneficiary.
- Device binding and PIN or biometric on every money movement.
- BTC volatility disclosure on first BTC purchase; quotes are locked so users never get slippage.
- Treasury: keep a buffer in each asset so payouts aren't blocked by pre-funding gaps.

## Build plan and open questions

Build in six milestones, each ending in something testable in the Bitnob sandbox; the ledger comes before any money flow.

1. **M0 — Plumbing.** HMAC signing client, `whoami` check, secret storage, webhook receiver with `webhook_events` table.
2. **M1 — Accounts.** Onboarding, KYC tier 1, Bitnob customer + virtual account, ledger with NGN deposit posting, Home and Add money screens.
3. **M2 — Swaps.** Prices, quotes, orders, `trade.completed` posting, Keypad and Swap review screens.
4. **M3 — Payouts.** Country list and details, beneficiaries, quote → initialize → finalize, pending/success/expired postings, sweeper.
5. **M4 — Crypto in/out.** Addresses per chain, deposits, withdrawals, network-fee preview.
6. **M5 — Social and beta.** Tags, in-app send, Activity, reconciliation reports, closed beta.

**Open questions**

- [ ] Exact required fields for customer creation and virtual accounts (BVN, NIN, or both?) and any per-tier limits Bitnob enforces
- [ ] Name and payload of the virtual-account deposit webhook
- [ ] Custody model: does Bitnob keep per-customer balances we can rely on, or do we run an omnibus wallet and track users only in our ledger?
- [ ] Which `from_asset` values payout quotes accept (USDT, USDC, BTC, NGN?), to know when a pre-swap is needed
- [ ] Bitnob fees and spread per swap pair and payout corridor, for pricing
- [ ] Webhook signature scheme and retry policy
- [ ] Lightning support for BTC receive/send in the customer-level APIs
- [ ] Legal entity and licensing structure (see Compliance)
