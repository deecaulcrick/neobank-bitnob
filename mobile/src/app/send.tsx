import { router } from 'expo-router';
import { useState } from 'react';
import { Text, TextInput, View } from 'react-native';

import { Button, Screen, styles } from '../components/ui';
import { colors, space } from '../theme';

// Send picker: a tag for in-app sends, or out to a bank / crypto address.
export default function Send() {
  const [tag, setTag] = useState('');
  const clean = tag.trim().replace(/^@/, '').toLowerCase();

  return (
    <Screen style={{ gap: space.md }}>
      <TextInput
        style={styles.input}
        value={tag}
        onChangeText={setTag}
        placeholder="@tag"
        placeholderTextColor={colors.muted}
        autoCapitalize="none"
        autoCorrect={false}
        autoFocus
      />
      <Button
        label="Next"
        disabled={!/^[a-z0-9_]{3,20}$/.test(clean)}
        onPress={() => router.push({ pathname: '/keypad', params: { action: 'send', to: clean } })}
      />
      {/* TODO(M5): recent tags and search. */}
      <View style={{ gap: space.sm, marginTop: space.lg }}>
        <Text style={styles.muted}>Or send out</Text>
        <Button label="Send abroad" variant="secondary" onPress={() => router.push('/payout-setup')} />
        <Button label="Send crypto" variant="secondary" onPress={() => router.push('/receive')} />
      </View>
    </Screen>
  );
}
