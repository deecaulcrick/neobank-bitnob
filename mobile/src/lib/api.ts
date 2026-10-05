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

export type Balance = { asset: Asset; available: number; pending: number; decimals: number };

export const api = {
  me: () => request<Me>('GET', '/v1/me'),
  setTag: (tag: string) => request<{ tag: string }>('PUT', '/v1/me/tag', { tag }),
  submitKyc: (input: KycInput) => request<VirtualAccount>('POST', '/v1/onboarding/kyc', input),
  virtualAccount: () => request<VirtualAccount>('GET', '/v1/virtual-account'),
  balances: () => request<{ balances: Balance[] }>('GET', '/v1/balances'),
  transfer: (input: { to_tag: string; asset: Asset; amount: string; idempotency_key: string }) =>
    request<{ entry_id: string; status: string }>('POST', '/v1/transfers', input),
  // Development only: pays NGN 1,000 in through the Bitnob sandbox.
  devSimulateDeposit: () => request('POST', '/v1/dev/simulate-deposit'),
  // Development only: credits a fake deposit.
  devFund: (asset: Asset, amount: string) => request('POST', '/v1/dev/fund', { asset, amount }),
};
