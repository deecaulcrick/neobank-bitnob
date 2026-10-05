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

  const res = await fetch(BASE_URL + path, {
    method,
    headers: {
      Accept: 'application/json',
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const json = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, json.error ?? 'Something went wrong');
  return json as T;
}

export type Me = {
  id: string;
  phone: string;
  tag: string | null;
  first_name: string | null;
  last_name: string | null;
  kyc_tier: number;
  display_currency: 'NGN' | 'USD';
};

export type Balance = { asset: Asset; available: number; pending: number; decimals: number };

export const api = {
  me: () => request<Me>('GET', '/v1/me'),
  setTag: (tag: string) => request<{ tag: string }>('PUT', '/v1/me/tag', { tag }),
  balances: () => request<{ balances: Balance[] }>('GET', '/v1/balances'),
  transfer: (input: { to_tag: string; asset: Asset; amount: string; idempotency_key: string }) =>
    request<{ entry_id: string; status: string }>('POST', '/v1/transfers', input),
  // Development only: credits a fake deposit.
  devFund: (asset: Asset, amount: string) => request('POST', '/v1/dev/fund', { asset, amount }),
};
