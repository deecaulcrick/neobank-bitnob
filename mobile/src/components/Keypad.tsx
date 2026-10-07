import * as Haptics from 'expo-haptics';
import { ChevronLeft } from 'lucide-react-native';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { colors, weight } from '../theme';

const KEYS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '.', '0', 'back'];

// Bare numerals straight on the background, no key outlines.
type KeypadProps = {
  onKey: (key: string) => void;
  // PIN entry has no decimal point.
  decimal?: boolean;
  // Shorter keys, for screens where the keypad shares space.
  compact?: boolean;
};

export function Keypad({ onKey, decimal = true, compact }: KeypadProps) {
  return (
    <View style={styles.grid}>
      {KEYS.map((key) => (
        <Pressable
          key={key}
          accessibilityRole="button"
          accessibilityLabel={key === 'back' ? 'Delete' : key === '.' ? 'Decimal point' : key}
          disabled={key === '.' && !decimal}
          style={({ pressed }) => [styles.key, compact && { height: 54 }, pressed && { opacity: 0.35 }]}
          onPress={() => {
            Haptics.selectionAsync();
            onKey(key);
          }}>
          {key === 'back' ? (
            <ChevronLeft size={30} strokeWidth={2.25} color={colors.ink} />
          ) : (
            <Text style={styles.label}>{key === '.' && !decimal ? '' : key}</Text>
          )}
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  grid: { flexDirection: 'row', flexWrap: 'wrap' },
  key: { width: '33.333%', height: 68, alignItems: 'center', justifyContent: 'center' },
  label: { color: colors.ink, fontSize: 30, fontWeight: weight.medium },
});
