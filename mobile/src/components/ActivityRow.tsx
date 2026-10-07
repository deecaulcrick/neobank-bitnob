import { router } from 'expo-router';
import { Pressable, Text, View } from 'react-native';

import type { ActivityItem, ActivityKind } from '../lib/api';
import { formatMinor } from '../lib/money';
import { colors, radius, weight } from '../theme';
import { styles } from './ui';

export const KIND_LABEL: Record<ActivityKind, string> = {
  deposit: 'Added',
  transfer_in: 'Received',
  transfer_out: 'Sent',
  swap: 'Swap',
  payout: 'Sent out',
  crypto_in: 'Crypto received',
  crypto_out: 'Crypto sent',
  card_create: 'Card',
  card_fund: 'Card',
  card_withdraw: 'Card',
};

// Honest words for a movement that isn't finished or didn't happen.
export const STATUS_LABEL = { pending: 'In progress', failed: 'Returned', done: 'Done' } as const;

export function signedAmount(item: ActivityItem) {
  return `${item.amount > 0 ? '+' : '−'}${formatMinor(item.asset, Math.abs(item.amount))}`;
}

const when = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) +
  ', ' +
  new Date(iso).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });

export function ActivityRow({ item }: { item: ActivityItem }) {
  const failed = item.status === 'failed';
  return (
    <Pressable
      accessibilityRole="button"
      onPress={() => router.push({ pathname: '/transaction/[id]', params: { id: item.id } })}
      style={({ pressed }) => [styles.row, { paddingVertical: 14, gap: 12 }, pressed && { opacity: 0.5 }]}>
      <View style={{ flexShrink: 1, gap: 2 }}>
        <Text style={styles.body} numberOfLines={1}>
          {item.title}
        </Text>
        <Text style={styles.muted}>
          {KIND_LABEL[item.kind]} · {when(item.created_at)}
        </Text>
      </View>
      <View style={{ alignItems: 'flex-end', gap: 4 }}>
        <Text
          style={[
            styles.body,
            { fontWeight: weight.semibold },
            failed && { textDecorationLine: 'line-through', color: colors.inkMuted },
          ]}>
          {signedAmount(item)}
        </Text>
        {item.status !== 'done' && (
          <View style={{ backgroundColor: colors.line, borderRadius: radius.pill, paddingHorizontal: 8, paddingVertical: 2 }}>
            <Text style={{ color: colors.ink, fontSize: 12, fontWeight: weight.medium }}>{STATUS_LABEL[item.status]}</Text>
          </View>
        )}
      </View>
    </Pressable>
  );
}
