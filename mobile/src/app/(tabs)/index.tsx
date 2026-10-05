import { router } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useState } from 'react';
import { Pressable, ScrollView, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Avatar } from '../../components/Avatar';
import { Button, styles } from '../../components/ui';
import { ASSET_BLURB, formatMinor } from '../../lib/money';
import { useBalances } from '../../lib/useBalances';
import { useMe } from '../../lib/useMe';
import { colors, radius, space, TAB_BAR_SPACE } from '../../theme';

const HIDDEN = '••••';

// Home: dark chrome up top, then a light sheet with one big number.
export default function Home() {
  const insets = useSafeAreaInsets();
  const { balances, error } = useBalances();
  const me = useMe();
  const [hidden, setHidden] = useState(false);

  const ngn = balances?.find((b) => b.asset === 'NGN');
  const others = balances?.filter((b) => b.asset !== 'NGN') ?? [];

  return (
    <View style={{ flex: 1, backgroundColor: colors.night, paddingTop: insets.top }}>
      <StatusBar style="light" />

      <View style={[styles.row, { paddingHorizontal: space.md, paddingVertical: space.sm, justifyContent: 'flex-end' }]}>
        <Avatar tag={me?.tag} />
      </View>

      {/* Identity strip tucked behind the sheet. */}
      <View
        style={[
          styles.row,
          {
            marginHorizontal: space.md,
            marginTop: space.md,
            padding: space.md,
            paddingBottom: space.md + radius.sheet,
            marginBottom: -radius.sheet,
            backgroundColor: colors.nightRaised,
            borderTopLeftRadius: radius.lg,
            borderTopRightRadius: radius.lg,
          },
        ]}>
        <View style={{ backgroundColor: colors.onNightWash, borderRadius: radius.pill, paddingHorizontal: 14, paddingVertical: 8 }}>
          <Text style={{ color: colors.white, fontSize: 14, fontWeight: '600' }}>Tier {me?.kyc_tier ?? 0}</Text>
        </View>
        <Text style={{ color: colors.white, fontSize: 17, fontWeight: '500', opacity: 0.8 }}>
          {me?.tag ? `@${me.tag}` : 'No tag yet'}
        </Text>
      </View>

      <ScrollView
        style={{ flex: 1, backgroundColor: colors.sheet, borderTopLeftRadius: radius.sheet, borderTopRightRadius: radius.sheet }}
        contentContainerStyle={{ padding: space.md, paddingTop: space.lg, paddingBottom: TAB_BAR_SPACE + insets.bottom, gap: space.md }}>
        <View style={{ paddingHorizontal: space.sm }}>
          <View style={styles.row}>
            {/* TODO(M2): total across all assets in the display currency, tap
                to cycle NGN/USD. Needs prices; until then this is naira only. */}
            <Text style={styles.body}>Naira balance</Text>
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={hidden ? 'Show balances' : 'Hide balances'}
              onPress={() => setHidden((h) => !h)}
              hitSlop={12}>
              <Text style={[styles.muted, { fontWeight: '600' }]}>{hidden ? 'Show' : 'Hide'}</Text>
            </Pressable>
          </View>
          <Text style={[styles.amount, { fontSize: 56, marginTop: space.xs }]} adjustsFontSizeToFit numberOfLines={1}>
            {hidden ? HIDDEN : ngn ? formatMinor('NGN', ngn.available) : '—'}
          </Text>
          {!!ngn && ngn.pending > 0 && !hidden && (
            <Text style={styles.muted}>{formatMinor('NGN', ngn.pending)} sending</Text>
          )}
          {!!error && <Text style={styles.error}>{error}</Text>}
        </View>

        <View style={{ flexDirection: 'row', gap: space.sm, marginTop: space.md }}>
          <Button label="Add money" variant="secondary" style={{ flex: 1 }} onPress={() => router.push('/add-money')} />
          <Button label="Send" variant="secondary" style={{ flex: 1 }} onPress={() => router.navigate('/pay')} />
        </View>

        {others.map((b) => (
          <View key={b.asset} style={[styles.card, styles.row]}>
            <View style={{ gap: 2 }}>
              <Text style={styles.body}>{b.asset}</Text>
              <Text style={[styles.amount, { fontSize: 32, letterSpacing: -1 }]}>
                {hidden ? HIDDEN : formatMinor(b.asset, b.available)}
              </Text>
              <Text style={styles.muted}>
                {b.pending > 0 && !hidden ? `${formatMinor(b.asset, b.pending)} sending` : ASSET_BLURB[b.asset]}
              </Text>
            </View>
            <Button label="Swap" variant="primary" style={{ height: 44 }} onPress={() => router.navigate('/pay')} />
          </View>
        ))}
      </ScrollView>
    </View>
  );
}
