import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { KeyboardAvoidingView, Platform, Text, TextInput, View } from 'react-native';

import { Button, IconButton, Screen, styles } from '../../components/ui';
import { supabase } from '../../lib/supabase';
import { colors, space } from '../../theme';

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
    <Screen>
      <KeyboardAvoidingView
        style={{ flex: 1, justifyContent: 'space-between' }}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={{ gap: space.sm }}>
          <IconButton glyph="←" label="Back" onPress={() => router.back()} />
          <Text style={[styles.heading, { marginTop: space.lg }]}>Enter the code</Text>
          <Text style={styles.muted}>Sent to {phone}</Text>
          <TextInput
            style={[styles.input, { fontSize: 28, letterSpacing: 10, marginTop: space.md }]}
            value={code}
            onChangeText={setCode}
            keyboardType="number-pad"
            autoComplete="sms-otp"
            textContentType="oneTimeCode"
            maxLength={6}
            autoFocus
            selectionColor={colors.ink}
          />
          <View style={styles.rule} />
          {!!error && <Text style={styles.error}>{error}</Text>}
        </View>
        <Button label="Verify" onPress={verify} loading={loading} disabled={code.length < 6} />
      </KeyboardAvoidingView>
    </Screen>
  );
}
