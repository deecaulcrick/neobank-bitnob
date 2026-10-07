import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Text, View } from 'react-native';

import { AmountField } from '../components/AmountField';
import { Button, Screen, styles } from '../components/ui';
import { api, ApiError, type PayoutQuote } from '../lib/api';
import { formatCurrency, formatMinor, minorToInput, type Asset } from '../lib/money';
import { Cancelled, usePin } from '../lib/pin';
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
  // The amount starts as typed on the keypad and can be changed here, e.g.
  // when it turns out to be under the corridor's minimum.
  const [asset, setAsset] = useState<Asset>(p.asset);
  const [amount, setAmount] = useState(p.amount);
  // Set when the user types exactly what the recipient should get instead.
  const [receive, setReceive] = useState('');
  const { withPin } = usePin();
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
      const q = await api.payoutQuote({
        country: p.country,
        currency: p.currency,
        from_asset: asset,
        ...(receive ? { settlement_amount: receive } : { amount }),
      });
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
  }, [p.country, p.currency, asset, amount, receive]);

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
      const payout = await withPin((pin) =>
        api.sendPayout(
          {
            quote_id: quote.id,
            beneficiary: { rail: p.rail, account_name: p.name, fields: JSON.parse(p.fields) },
            payment_reason: p.reason,
          },
          pin,
        ),
      );
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
      if (e instanceof Cancelled) return;
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
        <View style={{ gap: space.sm }}>
          <View>
            <Text style={styles.muted}>You send{receive ? ' about' : ''}</Text>
            <AmountField
              asset={asset}
              amount={receive ? (quote ? minorToInput(quote.from_asset, quote.from_amount) : '') : amount}
              placeholder="…"
              onChange={(a, v) => {
                setAsset(a);
                setAmount(v);
                setReceive('');
              }}
            />
          </View>
          <View>
            <Text style={styles.muted}>{p.name} gets{receive ? ' exactly' : ''}</Text>
            <AmountField
              asset={asset}
              fiat={p.currency}
              fontSize={28}
              amount={receive || (quote ? (quote.to_amount / 100).toFixed(2) : '')}
              placeholder="…"
              onChange={(_, v) => setReceive(v)}
            />
          </View>
        </View>
        <View style={[styles.card, { gap: space.md, minHeight: 170, justifyContent: 'center' }]}>
          {loading && !quote ? (
            <ActivityIndicator color={colors.ink} />
          ) : quote ? (
            <View style={{ opacity: expired ? 0.35 : 1, gap: space.md }}>
              <Row label="You pay" value={`${formatMinor(quote.from_asset, quote.from_amount)} ${quote.from_asset}`} />
              <Row label="Fee (included)" value={formatMinor(quote.from_asset, quote.fee_amount)} />
              <Row label="They get" value={formatCurrency(quote.to_currency, quote.to_amount)} />
              <Text style={styles.muted}>{expired ? 'This rate has expired.' : `Rate locked for ${clock(secondsLeft)}`}</Text>
            </View>
          ) : (
            <Text style={styles.muted}>Change the amount above to try again.</Text>
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
