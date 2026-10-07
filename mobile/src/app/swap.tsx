import { router, useLocalSearchParams } from 'expo-router';
import { ArrowUpDown } from 'lucide-react-native';
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { ActivityIndicator, Pressable, Text, View } from 'react-native';

import { Keypad } from '../components/Keypad';
import { Button, Chip, Screen, styles } from '../components/ui';
import { api, ApiError, type SwapQuote } from '../lib/api';
import { appendKey, ASSETS, formatBalance, formatInput, formatMinor, minorToInput, type Asset } from '../lib/money';
import { Cancelled, usePin } from '../lib/pin';
import { refreshBalances, useBalances } from '../lib/useBalances';
import { colors, radius, space } from '../theme';

// "1 USDT = 1,346.07 NGN", always quoting the pricier unit so the number reads well.
function describeRate(q: SwapQuote) {
  const rate = Number(q.rate);
  if (!(rate > 0)) return '';
  const [unit, per, value] = rate >= 1 ? [q.from_asset, q.to_asset, rate] : [q.to_asset, q.from_asset, 1 / rate];
  return `1 ${unit} = ${value.toLocaleString('en-US', { maximumFractionDigits: value >= 100 ? 2 : 6 })} ${per}`;
}

type SideProps = {
  label: string;
  value: string;
  // The side the keypad is typing into.
  active: boolean;
  onFocus: () => void;
  asset: Asset;
  choices: Asset[];
  picking: boolean;
  onPick: () => void;
  onChoose: (a: Asset) => void;
  muted?: boolean;
  footer?: ReactNode;
};

// One half of the swap: a figure and the asset it is in. Tapping the asset
// opens the choices in place.
function Side({ label, value, active, onFocus, asset, choices, picking, onPick, onChoose, muted, footer }: SideProps) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${label} ${value}. Tap to type this amount.`}
      onPress={onFocus}
      style={[
        styles.card,
        { padding: space.md, gap: space.xs, borderWidth: 2, borderColor: active ? colors.ink : 'transparent' },
      ]}>
      <View style={styles.row}>
        <Text style={styles.muted}>{label}</Text>
        {footer}
      </View>
      <View style={[styles.row, { gap: space.sm }]}>
        <Text
          style={[styles.amount, { fontSize: 36, letterSpacing: -1, flexShrink: 1 }, muted && { color: colors.inkMuted }]}
          adjustsFontSizeToFit
          numberOfLines={1}>
          {value}
        </Text>
        <Chip label={`${asset} ▾`} tone="sheet" onPress={onPick} />
      </View>
      {picking && (
        <View style={{ flexDirection: 'row', gap: space.xs, marginTop: space.xs }}>
          {choices.map((a) => (
            <Chip key={a} label={a} tone="sheet" selected={a === asset} onPress={() => onChoose(a)} />
          ))}
        </View>
      )}
    </Pressable>
  );
}

// Swap on one screen: what you pay, what you get, and a keypad. Type either
// figure (tap a card to choose which) and the other follows from the rate.
// "I want exactly 3 USDT" is typed into You get.
export default function Swap() {
  const params = useLocalSearchParams<{ from?: Asset; amount?: string }>();
  const [from, setFrom] = useState<Asset>(params.from ?? 'NGN');
  const [to, setTo] = useState<Asset>(params.from && params.from !== 'NGN' ? 'NGN' : 'USDT');
  // `amount` is whatever was typed, on the side it was typed into.
  const [side, setSide] = useState<'pay' | 'get'>('pay');
  const [amount, setAmount] = useState(params.amount ?? '');
  const [picking, setPicking] = useState<'from' | 'to' | null>(null);
  const [quote, setQuote] = useState<SwapQuote | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [secondsLeft, setSecondsLeft] = useState(0);
  const [expired, setExpired] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const autoRefreshed = useRef(false);
  const request = useRef(0);
  const { withPin } = usePin();
  const available = useBalances().balances?.find((b) => b.asset === from)?.available;
  const valid = Number(amount) > 0;

  const fetchQuote = useCallback(async () => {
    const id = ++request.current;
    setLoading(true);
    setError('');
    setExpired(false);
    try {
      const q = await api.swapQuote({ from, to, amount, side });
      if (id !== request.current) return; // superseded by a newer edit
      setQuote(q);
      setSecondsLeft(Math.max(0, Math.round((new Date(q.expires_at).getTime() - Date.now()) / 1000)));
    } catch (e) {
      if (id !== request.current) return;
      setQuote(null);
      setError(e instanceof Error ? e.message : 'Could not get a rate');
    } finally {
      if (id === request.current) setLoading(false);
    }
  }, [from, to, amount, side]);

  // Requote a moment after the last edit.
  useEffect(() => {
    request.current++;
    setQuote(null);
    setError('');
    setExpired(false);
    autoRefreshed.current = false;
    if (!valid) {
      setLoading(false);
      return;
    }
    setLoading(true);
    const timer = setTimeout(fetchQuote, 600);
    return () => clearTimeout(timer);
  }, [fetchQuote, valid]);

  // Count down on the quote's own expiry; refresh silently once, then ask.
  useEffect(() => {
    if (!quote || submitting) return;
    const timer = setInterval(() => {
      const left = Math.round((new Date(quote.expires_at).getTime() - Date.now()) / 1000);
      setSecondsLeft(Math.max(0, left));
      if (left <= 2) {
        clearInterval(timer);
        if (autoRefreshed.current) setExpired(true);
        else {
          autoRefreshed.current = true;
          fetchQuote();
        }
      }
    }, 1000);
    return () => clearInterval(timer);
  }, [quote, submitting, fetchQuote]);

  // Swap the two sides. The figure you typed stays with its currency, so
  // "pay 3 USDT" becomes "get 3 USDT" and back.
  function flip() {
    setFrom(to);
    setTo(from);
    setSide(side === 'pay' ? 'get' : 'pay');
    setPicking(null);
  }

  // Move the keypad to the other figure, starting from what the rate gave.
  function focus(next: 'pay' | 'get') {
    setPicking(null);
    if (next === side) return;
    const shown = quote ? (next === 'pay' ? minorToInput(from, quote.from_amount) : minorToInput(to, quote.to_amount)) : '';
    setSide(next);
    setAmount(shown);
  }

  const typed = side === 'pay' ? from : to;
  const payText = side === 'pay' ? formatInput(from, amount) : quote ? formatMinor(from, quote.from_amount) : formatInput(from, '');
  const getText = side === 'get' ? formatInput(to, amount) : quote ? formatMinor(to, quote.to_amount) : formatInput(to, '');

  function choose(which: 'from' | 'to', a: Asset) {
    setPicking(null);
    if (which === 'from') {
      if (a === to) return flip();
      setFrom(a);
      if (side === 'pay') setAmount('');
    } else {
      if (a === from) return flip();
      setTo(a);
      if (side === 'get') setAmount('');
    }
  }

  async function confirm() {
    if (!quote) return;
    setSubmitting(true);
    setError('');
    try {
      const trade = await withPin((pin) => api.swap(quote.id, pin));
      refreshBalances();
      const gave = formatMinor(trade.from_asset, trade.from_amount);
      const got = `${formatMinor(trade.to_asset, trade.to_amount)} ${trade.to_asset}`;
      router.replace({
        pathname: '/success',
        params:
          trade.status === 'completed'
            ? { message: `You swapped ${gave} for ${got}` }
            : { message: `Swapping ${gave} for ${got}. Your balance updates when it completes.`, pending: '1' },
      });
    } catch (e) {
      if (e instanceof Cancelled) return;
      setError(e instanceof Error ? e.message : 'Something went wrong');
      // The quote is spent or stale either way; get a fresh one.
      if (e instanceof ApiError && (e.status === 410 || e.status === 409)) fetchQuote();
    } finally {
      setSubmitting(false);
    }
  }

  const short = !!quote && !quote.enough_funds;
  const status = error
    ? error
    : short
      ? `You only have ${available !== undefined ? formatBalance(from, available) : 'less than that'}.`
      : expired
        ? 'This rate has expired.'
        : quote
          ? `${describeRate(quote)} · fee ${formatMinor(quote.to_asset, quote.fee_amount)} · locked ${secondsLeft}s`
          : valid
            ? 'Getting a rate…'
            : 'Enter an amount';

  return (
    <Screen sheet="Swap" style={{ justifyContent: 'space-between' }}>
      <View>
        <Side
          label="You pay"
          value={payText}
          active={side === 'pay'}
          onFocus={() => focus('pay')}
          muted={side === 'get' && (!quote || expired)}
          asset={from}
          choices={ASSETS}
          picking={picking === 'from'}
          onPick={() => setPicking(picking === 'from' ? null : 'from')}
          onChoose={(a) => choose('from', a)}
          footer={
            available !== undefined && available > 0 ? (
              <Pressable
                onPress={() => {
                  setSide('pay');
                  setAmount(minorToInput(from, available));
                }}
                hitSlop={10}>
                <Text style={styles.muted}>Max {formatBalance(from, available)}</Text>
              </Pressable>
            ) : undefined
          }
        />
        <View style={{ alignItems: 'center', height: space.sm, zIndex: 1 }}>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Swap the other way"
            onPress={flip}
            hitSlop={10}
            style={({ pressed }) => [
              {
                width: 44,
                height: 44,
                marginTop: -18,
                borderRadius: radius.pill,
                backgroundColor: colors.ink,
                alignItems: 'center',
                justifyContent: 'center',
                borderWidth: 4,
                borderColor: colors.sheet,
              },
              pressed && { opacity: 0.7 },
            ]}>
            <ArrowUpDown size={18} strokeWidth={2.25} color={colors.white} />
          </Pressable>
        </View>
        <Side
          label="You get"
          value={getText}
          active={side === 'get'}
          onFocus={() => focus('get')}
          muted={side === 'pay' && (!quote || expired)}
          asset={to}
          choices={ASSETS.filter((a) => a !== from)}
          picking={picking === 'to'}
          onPick={() => setPicking(picking === 'to' ? null : 'to')}
          onChoose={(a) => choose('to', a)}
          footer={loading ? <ActivityIndicator size="small" color={colors.ink} /> : undefined}
        />
        <Text style={[error || short ? styles.error : styles.muted, { marginTop: space.sm, textAlign: 'center' }]}>
          {status}
        </Text>
      </View>

      <View style={{ gap: space.sm }}>
        <Keypad compact onKey={(key) => setAmount((cur) => appendKey(typed, cur, key))} />
        {expired ? (
          <Button label="Get a new rate" onPress={fetchQuote} loading={loading} />
        ) : (
          <Button label="Swap" onPress={confirm} loading={submitting} disabled={!quote || short || loading} />
        )}
      </View>
    </Screen>
  );
}
