// Amounts are integers in each asset's smallest unit (kobo, micro, sat),
// exactly as the backend ledger stores them. Formatting is string-based so
// no float ever touches a balance.

export type Asset = 'NGN' | 'USDT' | 'USDC' | 'BTC';

export const ASSETS: Asset[] = ['NGN', 'USDT', 'USDC', 'BTC'];

export const DECIMALS: Record<Asset, number> = { NGN: 2, USDT: 6, USDC: 6, BTC: 8 };

// Decimals always shown; anything beyond is kept only when it is non-zero.
const DISPLAY_DECIMALS: Record<Asset, number> = { NGN: 2, USDT: 2, USDC: 2, BTC: 2 };

export const SYMBOL: Record<Asset, string> = { NGN: '₦', USDT: '$', USDC: '$', BTC: '₿' };

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

// Keypad string with thousands separators, for the big number.
export function formatInput(asset: Asset, input: string): string {
  const [whole, frac] = (input || '0').split('.');
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  return SYMBOL[asset] + grouped + (frac !== undefined ? '.' + frac : '');
}

export const ASSET_BLURB: Record<Asset, string> = {
  NGN: 'Spending',
  USDT: 'Dollar savings',
  USDC: 'Dollar savings',
  BTC: 'Long-term holding',
};

export type Fiat = 'NGN' | 'USD';

const FIAT_SYMBOL: Record<Fiat, string> = { NGN: '₦', USD: '$' };

// Value of every balance in one display currency, in that currency's minor
// units (kobo or cents). Built from indicative rates, so it is an estimate
// for the screen and never an amount to transact with.
export function totalValue(
  fiat: Fiat,
  balances: { asset: Asset; available: number }[],
  prices: { ngn: Record<Asset, number>; usd: Record<Asset, number> },
): number {
  const rates = fiat === 'NGN' ? prices.ngn : prices.usd;
  const major = balances.reduce((sum, b) => sum + (b.available / 10 ** DECIMALS[b.asset]) * rates[b.asset], 0);
  return Math.floor(major * 100);
}

// One asset's value in the display currency, in minor units.
export function valueOf(fiat: Fiat, asset: Asset, minor: number, prices: { ngn: Record<Asset, number>; usd: Record<Asset, number> }) {
  return totalValue(fiat, [{ asset, available: minor }], prices);
}

export function formatFiat(fiat: Fiat, minor: number): string {
  const digits = Math.abs(Math.trunc(minor)).toString().padStart(3, '0');
  const whole = digits.slice(0, -2).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  return `${minor < 0 ? '-' : ''}${FIAT_SYMBOL[fiat]}${whole}.${digits.slice(-2)}`;
}

// Short balance for the tab bar: ₦500, ₦6.7k, $55k, ₦1.2m. Truncates rather
// than rounds so it never shows more than the user has.
export function formatCompact(fiat: Fiat, minor: number): string {
  const major = Math.floor(minor / 100);
  const units: [number, string][] = [
    [1e9, 'b'],
    [1e6, 'm'],
    [1e3, 'k'],
  ];
  for (const [size, suffix] of units) {
    if (major >= size) {
      const tenths = Math.floor((major / size) * 10) / 10;
      const text = tenths < 10 && tenths % 1 !== 0 ? tenths.toFixed(1) : String(Math.floor(tenths));
      return `${FIAT_SYMBOL[fiat]}${text}${suffix}`;
    }
  }
  // Small dollar totals keep their cents; $3 would hide most of $3.63.
  if (fiat === 'USD' && major < 100) return `$${(Math.floor(minor) / 100).toFixed(2)}`;
  return `${FIAT_SYMBOL[fiat]}${major}`;
}

// Balance for cards and summaries: dollar stablecoins to the cent (truncated,
// never rounded up); everything else as formatMinor. Use formatMinor where
// the exact amount matters, such as a quote.
export function formatBalance(asset: Asset, minor: number): string {
  if (asset === 'USDT' || asset === 'USDC') {
    return formatMinor(asset, Math.trunc(minor / 10_000) * 10_000);
  }
  return formatMinor(asset, minor);
}
