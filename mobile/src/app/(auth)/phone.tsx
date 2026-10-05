import { router } from 'expo-router';
import { useState } from 'react';
import { KeyboardAvoidingView, Platform, Text, TextInput, View } from 'react-native';

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
    <Screen>
      <KeyboardAvoidingView
        style={{ flex: 1, justifyContent: 'space-between' }}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={{ gap: space.sm, marginTop: space.xl }}>
          <Text style={styles.heading}>Enter your phone number</Text>
          <Text style={styles.muted}>We'll text you a code to sign in or create your account.</Text>
          <TextInput
            style={[styles.input, { fontSize: 28, marginTop: space.md }]}
            value={phone}
            onChangeText={setPhone}
            keyboardType="phone-pad"
            autoComplete="tel"
            autoFocus
            selectionColor={colors.ink}
          />
          <View style={styles.rule} />
          {!!error && <Text style={styles.error}>{error}</Text>}
        </View>
        <Button label="Continue" onPress={sendCode} loading={loading} disabled={phone.length < 11} />
      </KeyboardAvoidingView>
    </Screen>
  );
}
