import { Text, View } from 'react-native';

import { Button, Screen, styles } from '../components/ui';
import { supabase } from '../lib/supabase';
import { useMe } from '../lib/useMe';
import { space } from '../theme';

export default function Profile() {
  const me = useMe();

  return (
    <Screen edges={['bottom']} style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.sm }}>
        <Text style={styles.heading}>{me?.tag ? `@${me.tag}` : 'Your profile'}</Text>
        <Text style={styles.muted}>{me?.phone ? `+${me.phone.replace(/^\+/, '')}` : ''}</Text>
        <View style={[styles.card, styles.row, { marginTop: space.md }]}>
          <Text style={styles.body}>KYC tier</Text>
          <Text style={styles.body}>{me?.kyc_tier ?? '—'}</Text>
        </View>
        {/* TODO(M1): pick a tag, limits used, upgrade KYC. */}
      </View>
      <Button label="Sign out" variant="secondary" onPress={() => supabase.auth.signOut()} />
    </Screen>
  );
}
