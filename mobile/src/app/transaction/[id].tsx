import { router, useLocalSearchParams } from 'expo-router';
import { ArrowLeft, Check, Clock, Undo2 } from 'lucide-react-native';
import { useEffect, useState } from 'react';
import { ActivityIndicator, ScrollView, Share, Text, View } from 'react-native';

import { KIND_LABEL, signedAmount, STATUS_LABEL } from '../../components/ActivityRow';
import { Button, IconButton, Screen, styles } from '../../components/ui';
import { api, type ActivityDetail } from '../../lib/api';
import { colors, space, weight } from '../../theme';

const stamp = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit' });

// One movement: what it was, where it has got to, and the figures behind it.
export default function TransactionDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const [item, setItem] = useState<ActivityDetail | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    api
      .activityItem(id)
      .then(setItem)
      .catch((e) => setError(e instanceof Error ? e.message : 'Could not load that transaction'));
  }, [id]);

  function share() {
    if (!item) return;
    const lines = [
      `${KIND_LABEL[item.kind]}: ${signedAmount(item)} ${item.asset}`,
      `Status: ${STATUS_LABEL[item.status]}`,
      ...item.rows.map((r) => `${r.label}: ${r.value}`),
      `Date: ${stamp(item.created_at)}`,
      `Reference: ${item.reference}`,
    ];
    Share.share({ message: lines.join('\n') });
  }

  const Icon = item?.status === 'done' ? Check : item?.status === 'failed' ? Undo2 : Clock;

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ flex: 1 }}>
        <IconButton icon={ArrowLeft} label="Back" onPress={() => router.back()} />
        {!!error && <Text style={[styles.error, { marginTop: space.md }]}>{error}</Text>}
        {!item && !error && <ActivityIndicator color={colors.ink} style={{ marginTop: space.xl }} />}
        {item && (
          <ScrollView showsVerticalScrollIndicator={false} contentContainerStyle={{ gap: space.md, paddingVertical: space.md }}>
            <View style={{ gap: space.xs }}>
              <Text style={styles.muted}>
                {KIND_LABEL[item.kind]} · {item.title}
              </Text>
              <Text style={[styles.amount, { fontSize: 44, letterSpacing: -1 }]} adjustsFontSizeToFit numberOfLines={1}>
                {signedAmount(item)}
              </Text>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
                <Icon size={18} strokeWidth={2.25} color={colors.ink} />
                <Text style={[styles.body, { fontWeight: weight.medium }]}>{STATUS_LABEL[item.status]}</Text>
              </View>
            </View>

            <View style={[styles.card, { gap: space.md }]}>
              {item.timeline.map((step, i) => (
                <View key={i} style={{ flexDirection: 'row', gap: space.md, opacity: step.at ? 1 : 0.4 }}>
                  <View
                    style={{
                      width: 12,
                      height: 12,
                      borderRadius: 6,
                      marginTop: 5,
                      backgroundColor: step.at ? colors.ink : 'transparent',
                      borderWidth: 2,
                      borderColor: colors.ink,
                    }}
                  />
                  <View style={{ flexShrink: 1 }}>
                    <Text style={styles.body}>{step.label}</Text>
                    <Text style={styles.muted}>{step.at ? stamp(step.at) : 'Waiting'}</Text>
                  </View>
                </View>
              ))}
            </View>

            <View style={[styles.card, { gap: space.md }]}>
              {item.rows.map((row) => (
                <View key={row.label} style={[styles.row, { alignItems: 'flex-start', gap: space.md }]}>
                  <Text style={styles.muted}>{row.label}</Text>
                  <Text selectable style={[styles.body, { flexShrink: 1, textAlign: 'right' }]}>
                    {row.value}
                  </Text>
                </View>
              ))}
              <View style={styles.rule} />
              <View style={[styles.row, { alignItems: 'flex-start', gap: space.md }]}>
                <Text style={styles.muted}>Reference</Text>
                <Text selectable style={[styles.muted, { flexShrink: 1, textAlign: 'right' }]}>
                  {item.reference}
                </Text>
              </View>
            </View>
          </ScrollView>
        )}
      </View>
      {item && <Button label="Share receipt" variant="secondary" onPress={share} />}
    </Screen>
  );
}
