import { router, useFocusEffect } from 'expo-router';
import { Eye, EyeOff } from 'lucide-react-native';
import { useCallback, useState } from 'react';
import { Pressable, ScrollView, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ActivityRow } from '../../components/ActivityRow';
import { Avatar } from '../../components/Avatar';
import { CardVisual } from '../../components/CardVisual';
import { Sky } from '../../components/Sky';
import { Button, styles, useStatusBar } from '../../components/ui';
import { api, type ActivityItem } from '../../lib/api';
import { useMeState } from '../../lib/me';
import { ASSET_BLURB, formatBalance, formatFiat, formatMinor, totalValue, valueOf } from '../../lib/money';
import { setHideBalances, useHideBalances, useSkyMode } from '../../lib/prefs';
import { useBalances } from '../../lib/useBalances';
import { colors, radius, space, TAB_BAR_SPACE, weight } from '../../theme';

const HIDDEN = '••••';

// Home: one balance, one tap to switch. The total of every asset, in the
// user's display currency, sits large at the top left; tapping it cycles NGN
// and USD. The per-asset breakdown follows underneath.
export default function Home() {
  const insets = useSafeAreaInsets();
  const { balances, prices, error } = useBalances();
  const { me, setDisplayCurrency } = useMeState();
  const hidden = useHideBalances();
  useStatusBar('light');
  const sky = useSkyMode();
  const [recent, setRecent] = useState<ActivityItem[]>([]);

  useFocusEffect(
    useCallback(() => {
      api
        .activity()
        .then((r) => setRecent(r.items.slice(0, 4)))
        .catch(() => {});
    }, []),
  );

  const fiat = me?.display_currency ?? 'NGN';
  const ngn = balances?.find((b) => b.asset === 'NGN');
  // Without rates there is nothing to total, so fall back to naira alone.
  const total = balances && prices ? formatFiat(fiat, totalValue(fiat, balances, prices)) : null;
  const headline = total ?? (ngn ? formatMinor('NGN', ngn.available) : '—');

  return (
    <View style={{ flex: 1, backgroundColor: colors.night, paddingTop: insets.top }}>
      {/* Sized to the strip above the sheet, so the horizon glow meets its edge. */}
      <View style={{ position: 'absolute', top: 0, left: 0, right: 0, height: insets.top + 210 }}>
        <Sky mode={sky} />
      </View>

      <View style={[styles.row, { paddingHorizontal: space.md, paddingVertical: space.sm, justifyContent: 'flex-end' }]}>
        <Avatar tag={me?.tag} />
      </View>

      {/* The card, tucked behind the sheet. Tap it to open the Card tab. */}
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Your card"
        onPress={() => router.navigate('/card')}
        style={{ marginHorizontal: space.lg, marginTop: space.sm, height: 78 + radius.sheet, marginBottom: -radius.sheet }}>
        <CardVisual tag={me?.tag} compact />
      </Pressable>

      <ScrollView
        style={{ flex: 1, backgroundColor: colors.sheet, borderTopLeftRadius: radius.sheet, borderTopRightRadius: radius.sheet }}
        contentContainerStyle={{ paddingTop: space.sm, paddingBottom: TAB_BAR_SPACE + insets.bottom }}>
        <View style={{ padding: space.md, gap: space.md }}>
          <View style={{ paddingHorizontal: space.sm }}>
            <View style={styles.row}>
              <Text style={styles.body}>{total ? `Total balance · ${fiat}` : 'Naira balance'}</Text>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={hidden ? 'Show balances' : 'Hide balances'}
                onPress={() => setHideBalances(!hidden)}
                hitSlop={12}>
                {hidden ? (
                  <Eye size={26} strokeWidth={2} color={colors.ink} />
                ) : (
                  <EyeOff size={26} strokeWidth={2} color={colors.ink} />
                )}
              </Pressable>
            </View>
            {/* Tapping the total cycles the display currency. */}
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={`Total balance ${hidden ? 'hidden' : headline}. Tap to show in ${fiat === 'NGN' ? 'dollars' : 'naira'}.`}
              disabled={!total}
              onPress={() => setDisplayCurrency(fiat === 'NGN' ? 'USD' : 'NGN')}>
              <Text style={[styles.amount, { fontSize: 56, marginTop: space.xs }]} adjustsFontSizeToFit numberOfLines={1}>
                {hidden ? HIDDEN : headline}
              </Text>
            </Pressable>
            {!!error && <Text style={styles.error}>{error}</Text>}
          </View>

          <View style={{ flexDirection: 'row', gap: space.sm, marginTop: space.sm }}>
            <Button label="Add money" variant="secondary" style={{ flex: 1 }} onPress={() => router.push('/add-money')} />
            <Button label="Send" variant="secondary" style={{ flex: 1 }} onPress={() => router.navigate('/pay')} />
          </View>
        </View>

        {/* What the total is made of, naira included. */}
        <View style={{ paddingHorizontal: space.md, gap: space.md }}>
          {balances?.map((b) => (
            <View key={b.asset} style={[styles.card, styles.row]}>
              <View style={{ gap: 2, flexShrink: 1 }}>
                <Text style={styles.body}>{b.asset}</Text>
                <Text style={[styles.amount, { fontSize: 32, letterSpacing: -1 }]} adjustsFontSizeToFit numberOfLines={1}>
                  {hidden ? HIDDEN : formatBalance(b.asset, b.available)}
                </Text>
                <Text style={styles.muted}>
                  {b.pending > 0 && !hidden
                    ? `${formatBalance(b.asset, b.pending)} in progress`
                    : // A dollar stablecoin's value in dollars would just repeat the line above.
                      prices && !hidden && b.asset !== fiat && !(fiat === 'USD' && b.asset !== 'BTC' && b.asset !== 'NGN')
                      ? `≈ ${formatFiat(fiat, valueOf(fiat, b.asset, b.available, prices))}`
                      : ASSET_BLURB[b.asset]}
                </Text>
              </View>
              <Button
                label="Swap"
                variant="primary"
                style={{ height: 44 }}
                onPress={() => router.push({ pathname: '/swap', params: { from: b.asset } })}
              />
            </View>
          ))}

          {recent.length > 0 && (
            <View style={[styles.card, { paddingVertical: space.sm }]}>
              <View style={[styles.row, { paddingTop: space.sm }]}>
                <Text style={styles.muted}>Recent</Text>
                <Pressable onPress={() => router.navigate('/activity')} hitSlop={12}>
                  <Text style={styles.muted}>See all</Text>
                </Pressable>
              </View>
              {recent.map((item) => (
                <ActivityRow key={item.id} item={item} />
              ))}
            </View>
          )}
        </View>
      </ScrollView>
    </View>
  );
}
