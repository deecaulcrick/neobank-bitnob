import { router, useLocalSearchParams } from 'expo-router';
import { useRef, useState } from 'react';
import { Text, View } from 'react-native';

import { Keypad } from '../components/Keypad';
import { Button, Chip, Screen, styles } from '../components/ui';
import { api } from '../lib/api';
import { appendKey, ASSETS, type Asset } from '../lib/money';
import { colors, space } from '../theme';

// Amount entry for every money action. Asset and destination are chips above
// the number, not form fields.
export default function KeypadScreen() {
  const { action, to } = useLocalSearchParams<{ action: 'send' | 'swap'; to?: string }>();
  const [asset, setAsset] = useState<Asset>('NGN');
  const [amount, setAmount] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // One key per attempt, so a retry after a dropped response can't send twice.
  const idempotencyKey = useRef(`${Date.now()}-${Math.random().toString(36).slice(2)}`);

  async function confirm() {
    if (action === 'swap') {
      return router.push({ pathname: '/swap-review', params: { from: asset, amount } });
    }
    setLoading(true);
    setError('');
    try {
      await api.transfer({ to_tag: to!, asset, amount, idempotency_key: idempotencyKey.current });
      router.dismissAll();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong');
    } finally {
      setLoading(false);
    }
  }

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ alignItems: 'center', gap: space.md }}>
        <View style={{ flexDirection: 'row', gap: space.sm }}>
          {ASSETS.map((a) => (
            <Chip
              key={a}
              label={a}
              selected={a === asset}
              onPress={() => {
                setAsset(a);
                setAmount('');
              }}
            />
          ))}
        </View>
        {!!to && <Chip label={`To @${to}`} />}
        <Text style={{ color: colors.text, fontSize: 64, fontWeight: '700' }} adjustsFontSizeToFit numberOfLines={1}>
          {amount || '0'}
        </Text>
        {/* TODO(M2): max button and live conversion line. */}
        {!!error && <Text style={styles.error}>{error}</Text>}
      </View>

      <View style={{ gap: space.md }}>
        <Keypad onKey={(key) => setAmount((cur) => appendKey(asset, cur, key))} />
        <Button
          label={action === 'swap' ? 'Review' : 'Send'}
          onPress={confirm}
          loading={loading}
          disabled={!(Number(amount) > 0)}
        />
      </View>
    </Screen>
  );
}
