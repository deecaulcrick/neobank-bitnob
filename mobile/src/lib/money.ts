// Amounts are integers in each asset's smallest unit (kobo, micro, sat),
// exactly as the backend ledger stores them. Formatting is string-based so
// no float ever touches a balance.

export type Asset = 'NGN' | 'USDT' | 'USDC' | 'BTC';

export const ASSETS: Asset[] = ['NGN', 'USDT', 'USDC', 'BTC'];

export const DECIMALS: Record<Asset, number> = { NGN: 2, USDT: 6, USDC: 6, BTC: 8 };

// Decimals worth showing; the rest are trimmed when they are zero.
const DISPLAY_DECIMALS: Record<Asset, number> = { NGN: 2, USDT: 2, USDC: 2, BTC: 8 };

const SYMBOL: Record<Asset, string> = { NGN: '₦', USDT: '$', USDC: '$', BTC: '₿' };

export function formatMinor(asset: Asset, minor: number): string {
  const d = DECIMALS[asset];
  const digits = Math.abs(Math.trunc(minor)).toString().padStart(d + 1, '0');
  const whole = digits.slice(0, -d).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  let frac = digits.slice(-d);
  const keep = DISPLAY_DECIMALS[asset];
  frac = frac.slice(0, keep) + frac.slice(keep).replace(/0+$/, '');
  return `${minor < 0 ? '-' : ''}${SYMBOL[asset]}${whole}${frac ? '.' + frac : ''}`;
}

// Keypad input rules: digits and one dot, capped at the asset's precision.
export function appendKey(asset: Asset, current: string, key: string): string {
  if (key === 'back') return current.slice(0, -1);
  if (key === '.') return current.includes('.') ? current : (current || '0') + '.';
  const next = current === '0' ? key : current + key;
  const frac = next.split('.')[1];
  if (frac && frac.length > DECIMALS[asset]) return current;
  if (next.replace('.', '').length > 15) return current;
  return next;
}
