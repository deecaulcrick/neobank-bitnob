import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Text, View } from 'react-native';

import { Button, Chip, Screen, styles } from '../components/ui';
import { api, ApiError, type SwapQuote } from '../lib/api';
import { ASSETS, formatInput, formatMinor, type Asset } from '../lib/money';
import { refreshBalances } from '../lib/useBalances';
import { colors, radius, space } from '../theme';

// "1 USDT = ₦1,346.07", always quoting the pricier unit so the number reads well.
function describeRate(q: SwapQuote) {
  const rate = Number(q.rate);
  if (!(rate > 0)) return '';
  const [unit, per, value] = rate >= 1 ? [q.from_asset, q.to_asset, rate] : [q.to_asset, q.from_asset, 1 / rate];
  const text = value.toLocaleString('en-US', { maximumFractionDigits: value >= 100 ? 2 : 6 });
  return `1 ${unit} = ${text} ${per}`;
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <View style={styles.row}>
      <Text style={styles.muted}>{label}</Text>
      <Text style={styles.body}>{value}</Text>
    </View>
  );
}

// Confirm a trade at a locked rate. The countdown runs off the quote's own
// expires_at; an expired quote refreshes silently once, then asks.
export default function SwapReview() {
  const { from, amount } = useLocalSearchParams<{ from: Asset; amount: string }>();
  const [to, setTo] = useState<Asset>(from === 'NGN' ? 'USDT' : 'NGN');
  const [quote, setQuote] = useState<SwapQuote | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [secondsLeft, setSecondsLeft] = useState(0);
  const [lifetime, setLifetime] = useState(30);
  const [expired, setExpired] = useState(false);
  const autoRefreshed = useRef(false);
  const request = useRef(0);

  const fetchQuote = useCallback(async () => {
    const id = ++request.current;
    setLoading(true);
    setError('');
    setExpired(false);
    try {
      const q = await api.swapQuote({ from, to, amount });
      if (id !== request.current) return; // a newer request superseded this one
      setQuote(q);
      const seconds = Math.max(0, Math.round((new Date(q.expires_at).getTime() - Date.now()) / 1000));
      setLifetime(Math.max(seconds, 1));
      setSecondsLeft(seconds);
    } catch (e) {
      if (id !== request.current) return;
      setQuote(null);
      setError(e instanceof Error ? e.message : 'Could not get a rate');
    } finally {
      if (id === request.current) setLoading(false);
    }
  }, [from, to, amount]);

  useEffect(() => {
    autoRefreshed.current = false;
    fetchQuote();
  }, [fetchQuote]);

  useEffect(() => {
    if (!quote || submitting) return;
    const timer = setInterval(() => {
      const left = Math.round((new Date(quote.expires_at).getTime() - Date.now()) / 1000);
      setSecondsLeft(Math.max(0, left));
      if (left <= 2) {
        clearInterval(timer);
        if (autoRefreshed.current) {
          setExpired(true);
        } else {
          autoRefreshed.current = true;
          fetchQuote();
        }
      }
    }, 1000);
    return () => clearInterval(timer);
  }, [quote, submitting, fetchQuote]);

  async function confirm() {
    if (!quote) return;
    setSubmitting(true);
    setError('');
    try {
      const trade = await api.swap(quote.id);
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
      setError(e instanceof Error ? e.message : 'Something went wrong');
      // 410: the rate expired between the countdown and the tap.
      if (e instanceof ApiError && e.status === 410) fetchQuote();
    } finally {
      setSubmitting(false);
    }
  }

  const stale = expired || !quote;

  return (
    <Screen sheet="Review swap" style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md }}>
        <View style={{ gap: space.sm }}>
          <Text style={styles.muted}>Swap {formatInput(from, amount)} to</Text>
          <View style={{ flexDirection: 'row', gap: space.xs }}>
            {ASSETS.filter((a) => a !== from).map((a) => (
              <Chip key={a} label={a} selected={a === to} onPress={() => setTo(a)} />
            ))}
          </View>
        </View>

        <View style={[styles.card, { gap: space.md, minHeight: 230, justifyContent: 'center' }]}>
          {loading && !quote ? (
            <ActivityIndicator color={colors.ink} />
          ) : quote ? (
            <>
              <View style={{ opacity: expired ? 0.35 : 1, gap: space.md }}>
                <View>
                  <Text style={styles.muted}>You get</Text>
                  <Text style={[styles.amount, { fontSize: 40, letterSpacing: -1 }]} adjustsFontSizeToFit numberOfLines={1}>
                    {formatMinor(quote.to_asset, quote.to_amount)}
                  </Text>
                </View>
                <View style={styles.rule} />
                <Row label="You pay" value={formatMinor(quote.from_asset, quote.from_amount)} />
                <Row label="Rate" value={describeRate(quote)} />
                <Row label="Fee (included)" value={formatMinor(quote.to_asset, quote.fee_amount)} />
              </View>
              <View style={{ gap: space.xs }}>
                <View style={{ height: 4, borderRadius: radius.pill, backgroundColor: colors.line, overflow: 'hidden' }}>
                  <View
                    style={{
                      height: 4,
                      width: `${expired ? 0 : Math.min(100, (secondsLeft / lifetime) * 100)}%`,
                      backgroundColor: colors.ink,
                    }}
                  />
                </View>
                <Text style={styles.muted}>
                  {expired ? 'This rate has expired.' : `Rate locked for ${secondsLeft}s`}
                </Text>
              </View>
            </>
          ) : (
            <Text style={styles.muted}>No rate available.</Text>
          )}
        </View>

        {!!error && <Text style={styles.error}>{error}</Text>}
        {quote && !quote.enough_funds && !error && (
          <Text style={styles.error}>You don't have enough {quote.from_asset} for this swap.</Text>
        )}
      </View>

      {stale ? (
        <Button label="Get a new rate" onPress={fetchQuote} loading={loading} />
      ) : (
        // TODO: slide-to-confirm, and PIN or biometric, before real money.
        <Button label="Swap" onPress={confirm} loading={submitting} disabled={!quote.enough_funds || loading} />
      )}
    </Screen>
  );
}
