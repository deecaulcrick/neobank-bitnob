import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { KeyboardAvoidingView, Platform, Pressable, Text, TextInput, View } from 'react-native';

import { AmountField } from '../components/AmountField';
import { Button, Screen, styles } from '../components/ui';
import { api, type Person } from '../lib/api';
import { formatInput, type Asset } from '../lib/money';
import { Cancelled, usePin } from '../lib/pin';
import { refreshBalances } from '../lib/useBalances';
import { colors, space } from '../theme';

// "Send ₦1,500 to @tag": the amount comes from the keypad tab and can be
// changed here without going back.
export default function Send() {
  const params = useLocalSearchParams<{ asset: Asset; amount: string }>();
  const [asset, setAsset] = useState<Asset>(params.asset);
  const [amount, setAmount] = useState(params.amount);
  const { withPin } = usePin();
  const [tag, setTag] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // One key per attempt, so a retry after a dropped response can't send twice.
  const idempotencyKey = useRef(`${Date.now()}-${Math.random().toString(36).slice(2)}`);

  const clean = tag.trim().replace(/^@/, '').toLowerCase();
  const [people, setPeople] = useState<Person[]>([]);

  // People you've paid before, or tags matching what is typed so far.
  useEffect(() => {
    let cancelled = false;
    const timer = setTimeout(
      () =>
        api
          .people(clean)
          .then((r) => !cancelled && setPeople(r.people))
          .catch(() => {}),
      clean ? 250 : 0,
    );
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [clean]);
  const pretty = formatInput(asset, amount);

  async function send() {
    setLoading(true);
    setError('');
    try {
      await withPin((pin) =>
        api.transfer({ to_tag: clean, asset, amount, idempotency_key: idempotencyKey.current }, pin),
      );
      refreshBalances();
      router.replace({ pathname: '/success', params: { message: `You sent ${pretty} to @${clean}` } });
    } catch (e) {
      if (e instanceof Cancelled) return;
      setError(e instanceof Error ? e.message : 'Something went wrong');
    } finally {
      setLoading(false);
    }
  }

  return (
    <Screen sheet>
      <KeyboardAvoidingView
        style={{ flex: 1, justifyContent: 'space-between' }}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        keyboardVerticalOffset={40}>
        <View>
          <Text style={styles.muted}>Send</Text>
          <AmountField
            asset={asset}
            amount={amount}
            fontSize={40}
            onChange={(a, v) => {
              setAsset(a);
              setAmount(v);
              setError('');
            }}
          />
          <View style={[styles.row, { marginTop: space.md, gap: space.md }]}>
            <TextInput
              style={[styles.input, { flex: 1 }]}
              value={tag}
              onChangeText={setTag}
              placeholder="To @tag"
              placeholderTextColor={colors.inkMuted}
              autoCapitalize="none"
              autoCorrect={false}
              autoFocus
              selectionColor={colors.ink}
            />
            <Button
              label="Send"
              style={{ height: 48 }}
              onPress={send}
              loading={loading}
              disabled={!/^[a-z0-9_]{3,20}$/.test(clean)}
            />
          </View>
          <View style={styles.rule} />
          {!!error && <Text style={[styles.error, { marginTop: space.sm }]}>{error}</Text>}
          {people.length > 0 && !people.some((p) => p.tag === clean) && (
            <View style={{ marginTop: space.sm }}>
              {!clean && <Text style={[styles.muted, { marginTop: space.sm }]}>Recent</Text>}
              {people.slice(0, 5).map((p) => (
                <Pressable
                  key={p.tag}
                  onPress={() => setTag(`@${p.tag}`)}
                  style={({ pressed }) => [styles.row, { paddingVertical: 12 }, pressed && { opacity: 0.5 }]}>
                  <Text style={styles.body}>@{p.tag}</Text>
                  {!!p.first_name && <Text style={styles.muted}>{p.first_name}</Text>}
                </Pressable>
              ))}
            </View>
          )}
        </View>

        <View style={{ gap: space.sm }}>
          <Text style={styles.muted}>Or send out</Text>
          <Button
            label="Bank or mobile money"
            variant="secondary"
            onPress={() => router.push({ pathname: '/payout-setup', params: { asset, amount } })}
          />
          {asset !== 'NGN' && (
            <Button
              label={`To a ${asset} address`}
              variant="secondary"
              onPress={() => router.push({ pathname: '/send-crypto', params: { asset, amount } })}
            />
          )}
        </View>
      </KeyboardAvoidingView>
    </Screen>
  );
}
