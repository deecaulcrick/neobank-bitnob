import { router, useLocalSearchParams } from 'expo-router';
import { Text, View } from 'react-native';

import { Button, IconButton, Screen, styles } from '../components/ui';
import { colors, space } from '../theme';

// Only shown once the money has actually moved. Payouts will show "Sending"
// here until the rail confirms.
export default function Success() {
  const { message } = useLocalSearchParams<{ message: string }>();
  const done = () => router.dismissTo('/');

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.lg }}>
        <IconButton glyph="✕" label="Close" onPress={done} />
        <View
          style={{
            width: 72,
            height: 72,
            borderRadius: 36,
            backgroundColor: colors.accent,
            alignItems: 'center',
            justifyContent: 'center',
          }}>
          <Text style={{ fontSize: 34, fontWeight: '700', color: colors.ink }}>✓</Text>
        </View>
        <Text style={styles.heading}>{message}</Text>
      </View>
      <Button label="Done" onPress={done} />
    </Screen>
  );
}
