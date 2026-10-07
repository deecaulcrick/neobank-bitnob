import { router, useFocusEffect } from 'expo-router';
import { ArrowDownToLine, Eye, Lock, LockOpen, Plus } from 'lucide-react-native';
import { useCallback, useRef, useState } from 'react';
import { ActivityIndicator, Pressable, RefreshControl, ScrollView, Text, View } from 'react-native';

import { AmountField } from '../../components/AmountField';
import { CardVisual } from '../../components/CardVisual';
import { Button, Screen, styles } from '../../components/ui';
import { api, newKey, type CardStatement, type CardView } from '../../lib/api';
import { formatMinor } from '../../lib/money';
import { Cancelled, usePin } from '../../lib/pin';
import { CARD_THEMES, setCardTheme, useCardTheme, type CardTheme } from '../../lib/prefs';
import { refreshBalances } from '../../lib/useBalances';
import { useMe } from '../../lib/useMe';
import { colors, radius, space, TAB_BAR_SPACE, weight } from '../../theme';

const usd = (micro: number) => formatMinor('USDC', Math.trunc(micro / 10_000) * 10_000);

function Action({ icon: Icon, label, onPress }: { icon: typeof Plus; label: string; onPress: () => void }) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [{ alignItems: 'center', gap: 6, flex: 1 }, pressed && { opacity: 0.5 }]}>
      <View style={{ width: 54, height: 54, borderRadius: 27, backgroundColor: colors.card, alignItems: 'center', justifyContent: 'center' }}>
        <Icon size={22} strokeWidth={2} color={colors.ink} />
      </View>
      <Text style={styles.muted}>{label}</Text>
    </Pressable>
  );
}

// The virtual dollar card: get one, see its balance, load it, lock it.
export default function CardTab() {
  const me = useMe();
  const theme = useCardTheme();
  const { withPin } = usePin();
  const [view, setView] = useState<CardView | null>(null);
  const [statement, setStatement] = useState<CardStatement[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [load, setLoad] = useState('5');
  // One key per attempt, so a retry can't issue two cards.
  const key = useRef('');

  const refresh = useCallback(async () => {
    try {
      const v = await api.card();
      setView(v);
      setError('');
      if (v.card && v.card.status !== 'pending') {
        api
          .cardTransactions()
          .then((r) => setStatement(r.transactions))
          .catch(() => {});
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not load your card');
    }
  }, []);

  useFocusEffect(
    useCallback(() => {
      refresh();
    }, [refresh]),
  );

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setError('');
    try {
      await action();
      await refresh();
    } catch (e) {
      if (!(e instanceof Cancelled)) setError(e instanceof Error ? e.message : 'Something went wrong');
    } finally {
      setBusy(false);
    }
  }

  const create = () =>
    run(async () => {
      await withPin((pin) => api.createCard(load, (key.current ||= newKey()), pin));
      refreshBalances();
    });

  const card = view?.card;
  const locked = card?.status === 'frozen';

  return (
    <Screen edges={['top']} style={{ paddingBottom: 0 }}>
      <ScrollView
        showsVerticalScrollIndicator={false}
        contentContainerStyle={{ gap: space.md, paddingBottom: TAB_BAR_SPACE + space.xl }}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            tintColor={colors.ink}
            onRefresh={async () => {
              setRefreshing(true);
              await refresh();
              setRefreshing(false);
            }}
          />
        }>
        <Text style={styles.heading}>Card</Text>
        {!view && !error && <ActivityIndicator color={colors.ink} style={{ marginTop: space.xl }} />}

        {view && (
          <View style={{ paddingHorizontal: space.sm, paddingVertical: space.md }}>
            <CardVisual tag={me?.tag} last4={card?.last4} locked={locked} tilt />
          </View>
        )}
        {!!error && <Text style={styles.error}>{error}</Text>}

        {/* No card yet: verify, then load and create. */}
        {view && !card && view.kyc_status !== 'approved' && (
          <View style={{ gap: space.md }}>
            <Text style={styles.title}>A dollar card for paying online</Text>
            <Text style={styles.muted}>
              A virtual Visa card you load from your USDC balance. Use it for subscriptions, shopping and anything
              priced in dollars. Lock it whenever you like.
            </Text>
            {view.kyc_status === 'pending' ? (
              <Text style={styles.body}>We're checking your details. This screen updates when that's done.</Text>
            ) : (
              <>
                {view.kyc_status === 'rejected' && (
                  <Text style={styles.error}>We couldn't verify your details. Check them and try again.</Text>
                )}
                <Button label="Get your card" onPress={() => router.push('/card-setup')} />
              </>
            )}
          </View>
        )}

        {view && !card && view.kyc_status === 'approved' && (
          <View style={{ gap: space.md }}>
            <Text style={styles.title}>You're verified. Load your card.</Text>
            <View>
              <Text style={styles.muted}>Start it with</Text>
              <AmountField asset="USDC" assets={['USDC']} amount={load} onChange={(_, v) => setLoad(v)} />
            </View>
            <Text style={styles.muted}>
              One-time card fee {usd(view.creation_fee)}. Total from your USDC balance:{' '}
              {usd(Math.round(Number(load) * 1_000_000) + view.creation_fee)}.
            </Text>
            <Button label="Create card" onPress={create} loading={busy} disabled={!(Number(load) >= 1)} />
          </View>
        )}

        {card?.status === 'pending' && (
          <View style={{ gap: space.sm }}>
            <Text style={styles.title}>Your card is being made</Text>
            <Text style={styles.muted}>This usually takes under a minute. Pull down to check.</Text>
          </View>
        )}

        {card && card.status !== 'pending' && (
          <>
            <View>
              <Text style={styles.muted}>Card balance</Text>
              <Text style={[styles.amount, { fontSize: 48 }]} adjustsFontSizeToFit numberOfLines={1}>
                {card.balance === null ? '—' : usd(card.balance)}
              </Text>
            </View>

            <View style={{ flexDirection: 'row', marginTop: space.sm }}>
              <Action icon={Plus} label="Add" onPress={() => router.push({ pathname: '/card-move', params: { kind: 'fund' } })} />
              <Action
                icon={ArrowDownToLine}
                label="Withdraw"
                onPress={() => router.push({ pathname: '/card-move', params: { kind: 'withdraw' } })}
              />
              <Action icon={Eye} label="Details" onPress={() => router.push('/card-details')} />
              <Action
                icon={locked ? LockOpen : Lock}
                label={locked ? 'Unlock' : 'Lock'}
                onPress={() => run(() => api.lockCard(!locked))}
              />
            </View>
            {locked && <Text style={styles.muted}>Your card is locked. Every payment will be declined until you unlock it.</Text>}

            <View style={[styles.card, styles.row]}>
              <Text style={styles.body}>Colour</Text>
              <View style={{ flexDirection: 'row', gap: space.sm }}>
                {(Object.keys(CARD_THEMES) as CardTheme[]).map((t) => (
                  <Pressable
                    key={t}
                    accessibilityRole="button"
                    accessibilityLabel={`${t} card`}
                    onPress={() => setCardTheme(t)}
                    style={{
                      width: 30,
                      height: 30,
                      borderRadius: radius.pill,
                      backgroundColor: CARD_THEMES[t].bg,
                      borderWidth: t === theme ? 3 : 1,
                      borderColor: t === theme ? colors.ink : colors.line,
                    }}
                  />
                ))}
              </View>
            </View>

            <View style={[styles.card, { paddingVertical: space.md, gap: space.sm }]}>
              <Text style={styles.muted}>Card activity</Text>
              {statement.length === 0 && <Text style={styles.body}>Nothing yet.</Text>}
              {statement.map((t) => (
                <View key={t.id} style={[styles.row, { gap: space.md }]}>
                  <View style={{ flexShrink: 1 }}>
                    <Text style={styles.body} numberOfLines={1}>
                      {t.description || t.type}
                    </Text>
                    <Text style={styles.muted}>
                      {new Date(t.created_at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}
                      {t.status !== 'completed' ? ` · ${t.status}` : ''}
                    </Text>
                  </View>
                  <Text style={[styles.body, { fontWeight: weight.semibold }]}>{usd(t.amount)}</Text>
                </View>
              ))}
            </View>
          </>
        )}
      </ScrollView>
    </Screen>
  );
}
