import { router, useLocalSearchParams } from 'expo-router';
import { useRef, useState } from 'react';
import { KeyboardAvoidingView, Platform, Text, TextInput, View } from 'react-native';

import { Button, Screen, styles } from '../components/ui';
import { api } from '../lib/api';
import { formatInput, type Asset } from '../lib/money';
import { colors, space } from '../theme';

// "Send ₦1,500 to @tag": the amount comes from the keypad tab.
export default function Send() {
  const { asset, amount } = useLocalSearchParams<{ asset: Asset; amount: string }>();
  const [tag, setTag] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // One key per attempt, so a retry after a dropped response can't send twice.
  const idempotencyKey = useRef(`${Date.now()}-${Math.random().toString(36).slice(2)}`);

  const clean = tag.trim().replace(/^@/, '').toLowerCase();
  const pretty = formatInput(asset, amount);

  async function send() {
    setLoading(true);
    setError('');
    try {
      await api.transfer({ to_tag: clean, asset, amount, idempotency_key: idempotencyKey.current });
      router.replace({ pathname: '/success', params: { message: `You sent ${pretty} to @${clean}` } });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong');
    } finally {
      setLoading(false);
    }
  }

  return (
    <Screen edges={['bottom']}>
      <KeyboardAvoidingView
        style={{ flex: 1, justifyContent: 'space-between' }}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        keyboardVerticalOffset={100}>
        <View>
          <Text style={styles.heading}>
            Send {pretty} <Text style={{ color: colors.inkMuted }}>to</Text>
          </Text>
          <View style={[styles.row, { marginTop: space.lg, gap: space.md }]}>
            <TextInput
              style={[styles.input, { flex: 1 }]}
              value={tag}
              onChangeText={setTag}
              placeholder="@tag"
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
          {/* TODO(M5): recent tags and search. */}
        </View>

        <View style={{ gap: space.sm }}>
          <Text style={styles.muted}>Or send out</Text>
          <Button label="Send abroad" variant="secondary" onPress={() => router.push('/payout-setup')} />
          <Button label="Send crypto" variant="secondary" onPress={() => router.push('/receive')} />
        </View>
      </KeyboardAvoidingView>
    </Screen>
  );
}
