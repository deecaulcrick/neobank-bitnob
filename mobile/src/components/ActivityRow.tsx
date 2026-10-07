import { router } from 'expo-router';
import { ArrowDownLeft, ArrowLeftRight, ArrowUpRight, CreditCard, type LucideIcon } from 'lucide-react-native';
import { Pressable, Text, View } from 'react-native';

import type { ActivityItem, ActivityKind } from '../lib/api';
import { ASSETS, formatBalance, formatCurrency, formatMinor, type Asset } from '../lib/money';
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

type Look = { icon: LucideIcon; wash: string; ink: string };
const IN: Look = { icon: ArrowDownLeft, wash: colors.inWash, ink: colors.positive };
const OUT: Look = { icon: ArrowUpRight, wash: colors.outWash, ink: colors.ink };
const SWAP: Look = { icon: ArrowLeftRight, wash: colors.swapWash, ink: colors.ink };
const CARD: Look = { icon: CreditCard, wash: colors.cardWash, ink: colors.cardInk };

// One colour per direction of travel, so the list can be read by its left edge.
const LOOK: Record<ActivityKind, Look> = {
  deposit: IN,
  transfer_in: IN,
  crypto_in: IN,
  transfer_out: OUT,
  payout: OUT,
  crypto_out: OUT,
  swap: SWAP,
  card_create: CARD,
  card_fund: CARD,
  card_withdraw: CARD,
};

export function KindBadge({ kind, size = 44 }: { kind: ActivityKind; size?: number }) {
  const { icon: Icon, wash, ink } = LOOK[kind];
  return (
    <View
      style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: wash, alignItems: 'center', justifyContent: 'center' }}>
      <Icon size={size * 0.45} strokeWidth={2} color={ink} />
    </View>
  );
}

export function StatusPill({ status }: { status: ActivityItem['status'] }) {
  if (status === 'done') return null;
  const failed = status === 'failed';
  return (
    <View
      style={{
        backgroundColor: failed ? colors.dangerWash : colors.swapWash,
        borderRadius: radius.pill,
        paddingHorizontal: 8,
        paddingVertical: 2,
      }}>
      <Text style={{ color: failed ? colors.danger : colors.ink, fontSize: 12, fontWeight: weight.medium }}>
        {STATUS_LABEL[status]}
      </Text>
    </View>
  );
}

// Every digit, for the transaction's own page.
export function signedAmount(item: ActivityItem) {
  return `${item.amount > 0 ? '+' : '−'}${formatMinor(item.asset, Math.abs(item.amount))}`;
}

// Lists show dollars to the cent.
const listAmount = (item: ActivityItem) => `${item.amount > 0 ? '+' : '−'}${formatBalance(item.asset, Math.abs(item.amount))}`;

// The other half of a swap ("+$2.01 USDC") or what a payout arrived as ("GHS 50.00").
function otherSide(item: ActivityItem) {
  if (!item.other_currency || item.other_amount == null) return '';
  const amount = Math.abs(item.other_amount);
  if (item.kind === 'swap' && (ASSETS as string[]).includes(item.other_currency)) {
    const to = item.other_currency as Asset;
    return `+${formatBalance(to, amount)}${to === 'USDT' || to === 'USDC' ? ` ${to}` : ''}`;
  }
  return item.other_currency === item.asset ? '' : formatCurrency(item.other_currency, amount);
}

// Banks send names in capitals; set them in ordinary case.
const tidy = (title: string) =>
  /[a-z]/.test(title) ? title : title.toLowerCase().replace(/(^|[\s'-])\p{L}/gu, (m) => m.toUpperCase());

const time = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
const date = (iso: string) => new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });

// `dated` adds the day, for lists that are not already grouped by day.
export function ActivityRow({ item, dated }: { item: ActivityItem; dated?: boolean }) {
  const failed = item.status === 'failed';
  const other = failed ? '' : otherSide(item);
  return (
    <Pressable
      accessibilityRole="button"
      onPress={() => router.push({ pathname: '/transaction/[id]', params: { id: item.id } })}
      style={({ pressed }) => [{ flexDirection: 'row', alignItems: 'center', paddingVertical: 12, gap: 12 }, pressed && { opacity: 0.5 }]}>
      <KindBadge kind={item.kind} />
      <View style={{ flex: 1, gap: 2 }}>
        <Text style={[styles.body, { fontWeight: weight.medium }]} numberOfLines={1}>
          {tidy(item.title)}
        </Text>
        <Text style={styles.muted} numberOfLines={1}>
          {KIND_LABEL[item.kind]} · {dated ? `${date(item.created_at)}, ` : ''}
          {time(item.created_at)}
        </Text>
      </View>
      <View style={{ alignItems: 'flex-end', gap: 3 }}>
        <Text
          style={[
            styles.body,
            { fontWeight: weight.semibold, fontVariant: ['tabular-nums'] },
            item.amount > 0 && { color: colors.positive },
            failed && { textDecorationLine: 'line-through', color: colors.inkMuted },
          ]}>
          {listAmount(item)}
        </Text>
        {item.status !== 'done' ? <StatusPill status={item.status} /> : !!other && <Text style={styles.muted}>{other}</Text>}
      </View>
    </Pressable>
  );
}
