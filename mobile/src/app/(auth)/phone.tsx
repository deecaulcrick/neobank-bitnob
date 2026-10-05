import { router } from 'expo-router';
import { useState } from 'react';
import { Text, TextInput, View } from 'react-native';

import { Button, Screen, styles } from '../../components/ui';
import { supabase } from '../../lib/supabase';
import { colors, space } from '../../theme';

// Onboarding step 1: phone number, then OTP. Name, date of birth, BVN/NIN
// and selfie follow in M1 once Bitnob's customer fields are confirmed.
export default function Phone() {
  const [phone, setPhone] = useState('+234');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function sendCode() {
    setLoading(true);
    setError('');
    const { error } = await supabase.auth.signInWithOtp({ phone });
    setLoading(false);
    if (error) return setError(error.message);
    router.push({ pathname: '/verify', params: { phone } });
  }

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md, marginTop: space.xl }}>
        <Text style={styles.title}>What's your number?</Text>
        <Text style={styles.muted}>We'll text you a code to sign in or create your account.</Text>
        <TextInput
          style={styles.input}
          value={phone}
          onChangeText={setPhone}
          keyboardType="phone-pad"
          autoComplete="tel"
          autoFocus
          placeholderTextColor={colors.muted}
        />
        {!!error && <Text style={styles.error}>{error}</Text>}
      </View>
      <Button label="Continue" onPress={sendCode} loading={loading} disabled={phone.length < 11} />
    </Screen>
  );
}
