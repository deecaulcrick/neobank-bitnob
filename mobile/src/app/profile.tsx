import { useEffect, useState } from 'react';
import { ScrollView, Text, View } from 'react-native';

import { Button, Chip, Screen, styles } from '../components/ui';
import { api, type Limits } from '../lib/api';
import { formatFiat } from '../lib/money';
import { usePin } from '../lib/pin';
import { setSkyMode, useSkyMode } from '../lib/prefs';
import { supabase } from '../lib/supabase';
import { useMe } from '../lib/useMe';
import { colors, space } from '../theme';

export default function Profile() {
  const me = useMe();
  const sky = useSkyMode();
  const { biometricsAvailable, biometricsOn, setBiometricsOn } = usePin();
  const [limits, setLimits] = useState<Limits | null>(null);

  useEffect(() => {
    api.limits().then(setLimits).catch(() => {});
  }, []);

  const naira = (kobo: number) => formatFiat('NGN', kobo);
  const used = limits ? Math.min(1, limits.used_today / Math.max(1, limits.daily_limit)) : 0;

  return (
    <Screen sheet style={{ justifyContent: 'space-between' }}>
      <ScrollView showsVerticalScrollIndicator={false} contentContainerStyle={{ gap: space.sm, paddingBottom: space.md }}>
        <Text style={styles.heading}>{[me?.first_name, me?.last_name].filter(Boolean).join(' ') || 'Your profile'}</Text>
        <Text style={styles.muted}>
          {[me?.tag && `@${me.tag}`, me?.phone && `+${me.phone.replace(/^\+/, '')}`].filter(Boolean).join('  ·  ')}
        </Text>
        <View style={[styles.card, styles.row, { marginTop: space.md }]}>
          <Text style={styles.body}>Identity</Text>
          <Text style={styles.body}>{me && me.kyc_tier >= 1 ? 'Verified' : 'Not verified'}</Text>
        </View>
        <View style={[styles.card, styles.row]}>
          <Text style={styles.body}>Home sky</Text>
          <View style={{ flexDirection: 'row', gap: space.xs }}>
            <Chip label="Day" tone="sheet" selected={sky === 'day'} onPress={() => setSkyMode('day')} />
            <Chip label="Night" tone="sheet" selected={sky === 'night'} onPress={() => setSkyMode('night')} />
          </View>
        </View>
        {limits && (
          <View style={[styles.card, { gap: space.sm }]}>
            <View style={styles.row}>
              <Text style={styles.body}>Sent today</Text>
              <Text style={styles.body}>
                {naira(limits.used_today)} of {naira(limits.daily_limit)}
              </Text>
            </View>
            <View style={{ height: 6, borderRadius: 3, backgroundColor: colors.line, overflow: 'hidden' }}>
              <View style={{ height: 6, width: `${used * 100}%`, backgroundColor: colors.ink }} />
            </View>
            <Text style={styles.muted}>
              {naira(limits.remaining)} left today · up to {naira(limits.single_limit)} at once · {limits.sends_today} of{' '}
              {limits.max_sends} sends
            </Text>
            {!!limits.new_account_until && (
              <Text style={styles.muted}>
                New account: up to {naira(limits.new_account_daily_limit)} a day to banks, mobile money and crypto
                addresses until {new Date(limits.new_account_until).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}.
              </Text>
            )}
          </View>
        )}
        {biometricsAvailable && (
          <View style={[styles.card, styles.row]}>
            <View style={{ flexShrink: 1 }}>
              <Text style={styles.body}>Face ID or fingerprint</Text>
              <Text style={styles.muted}>Confirm payments without typing your PIN</Text>
            </View>
            <View style={{ flexDirection: 'row', gap: space.xs }}>
              <Chip label="On" tone="sheet" selected={biometricsOn} onPress={() => setBiometricsOn(true)} />
              <Chip label="Off" tone="sheet" selected={!biometricsOn} onPress={() => setBiometricsOn(false)} />
            </View>
          </View>
        )}
        {/* TODO: change PIN, and upgrade KYC once higher tiers exist. */}
      </ScrollView>
      <Button label="Sign out" variant="secondary" onPress={() => supabase.auth.signOut()} />
    </Screen>
  );
}
