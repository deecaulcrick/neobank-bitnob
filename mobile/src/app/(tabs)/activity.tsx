import { useFocusEffect } from 'expo-router';
import { useCallback, useRef, useState } from 'react';
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
];

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

  return (
    <Screen edges={['top']} style={{ paddingBottom: 0 }}>
      <Text style={[styles.heading, { marginBottom: space.md }]}>Activity</Text>
      <View style={{ gap: space.sm, marginBottom: space.sm }}>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: space.xs }}>
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
        </ScrollView>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: space.xs }}>
          {(['', ...ASSETS] as (Asset | '')[]).map((a) => (
            <Chip
              key={a || 'all'}
              label={a || 'Any asset'}
              selected={a === asset}
              onPress={() => {
                setItems(null);
                setAsset(a);
              }}
            />
          ))}
        </ScrollView>
      </View>

      {!!error && <Text style={styles.error}>{error}</Text>}
      {!items && !error ? (
        <ActivityIndicator color={colors.ink} style={{ marginTop: space.xl }} />
      ) : (
        <FlatList
          data={items ?? []}
          keyExtractor={(item) => item.id}
          renderItem={({ item }) => <ActivityRow item={item} />}
          ItemSeparatorComponent={() => <View style={styles.rule} />}
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
