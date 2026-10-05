import * as Haptics from 'expo-haptics';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { colors } from '../theme';

const KEYS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '.', '0', 'back'];

// Bare numerals straight on the background, no key outlines.
export function Keypad({ onKey }: { onKey: (key: string) => void }) {
  return (
    <View style={styles.grid}>
      {KEYS.map((key) => (
        <Pressable
          key={key}
          accessibilityRole="button"
          accessibilityLabel={key === 'back' ? 'Delete' : key === '.' ? 'Decimal point' : key}
          style={({ pressed }) => [styles.key, pressed && { opacity: 0.35 }]}
          onPress={() => {
            Haptics.selectionAsync();
            onKey(key);
          }}>
          <Text style={styles.label}>{key === 'back' ? '‹' : key}</Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  key: { width: '33.333%', height: 68, alignItems: 'center', justifyContent: 'center' },
  label: { color: colors.ink, fontSize: 30, fontWeight: '600' },
});
