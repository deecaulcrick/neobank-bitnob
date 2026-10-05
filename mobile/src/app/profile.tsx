import { Text, View } from 'react-native';

import { Button, Chip, Screen, styles } from '../components/ui';
import { setSkyMode, useSkyMode } from '../lib/prefs';
import { supabase } from '../lib/supabase';
import { useMe } from '../lib/useMe';
import { space } from '../theme';

export default function Profile() {
  const me = useMe();
  const sky = useSkyMode();

  return (
    <Screen sheet style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.sm }}>
        <Text style={styles.heading}>{[me?.first_name, me?.last_name].filter(Boolean).join(' ') || 'Your profile'}</Text>
        <Text style={styles.muted}>
          {[me?.tag && `@${me.tag}`, me?.phone && `+${me.phone.replace(/^\+/, '')}`].filter(Boolean).join('  ·  ')}
        </Text>
        <View style={[styles.card, styles.row, { marginTop: space.md }]}>
          <Text style={styles.body}>KYC tier</Text>
          <Text style={styles.body}>{me?.kyc_tier ?? '—'}</Text>
        </View>
        <View style={[styles.card, styles.row]}>
          <Text style={styles.body}>Home sky</Text>
          <View style={{ flexDirection: 'row', gap: space.xs }}>
            <Chip label="Day" tone="sheet" selected={sky === 'day'} onPress={() => setSkyMode('day')} />
            <Chip label="Night" tone="sheet" selected={sky === 'night'} onPress={() => setSkyMode('night')} />
          </View>
        </View>
        {/* TODO: limits used and upgrade KYC, once tier limits are agreed. */}
      </View>
      <Button label="Sign out" variant="secondary" onPress={() => supabase.auth.signOut()} />
    </Screen>
  );
}
