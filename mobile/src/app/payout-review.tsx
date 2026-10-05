import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Text, View } from 'react-native';

import { Button, Screen, styles } from '../components/ui';
import { api, ApiError, type PayoutQuote } from '../lib/api';
import { formatCurrency, formatMinor, type Asset } from '../lib/money';
import { refreshBalances } from '../lib/useBalances';
import { colors, space } from '../theme';

function Row({ label, value }: { label: string; value: string }) {
  return (
    <View style={styles.row}>
      <Text style={styles.muted}>{label}</Text>
      <Text style={[styles.body, { flexShrink: 1, textAlign: 'right' }]}>{value}</Text>
    </View>
  );
}

const clock = (seconds: number) => `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;

// Confirm a payout at a locked rate. The countdown runs off the quote's own
// expires_at. After sending, the money shows as "Sending" until the rail
// confirms; if it fails or expires it goes back to the balance.
export default function PayoutReview() {
  const p = useLocalSearchParams<{
    asset: Asset;
    amount: string;
    country: string;
    currency: string;
    rail: string;
    name: string;
    fields: string;
    reason: string;
  }>();
  const [quote, setQuote] = useState<PayoutQuote | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [secondsLeft, setSecondsLeft] = useState(0);
  const request = useRef(0);

  const fetchQuote = useCallback(async () => {
    const id = ++request.current;
    setLoading(true);
    setError('');
    try {
      const q = await api.payoutQuote({ country: p.country, currency: p.currency, from_asset: p.asset, amount: p.amount });
      if (id !== request.current) return;
      setQuote(q);
      setSecondsLeft(Math.max(0, Math.round((new Date(q.expires_at).getTime() - Date.now()) / 1000)));
    } catch (e) {
      if (id !== request.current) return;
      setQuote(null);
      setError(e instanceof Error ? e.message : 'Could not get a rate');
    } finally {
      if (id === request.current) setLoading(false);
    }
  }, [p.country, p.currency, p.asset, p.amount]);

  useEffect(() => {
    fetchQuote();
  }, [fetchQuote]);

  useEffect(() => {
    if (!quote) return;
    const timer = setInterval(() => {
      setSecondsLeft(Math.max(0, Math.round((new Date(quote.expires_at).getTime() - Date.now()) / 1000)));
    }, 1000);
    return () => clearInterval(timer);
  }, [quote]);

  const expired = !!quote && secondsLeft <= 5;

  async function send() {
    if (!quote) return;
    setSubmitting(true);
    setError('');
    try {
      const payout = await api.sendPayout({
        quote_id: quote.id,
        beneficiary: { rail: p.rail, account_name: p.name, fields: JSON.parse(p.fields) },
        payment_reason: p.reason,
      });
      refreshBalances();
      const what = `${formatCurrency(payout.to_currency, payout.to_amount)} to ${payout.beneficiary_name}`;
      router.replace({
        pathname: '/success',
        params:
          payout.status === 'success'
            ? { message: `You sent ${what}` }
            : { message: `Sending ${what}. If it doesn't arrive, the money goes back to your balance.`, pending: '1' },
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong');
      refreshBalances();
      if (e instanceof ApiError && (e.status === 410 || e.status === 409)) fetchQuote();
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Screen sheet="Review" style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md }}>
        <View style={[styles.card, { gap: space.md, minHeight: 250, justifyContent: 'center' }]}>
          {loading && !quote ? (
            <ActivityIndicator color={colors.ink} />
          ) : quote ? (
            <View style={{ opacity: expired ? 0.35 : 1, gap: space.md }}>
              <View>
                <Text style={styles.muted}>{p.name} gets</Text>
                <Text style={[styles.amount, { fontSize: 40, letterSpacing: -1 }]} adjustsFontSizeToFit numberOfLines={1}>
                  {formatCurrency(quote.to_currency, quote.to_amount)}
                </Text>
              </View>
              <View style={styles.rule} />
              <Row label="You pay" value={`${formatMinor(quote.from_asset, quote.from_amount)} ${quote.from_asset}`} />
              <Row label="Fee (included)" value={formatMinor(quote.from_asset, quote.fee_amount)} />
              <Row label="To" value={p.name} />
              <Text style={styles.muted}>{expired ? 'This rate has expired.' : `Rate locked for ${clock(secondsLeft)}`}</Text>
            </View>
          ) : (
            <Text style={styles.muted}>No rate available.</Text>
          )}
        </View>
        {!!error && <Text style={styles.error}>{error}</Text>}
        {quote && !quote.enough_funds && !error && (
          <Text style={styles.error}>
            You need {formatMinor(quote.from_asset, quote.from_amount)} {quote.from_asset} including the fee.
          </Text>
        )}
      </View>

      {expired || !quote ? (
        <Button label="Get a new rate" onPress={fetchQuote} loading={loading} />
      ) : (
        // TODO: PIN or biometric before real money.
        <Button label="Send" onPress={send} loading={submitting} disabled={!quote.enough_funds || loading} />
      )}
    </Screen>
  );
}
