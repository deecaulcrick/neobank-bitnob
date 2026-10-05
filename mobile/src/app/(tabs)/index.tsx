import { router } from 'expo-router';
import { Text, View } from 'react-native';

import { Button, Screen, styles } from '../../components/ui';
import { formatMinor } from '../../lib/money';
import { useBalances } from '../../lib/useBalances';
import { colors, space } from '../../theme';

// Home: one big number and three verbs.
export default function Home() {
  const { balances, error } = useBalances();
  const ngn = balances?.find((b) => b.asset === 'NGN');

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ alignItems: 'center', marginTop: space.xl * 2, gap: space.sm }}>
        {/* TODO(M2): total across all assets in the display currency, tap to
            cycle NGN/USD. Needs prices; until then this is the NGN balance. */}
        <Text style={styles.muted}>NGN balance</Text>
        <Text style={{ color: colors.text, fontSize: 56, fontWeight: '700' }} adjustsFontSizeToFit numberOfLines={1}>
          {ngn ? formatMinor('NGN', ngn.available) : '—'}
        </Text>
        {!!error && <Text style={styles.error}>{error}</Text>}
      </View>

      <View style={{ flexDirection: 'row', gap: space.sm }}>
        <Button label="Add" variant="secondary" style={{ flex: 1 }} onPress={() => router.push('/add-money')} />
        <Button
          label="Swap"
          variant="secondary"
          style={{ flex: 1 }}
          onPress={() => router.push({ pathname: '/keypad', params: { action: 'swap' } })}
        />
        <Button label="Send" style={{ flex: 1 }} onPress={() => router.push('/send')} />
      </View>
    </Screen>
  );
}
