import { Text, View } from 'react-native';

import { Screen, styles } from '../../components/ui';
import { formatMinor } from '../../lib/money';
import { useBalances } from '../../lib/useBalances';
import { space } from '../../theme';

export default function Assets() {
  const { balances, error } = useBalances();

  return (
    <Screen style={{ gap: space.sm }}>
      <Text style={[styles.title, { marginBottom: space.sm }]}>Assets</Text>
      {!!error && <Text style={styles.error}>{error}</Text>}
      {balances?.map((b) => (
        <View key={b.asset} style={[styles.card, styles.row]}>
          <Text style={styles.body}>{b.asset}</Text>
          <View style={{ alignItems: 'flex-end' }}>
            <Text style={styles.body}>{formatMinor(b.asset, b.available)}</Text>
            {b.pending > 0 && <Text style={styles.muted}>{formatMinor(b.asset, b.pending)} sending</Text>}
          </View>
        </View>
      ))}
      {/* TODO(M2): value in display currency and 24h change for BTC. */}
    </Screen>
  );
}
