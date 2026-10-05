import { router, useLocalSearchParams } from 'expo-router';
import { Check, Clock, X } from 'lucide-react-native';
import { Text, View } from 'react-native';

import { Button, IconButton, Screen, styles } from '../components/ui';
import { colors, space } from '../theme';

// A tick only once the money has actually moved; anything still in flight
// gets a clock and wording that says so.
export default function Success() {
  // `pending` marks a movement that is under way but not yet confirmed.
  const { message, pending } = useLocalSearchParams<{ message: string; pending?: string }>();
  const done = () => router.dismissTo('/');

  return (
    <Screen style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.lg }}>
        <IconButton icon={X} label="Close" onPress={done} />
        <View
          style={{
            width: 72,
            height: 72,
            borderRadius: 36,
            backgroundColor: pending ? colors.card : colors.accent,
            alignItems: 'center',
            justifyContent: 'center',
          }}>
          {pending ? (
            <Clock size={34} strokeWidth={2.25} color={colors.ink} />
          ) : (
            <Check size={36} strokeWidth={2.25} color={colors.ink} />
          )}
        </View>
        <Text style={styles.heading}>{message}</Text>
      </View>
      <Button label="Done" onPress={done} />
    </Screen>
  );
}
