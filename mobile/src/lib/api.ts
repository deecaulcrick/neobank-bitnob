import type { Asset } from './money';
import { supabase } from './supabase';

const BASE_URL = process.env.EXPO_PUBLIC_API_URL ?? 'http://localhost:8080';

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const { data } = await supabase.auth.getSession();
  const token = data.session?.access_token;

  let res: Response;
  try {
    res = await fetch(BASE_URL + path, {
      method,
      headers: {
        Accept: 'application/json',
        ...(body ? { 'Content-Type': 'application/json' } : {}),
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError(0, "We can't reach the server. Check your connection and try again.");
  }
  const json = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, json.error ?? 'Something went wrong');
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

export type Balance = { asset: Asset; available: number; pending: number; decimals: number };

export const api = {
  me: () => request<Me>('GET', '/v1/me'),
  setTag: (tag: string) => request<{ tag: string }>('PUT', '/v1/me/tag', { tag }),
  submitKyc: (input: KycInput) => request<VirtualAccount>('POST', '/v1/onboarding/kyc', input),
  virtualAccount: () => request<VirtualAccount>('GET', '/v1/virtual-account'),
  setDisplayCurrency: (currency: 'NGN' | 'USD') =>
    request<{ display_currency: 'NGN' | 'USD' }>('PUT', '/v1/me/display-currency', { currency }),
  prices: () => request<Prices>('GET', '/v1/prices'),
  balances: () => request<{ balances: Balance[] }>('GET', '/v1/balances'),
  swapQuote: (input: { from: Asset; to: Asset; amount: string }) =>
    request<SwapQuote>('POST', '/v1/swaps/quotes', input),
  swap: (quoteId: string) => request<SwapTrade>('POST', '/v1/swaps', { quote_id: quoteId }),
  payoutCountries: () => request<{ countries: PayoutCountry[] }>('GET', '/v1/payouts/countries'),
  payoutCountry: (code: string) => request<PayoutCountryDetails>('GET', `/v1/payouts/countries/${code}`),
  payoutLookup: (country: string, rail: string, provider: string, account: string) =>
    request<{ account_name: string }>(
      'GET',
      `/v1/payouts/account-lookup?country=${country}&rail=${rail}&provider=${encodeURIComponent(provider)}&account=${encodeURIComponent(account)}`,
    ),
  beneficiaries: () => request<{ beneficiaries: SavedBeneficiary[] }>('GET', '/v1/beneficiaries'),
  payoutQuote: (input: { country: string; currency: string; from_asset: Asset; amount: string }) =>
    request<PayoutQuote>('POST', '/v1/payouts/quotes', input),
  sendPayout: (input: { quote_id: string; beneficiary: PayoutBeneficiary; payment_reason: string }) =>
    request<Payout>('POST', '/v1/payouts', input),
  transfer: (input: { to_tag: string; asset: Asset; amount: string; idempotency_key: string }) =>
    request<{ entry_id: string; status: string }>('POST', '/v1/transfers', input),
  // Development only: pays NGN 1,000 in through the Bitnob sandbox.
  devSimulateDeposit: () => request('POST', '/v1/dev/simulate-deposit'),
  // Development only: credits a fake deposit.
  devFund: (asset: Asset, amount: string) => request('POST', '/v1/dev/fund', { asset, amount }),
};
