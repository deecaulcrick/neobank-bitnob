import type { Asset } from './money';
import { supabase } from './supabase';

const BASE_URL = process.env.EXPO_PUBLIC_API_URL ?? 'http://localhost:8080';

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    // Machine-readable reason, e.g. 'pin_wrong', 'limit', 'not_invited'.
    public code?: string,
  ) {
    super(message);
  }
}

// pin: the transaction PIN, sent on every request that moves money.
async function request<T>(method: string, path: string, body?: unknown, pin?: string): Promise<T> {
  const { data } = await supabase.auth.getSession();
  const token = data.session?.access_token;

  // Give up after a while rather than leave a spinner turning forever.
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), 45_000);
  let res: Response;
  try {
    res = await fetch(BASE_URL + path, {
      method,
      signal: abort.signal,
      headers: {
        Accept: 'application/json',
        ...(body ? { 'Content-Type': 'application/json' } : {}),
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(pin ? { 'X-Pin': pin } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError(
      0,
      abort.signal.aborted
        ? 'That took too long. Check your connection and try again.'
        : "We can't reach the server. Check your connection and try again.",
    );
  } finally {
    clearTimeout(timer);
  }
  const json = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, json.error ?? 'Something went wrong', json.code);
  return json as T;
}

export type Me = {
  id: string;
  phone: string;
  email: string | null;
  tag: string | null;
  first_name: string | null;
  last_name: string | null;
  kyc_tier: number;
  display_currency: 'NGN' | 'USD';
  has_pin: boolean;
};

// Today's outflow against the user's limits, in kobo.
export type Limits = {
  daily_limit: number;
  single_limit: number;
  used_today: number;
  remaining: number;
  sends_today: number;
  max_sends: number;
  new_account_until: string | null;
  new_account_daily_limit: number;
};

export type KycInput = {
  first_name: string;
  last_name: string;
  email: string;
  date_of_birth: string; // YYYY-MM-DD
  bvn: string;
};

export type VirtualAccount = { account_number: string; account_name: string; bank_name: string };

// Indicative value of one major unit of each asset, for display only.
export type Prices = { ngn: Record<Asset, number>; usd: Record<Asset, number>; as_of: string };

export type SwapQuote = {
  id: string;
  from_asset: Asset;
  to_asset: Asset;
  from_amount: number;
  to_amount: number; // what the user receives, after the fee
  fee_amount: number; // in to_asset
  rate: string; // to_asset per one from_asset, all-in
  expires_at: string;
  enough_funds: boolean;
};

export type SwapTrade = {
  id: string;
  status: 'pending' | 'completed' | 'failed';
  from_asset: Asset;
  to_asset: Asset;
  from_amount: number;
  to_amount: number;
};

export type PayoutCountry = {
  code: string;
  name: string;
  flag: string;
  corridors: { currency: string; rails: string[] }[];
};

// One input on a rail's recipient form, as Bitnob defines it.
export type PayoutField = {
  key: string;
  label: string;
  required: boolean;
  component: string; // 'text' | 'select' | ...
  pattern?: string;
  description?: string;
  placeholder?: string;
  options: { label: string; value: string }[];
  options_ref?: string; // 'banks': choose from the rail's bank list
};

export type PayoutRail = {
  label: string;
  fields: PayoutField[];
  banks?: { name: string; code: string }[];
  limits?: { min_amount: string; max_amount: string; currency: string };
};

export type PayoutCountryDetails = { code: string; name: string; flag: string; rails: Record<string, PayoutRail> };

export type PayoutBeneficiary = { rail: string; account_name: string; fields: Record<string, string> };

export type SavedBeneficiary = PayoutBeneficiary & { id: string; country: string; currency: string };

export type PayoutQuote = {
  id: string;
  from_asset: Asset;
  from_amount: number; // everything the user pays, fee included
  fee_amount: number;
  country: string;
  to_currency: string;
  to_amount: number; // hundredths of to_currency
  rate: string;
  expires_at: string;
  enough_funds: boolean;
};

export type Payout = {
  id: string;
  status: 'processing' | 'success' | 'expired' | 'failed';
  from_asset: Asset;
  from_amount: number;
  to_currency: string;
  to_amount: number;
  beneficiary_name: string;
};

export type CryptoNetwork = { network: string; label: string; fee: number; min_withdrawal: number };

export type WithdrawalPreview = {
  asset: Asset;
  network: string;
  address: string;
  amount: number;
  fee: number;
  total: number;
  enough_funds: boolean;
};

export type CryptoTransfer = { id: string; status: 'pending' | 'success' | 'failed'; asset: Asset; amount: number; fee: number };

type WithdrawalInput = { asset: Asset; network: string; address: string; amount: string };

export type ActivityKind =
  | 'deposit'
  | 'transfer_in'
  | 'transfer_out'
  | 'swap'
  | 'payout'
  | 'crypto_in'
  | 'crypto_out'
  | 'card_create'
  | 'card_fund'
  | 'card_withdraw';

export type ActivityItem = {
  id: string;
  kind: ActivityKind;
  status: 'done' | 'pending' | 'failed';
  asset: Asset;
  amount: number; // signed minor units; positive is money in
  other_currency: string | null;
  other_amount: number | null;
  title: string;
  created_at: string;
};

export type ActivityDetail = ActivityItem & {
  timeline: { label: string; at: string | null }[];
  rows: { label: string; value: string }[];
  reference: string;
};

export type Person = { tag: string; first_name: string | null };

// The user's virtual dollar card. Amounts are micro-dollars, the same unit as USDC.
export type CardView = {
  kyc_status: '' | 'pending' | 'approved' | 'rejected';
  card: {
    id: string;
    status: 'pending' | 'active' | 'frozen';
    brand: string;
    last4: string;
    name: string;
    balance: number | null; // null when it couldn't be read just now
  } | null;
  creation_fee: number;
  fund_fee: number;
};

export type CardKycInput = {
  bvn: string;
  line1: string;
  city: string;
  state: string;
  postal_code: string;
  occupation: string;
  employment_status: string;
  account_purpose: string;
  annual_salary: string;
  expected_monthly_volume: string;
  accept_terms: boolean;
};

export type CardTransfer = { id: string; kind: 'create' | 'fund' | 'withdraw'; status: 'pending' | 'success' | 'failed'; amount: number; fee: number };

export type CardSecrets = {
  number: string;
  cvv: string;
  expiry_month: string;
  expiry_year: string;
  name: string;
  billing_address: string;
};

export type CardStatement = { id: string; type: string; status: string; description: string; amount: number; created_at: string };

export type Balance = { asset: Asset; available: number; pending: number; decimals: number };

// A fresh idempotency key. Call it from a handler, not while rendering.
export const newKey = () => `${Date.now()}-${Math.random().toString(36).slice(2)}`;

export const api = {
  me: () => request<Me>('GET', '/v1/me'),
  setTag: (tag: string) => request<{ tag: string }>('PUT', '/v1/me/tag', { tag }),
  submitKyc: (input: KycInput) => request<VirtualAccount>('POST', '/v1/onboarding/kyc', input),
  virtualAccount: () => request<VirtualAccount>('GET', '/v1/virtual-account'),
  setDisplayCurrency: (currency: 'NGN' | 'USD') =>
    request<{ display_currency: 'NGN' | 'USD' }>('PUT', '/v1/me/display-currency', { currency }),
  prices: () => request<Prices>('GET', '/v1/prices'),
  balances: () => request<{ balances: Balance[] }>('GET', '/v1/balances'),
  // side 'pay': amount is what you give, in `from`. side 'get': amount is
  // exactly what you want to receive, in `to`.
  swapQuote: (input: { from: Asset; to: Asset; amount: string; side: 'pay' | 'get' }) =>
    request<SwapQuote>('POST', '/v1/swaps/quotes', input),
  swap: (quoteId: string, pin: string) => request<SwapTrade>('POST', '/v1/swaps', { quote_id: quoteId }, pin),
  payoutCountries: () => request<{ countries: PayoutCountry[] }>('GET', '/v1/payouts/countries'),
  payoutCountry: (code: string) => request<PayoutCountryDetails>('GET', `/v1/payouts/countries/${code}`),
  payoutLookup: (country: string, rail: string, provider: string, account: string) =>
    request<{ account_name: string }>(
      'GET',
      `/v1/payouts/account-lookup?country=${country}&rail=${rail}&provider=${encodeURIComponent(provider)}&account=${encodeURIComponent(account)}`,
    ),
  beneficiaries: () => request<{ beneficiaries: SavedBeneficiary[] }>('GET', '/v1/beneficiaries'),
  // Send either `amount` (what you pay, in from_asset) or `settlement_amount`
  // (exactly what the recipient gets, in `currency`).
  payoutQuote: (input: { country: string; currency: string; from_asset: Asset; amount?: string; settlement_amount?: string }) =>
    request<PayoutQuote>('POST', '/v1/payouts/quotes', input),
  sendPayout: (input: { quote_id: string; beneficiary: PayoutBeneficiary; payment_reason: string }, pin: string) =>
    request<Payout>('POST', '/v1/payouts', input, pin),
  cryptoNetworks: (asset: Asset) => request<{ networks: CryptoNetwork[] }>('GET', `/v1/crypto/networks?asset=${asset}`),
  cryptoAddress: (asset: Asset, network: string) =>
    request<{ address: string }>('POST', '/v1/crypto/addresses', { asset, network }),
  cryptoPreview: (input: WithdrawalInput) => request<WithdrawalPreview>('POST', '/v1/crypto/withdrawals/preview', input),
  cryptoWithdraw: (input: WithdrawalInput & { idempotency_key: string }, pin: string) =>
    request<CryptoTransfer>('POST', '/v1/crypto/withdrawals', input, pin),
  people: (query: string) => request<{ people: Person[] }>('GET', `/v1/people?q=${encodeURIComponent(query)}`),
  activity: (filter: { asset?: string; kinds?: string[]; before?: string } = {}) => {
    const q = [
      filter.asset && `asset=${filter.asset}`,
      filter.kinds?.length && `kinds=${filter.kinds.join(',')}`,
      filter.before && `before=${encodeURIComponent(filter.before)}`,
    ].filter(Boolean);
    return request<{ items: ActivityItem[] }>('GET', `/v1/activity${q.length ? '?' + q.join('&') : ''}`);
  },
  activityItem: (id: string) => request<ActivityDetail>('GET', `/v1/activity/${encodeURIComponent(id)}`),
  card: () => request<CardView>('GET', '/v1/card'),
  cardKyc: (input: CardKycInput) => request<{ kyc_status: string }>('POST', '/v1/card/kyc', input),
  createCard: (amount: string, key: string, pin: string) =>
    request<CardTransfer>('POST', '/v1/card', { amount, idempotency_key: key }, pin),
  moveCard: (kind: 'fund' | 'withdraw', amount: string, key: string, pin: string) =>
    request<CardTransfer>('POST', `/v1/card/${kind}`, { amount, idempotency_key: key }, pin),
  revealCard: (pin: string) => request<CardSecrets>('POST', '/v1/card/reveal', undefined, pin),
  lockCard: (locked: boolean) => request<{ status: 'active' | 'frozen' }>('POST', '/v1/card/lock', { locked }),
  cardTransactions: () => request<{ transactions: CardStatement[] }>('GET', '/v1/card/transactions'),
  transfer: (input: { to_tag: string; asset: Asset; amount: string; idempotency_key: string }, pin: string) =>
    request<{ entry_id: string; status: string }>('POST', '/v1/transfers', input, pin),
  setPin: (pin: string, currentPin?: string) =>
    request<{ has_pin: boolean }>('PUT', '/v1/me/pin', { pin, current_pin: currentPin ?? '' }),
  limits: () => request<Limits>('GET', '/v1/limits'),
  registerDevice: (token: string, platform: string) => request('POST', '/v1/devices', { token, platform }),
  // Development only: pays NGN 1,000 in through the Bitnob sandbox.
  devSimulateDeposit: () => request('POST', '/v1/dev/simulate-deposit'),
  // Development only: credits a fake deposit.
  devFund: (asset: Asset, amount: string) => request('POST', '/v1/dev/fund', { asset, amount }),
};
