import { useFocusEffect } from 'expo-router';
import { useCallback, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, FlatList, RefreshControl, ScrollView, Text, View } from 'react-native';

import { ActivityRow } from '../../components/ActivityRow';
import { Chip, Screen, styles } from '../../components/ui';
import { api, type ActivityItem, type ActivityKind } from '../../lib/api';
import { ASSETS, type Asset } from '../../lib/money';
import { colors, space, TAB_BAR_SPACE } from '../../theme';

const TYPES: { label: string; kinds: ActivityKind[] }[] = [
  { label: 'All', kinds: [] },
  { label: 'Added', kinds: ['deposit', 'transfer_in', 'crypto_in'] },
  { label: 'Sent', kinds: ['transfer_out', 'payout', 'crypto_out'] },
  { label: 'Swaps', kinds: ['swap'] },
  { label: 'Card', kinds: ['card_create', 'card_fund', 'card_withdraw'] },
];

// "Today", "Yesterday", then the date; the year only once it is not this one.
function dayLabel(iso: string) {
  const d = new Date(iso);
  const now = new Date();
  const days = Math.round((new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() - new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()) / 86_400_000);
  if (days === 0) return 'Today';
  if (days === 1) return 'Yesterday';
  return d.toLocaleDateString(undefined, {
    weekday: days < 7 ? 'long' : undefined,
    day: 'numeric',
    month: 'short',
    year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric',
  });
}

// Full history, filtered by asset and type, newest first.
export default function Activity() {
  const [asset, setAsset] = useState<Asset | ''>('');
  const [type, setType] = useState(0);
  const [items, setItems] = useState<ActivityItem[] | null>(null);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [more, setMore] = useState(true);
  const request = useRef(0);

  const load = useCallback(
    async (after?: ActivityItem[]) => {
      const id = ++request.current;
      try {
        const page = await api.activity({
          asset: asset || undefined,
          kinds: TYPES[type].kinds,
          before: after?.[after.length - 1]?.created_at,
        });
        if (id !== request.current) return; // filters changed meanwhile
        setItems(after ? [...after, ...page.items] : page.items);
        setMore(page.items.length >= 30);
        setError('');
      } catch (e) {
        if (id === request.current) setError(e instanceof Error ? e.message : 'Could not load activity');
      }
    },
    [asset, type],
  );

  useFocusEffect(
    useCallback(() => {
      load();
    }, [load]),
  );

  // One card per day, newest first, as the list already is.
  const days = useMemo(() => {
    const out: { label: string; items: ActivityItem[] }[] = [];
    for (const item of items ?? []) {
      const label = dayLabel(item.created_at);
      if (out[out.length - 1]?.label === label) out[out.length - 1].items.push(item);
      else out.push({ label, items: [item] });
    }
    return out;
  }, [items]);

  return (
    <Screen edges={['top']} style={{ paddingBottom: 0 }}>
      <Text style={[styles.heading, { marginBottom: space.md }]}>Activity</Text>
      {/* One row of filters: what happened, then which balance. */}
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={{ flexGrow: 0, marginBottom: space.md, marginHorizontal: -space.md }}
        contentContainerStyle={{ gap: space.xs, alignItems: 'center', paddingHorizontal: space.md }}>
        {TYPES.map((t, i) => (
          <Chip
            key={t.label}
            label={t.label}
            selected={i === type}
            onPress={() => {
              setItems(null);
              setType(i);
            }}
          />
        ))}
        <View style={{ width: 1, height: 20, backgroundColor: colors.line, marginHorizontal: space.xs }} />
        {ASSETS.map((a) => (
          <Chip
            key={a}
            label={a}
            selected={a === asset}
            onPress={() => {
              setItems(null);
              setAsset(a === asset ? '' : a);
            }}
          />
        ))}
      </ScrollView>

      {!!error && <Text style={styles.error}>{error}</Text>}
      {!items && !error ? (
        <ActivityIndicator color={colors.ink} style={{ marginTop: space.xl }} />
      ) : (
        <FlatList
          data={days}
          keyExtractor={(day) => day.label}
          renderItem={({ item: day }) => (
            <View style={{ marginBottom: space.md }}>
              <Text style={[styles.muted, { marginLeft: space.sm, marginBottom: space.sm }]}>{day.label}</Text>
              <View style={[styles.card, { paddingVertical: space.xs, paddingHorizontal: space.md }]}>
                {day.items.map((item, i) => (
                  <View key={item.id}>
                    {/* The rule starts under the text, clear of the badges. */}
                    {i > 0 && <View style={[styles.rule, { marginLeft: 56 }]} />}
                    <ActivityRow item={item} />
                  </View>
                ))}
              </View>
            </View>
          )}
          ListEmptyComponent={<Text style={[styles.muted, { marginTop: space.xl, textAlign: 'center' }]}>Nothing here yet.</Text>}
          contentContainerStyle={{ paddingBottom: TAB_BAR_SPACE + space.xl }}
          showsVerticalScrollIndicator={false}
          onEndReachedThreshold={0.4}
          onEndReached={() => {
            if (more && items?.length) load(items);
          }}
          refreshControl={
            <RefreshControl
              refreshing={refreshing}
              tintColor={colors.ink}
              onRefresh={async () => {
                setRefreshing(true);
                await load();
                setRefreshing(false);
              }}
            />
          }
        />
      )}
    </Screen>
  );
}
