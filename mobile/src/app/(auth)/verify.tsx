import { useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Text, TextInput, View } from 'react-native';

import { Button, Screen, styles } from '../../components/ui';
import { supabase } from '../../lib/supabase';
import { space } from '../../theme';

export default function Verify() {
  const { phone } = useLocalSearchParams<{ phone: string }>();
  const [code, setCode] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function verify() {
    setLoading(true);
    setError('');
    const { error } = await supabase.auth.verifyOtp({ phone, token: code, type: 'sms' });
    setLoading(false);
    // On success the session listener flips the root guard and shows the tabs.
    if (error) setError(error.message);
  }

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md, marginTop: space.xl }}>
        <Text style={styles.title}>Enter the code</Text>
        <Text style={styles.muted}>Sent to {phone}</Text>
        <TextInput
          style={[styles.input, { letterSpacing: 8 }]}
          value={code}
          onChangeText={setCode}
          keyboardType="number-pad"
          autoComplete="sms-otp"
          textContentType="oneTimeCode"
          maxLength={6}
          autoFocus
        />
        {!!error && <Text style={styles.error}>{error}</Text>}
      </View>
      <Button label="Verify" onPress={verify} loading={loading} disabled={code.length < 6} />
    </Screen>
  );
}
