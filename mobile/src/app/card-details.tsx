import * as Clipboard from 'expo-clipboard';
import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { ActivityIndicator, Pressable, Text, View } from 'react-native';

import { Button, Screen, styles } from '../components/ui';
import { api, type CardSecrets } from '../lib/api';
import { Cancelled, usePin } from '../lib/pin';
import { colors, space, weight } from '../theme';

// The full card number, expiry and security code. Asked for behind the PIN
// each time, shown here only, and gone when the sheet closes.
export default function CardDetails() {
  const { withPin } = usePin();
  const [secrets, setSecrets] = useState<CardSecrets | null>(null);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState('');

  useEffect(() => {
    withPin((pin) => api.revealCard(pin))
      .then(setSecrets)
      .catch((e) => {
        if (e instanceof Cancelled) router.back();
        else setError(e instanceof Error ? e.message : 'Could not load your card details');
      });
    // Runs once, when the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function copy(label: string, value: string) {
    await Clipboard.setStringAsync(value);
    setCopied(label);
    setTimeout(() => setCopied(''), 1500);
  }

  const Line = ({ label, value, shown }: { label: string; value: string; shown?: string }) => (
    <Pressable onPress={() => copy(label, value)} style={({ pressed }) => [styles.row, { gap: space.md }, pressed && { opacity: 0.5 }]}>
      <Text style={styles.muted}>{label}</Text>
      <Text style={[styles.body, { fontWeight: weight.medium, flexShrink: 1, textAlign: 'right' }]}>
        {copied === label ? 'Copied' : (shown ?? value)}
      </Text>
    </Pressable>
  );

  return (
    <Screen sheet="Card details" style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md }}>
        {!secrets && !error && <ActivityIndicator color={colors.ink} style={{ marginTop: space.xl }} />}
        {!!error && <Text style={styles.error}>{error}</Text>}
        {secrets && (
          <>
            <View style={[styles.card, { gap: space.md }]}>
              <Line label="Card number" value={secrets.number} shown={secrets.number.replace(/(.{4})/g, '$1 ').trim()} />
              <View style={styles.rule} />
              <Line label="Expires" value={`${secrets.expiry_month}/${secrets.expiry_year.slice(-2)}`} />
              <Line label="Security code" value={secrets.cvv} />
              <Line label="Name on card" value={secrets.name} />
            </View>
            {!!secrets.billing_address && (
              <View style={[styles.card, { gap: space.sm }]}>
                <Text style={styles.muted}>Billing address</Text>
                <Pressable onPress={() => copy('address', secrets.billing_address)}>
                  <Text style={styles.body}>{copied === 'address' ? 'Copied' : secrets.billing_address}</Text>
                </Pressable>
              </View>
            )}
            <Text style={styles.muted}>Tap anything to copy it. Don't share these with anyone.</Text>
          </>
        )}
      </View>
      <Button label="Done" variant="secondary" onPress={() => router.back()} />
    </Screen>
  );
}
