import { useEffect, useState } from 'react';
import { Text, View } from 'react-native';

import { Button, Screen, styles } from '../../components/ui';
import { api, type Me } from '../../lib/api';
import { supabase } from '../../lib/supabase';
import { space } from '../../theme';

export default function Profile() {
  const [me, setMe] = useState<Me | null>(null);

  useEffect(() => {
    api.me().then(setMe).catch(() => {});
  }, []);

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.sm }}>
        <Text style={styles.title}>{me?.tag ? `@${me.tag}` : 'Profile'}</Text>
        <Text style={styles.muted}>{me?.phone}</Text>
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
