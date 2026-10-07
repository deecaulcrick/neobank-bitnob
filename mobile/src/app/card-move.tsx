import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { Text, View } from 'react-native';

import { AmountField } from '../components/AmountField';
import { Button, Screen, styles } from '../components/ui';
import { api, type CardView } from '../lib/api';
import { formatMinor } from '../lib/money';
import { Cancelled, usePin } from '../lib/pin';
import { refreshBalances } from '../lib/useBalances';
import { space } from '../theme';

const usd = (micro: number) => formatMinor('USDC', Math.trunc(micro / 10_000) * 10_000);

// Load the card from USDC, or move card money back to USDC.
export default function CardMove() {
  const { kind } = useLocalSearchParams<{ kind: 'fund' | 'withdraw' }>();
  const funding = kind === 'fund';
  const { withPin } = usePin();
  const [view, setView] = useState<CardView | null>(null);
  const [amount, setAmount] = useState('5');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // One key per attempt, so a retry after a dropped response can't move money twice.
  const key = useRef(`${Date.now()}-${Math.random().toString(36).slice(2)}`);

  useEffect(() => {
    api.card().then(setView).catch(() => {});
  }, []);

  const micro = Math.round(Number(amount) * 1_000_000);
  const fee = funding ? (view?.fund_fee ?? 0) : 0;

  async function confirm() {
    setLoading(true);
    setError('');
    try {
      await withPin((pin) => api.moveCard(kind, amount, key.current, pin));
      refreshBalances();
      router.replace({
        pathname: '/success',
        params: {
          message: funding
            ? `Adding ${usd(micro)} to your card. It shows on the card in a moment.`
            : `Moving ${usd(micro)} from your card to your USDC balance.`,
          pending: '1',
        },
      });
    } catch (e) {
      if (e instanceof Cancelled) return;
      setError(e instanceof Error ? e.message : 'Something went wrong');
      key.current = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    } finally {
      setLoading(false);
    }
  }

  return (
    <Screen sheet={funding ? 'Add to card' : 'Withdraw from card'} style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md }}>
        <View>
          <Text style={styles.muted}>{funding ? 'From your USDC balance' : 'To your USDC balance'}</Text>
          <AmountField asset="USDC" assets={['USDC']} amount={amount} fontSize={44} onChange={(_, v) => setAmount(v)} />
        </View>
        <View style={[styles.card, { gap: space.md }]}>
          {view?.card?.balance != null && (
            <View style={styles.row}>
              <Text style={styles.muted}>On the card now</Text>
              <Text style={styles.body}>{usd(view.card.balance)}</Text>
            </View>
          )}
          <View style={styles.row}>
            <Text style={styles.muted}>Fee</Text>
            <Text style={styles.body}>{fee ? usd(fee) : 'Free'}</Text>
          </View>
          <View style={styles.row}>
            <Text style={styles.muted}>{funding ? 'Total from USDC' : 'You get'}</Text>
            <Text style={styles.body}>{usd(micro + fee)}</Text>
          </View>
        </View>
        {!!error && <Text style={styles.error}>{error}</Text>}
      </View>
      <Button label={funding ? 'Add to card' : 'Withdraw'} onPress={confirm} loading={loading} disabled={!(micro >= 1_000_000)} />
    </Screen>
  );
}
