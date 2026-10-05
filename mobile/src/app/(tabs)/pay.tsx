import { router } from 'expo-router';
import { useState } from 'react';
import { Text, View } from 'react-native';

import { Avatar } from '../../components/Avatar';
import { Keypad } from '../../components/Keypad';
import { Button, Chip, Screen, styles } from '../../components/ui';
import { appendKey, ASSETS, formatInput, type Asset } from '../../lib/money';
import { useMe } from '../../lib/useMe';
import { space, TAB_BAR_SPACE } from '../../theme';

// The keypad tab: amount first, then choose what to do with it.
export default function Pay() {
  const me = useMe();
  const [asset, setAsset] = useState<Asset>('NGN');
  const [amount, setAmount] = useState('');
  const valid = Number(amount) > 0;

  return (
    <Screen tone="accent" edges={['top']} style={{ paddingBottom: TAB_BAR_SPACE }}>
      <View style={styles.row}>
        <View style={{ flexDirection: 'row', gap: space.xs }}>
          {ASSETS.map((a) => (
            <Chip
              key={a}
              label={a}
              tone="accent"
              selected={a === asset}
              onPress={() => {
                setAsset(a);
                setAmount('');
              }}
            />
          ))}
        </View>
        <Avatar tag={me?.tag} />
      </View>

      <View style={{ flex: 1, justifyContent: 'center', alignItems: 'center' }}>
        <Text style={[styles.amount, { fontSize: 88 }]} adjustsFontSizeToFit numberOfLines={1}>
          {formatInput(asset, amount)}
        </Text>
        {/* TODO(M2): max button and live conversion line. */}
      </View>

      <Keypad onKey={(key) => setAmount((cur) => appendKey(asset, cur, key))} />

      <View style={{ gap: space.sm, marginTop: space.sm }}>
        <View style={{ flexDirection: 'row', gap: space.sm }}>
          <Button label="Add" variant="wash" style={{ flex: 1 }} onPress={() => router.push('/add-money')} />
          <Button
            label="Swap"
            variant="wash"
            style={{ flex: 1 }}
            disabled={!valid}
            onPress={() => router.push({ pathname: '/swap-review', params: { from: asset, amount } })}
          />
        </View>
        <Button
          label="Send"
          disabled={!valid}
          onPress={() => router.push({ pathname: '/send', params: { asset, amount } })}
        />
      </View>
    </Screen>
  );
}
