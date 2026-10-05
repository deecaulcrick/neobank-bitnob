import type { ReactNode } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View, type ViewStyle } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { colors, radius, space } from '../theme';

export function Screen({ children, style }: { children: ReactNode; style?: ViewStyle }) {
  return (
    <SafeAreaView style={styles.safe}>
      <View style={[styles.screen, style]}>{children}</View>
    </SafeAreaView>
  );
}

type ButtonProps = {
  label: string;
  onPress: () => void;
  variant?: 'primary' | 'secondary';
  disabled?: boolean;
  loading?: boolean;
  style?: ViewStyle;
};

export function Button({ label, onPress, variant = 'primary', disabled, loading, style }: ButtonProps) {
  const primary = variant === 'primary';
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      disabled={disabled || loading}
      style={({ pressed }) => [
        styles.button,
        primary ? styles.primary : styles.secondary,
        (disabled || loading) && styles.disabled,
        pressed && styles.pressed,
        style,
      ]}>
      {loading ? (
        <ActivityIndicator color={primary ? colors.onAccent : colors.text} />
      ) : (
        <Text style={[styles.buttonLabel, { color: primary ? colors.onAccent : colors.text }]}>{label}</Text>
      )}
    </Pressable>
  );
}

// Chips sit above the keypad number: asset and destination, not form fields.
export function Chip({ label, selected, onPress }: { label: string; selected?: boolean; onPress?: () => void }) {
  return (
    <Pressable onPress={onPress} style={[styles.chip, selected && styles.chipSelected]}>
      <Text style={[styles.chipLabel, selected && { color: colors.onAccent }]}>{label}</Text>
    </Pressable>
  );
}

// Stand-in for screens whose milestone hasn't been built yet.
export function ComingSoon({ title, milestone, children }: { title: string; milestone: string; children?: ReactNode }) {
  return (
    <Screen style={{ justifyContent: 'center', alignItems: 'center', gap: space.sm }}>
      <Text style={styles.title}>{title}</Text>
      <Text style={styles.muted}>Lands in {milestone}</Text>
      {children}
    </Screen>
  );
}

export const styles = StyleSheet.create({
  safe: { flex: 1, backgroundColor: colors.bg },
  screen: { flex: 1, padding: space.md, backgroundColor: colors.bg },
  title: { color: colors.text, fontSize: 24, fontWeight: '700' },
  body: { color: colors.text, fontSize: 16 },
  muted: { color: colors.muted, fontSize: 14 },
  error: { color: colors.danger, fontSize: 14 },
  button: { height: 56, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center', paddingHorizontal: space.lg },
  primary: { backgroundColor: colors.accent },
  secondary: { backgroundColor: colors.surface },
  disabled: { opacity: 0.4 },
  pressed: { opacity: 0.8 },
  buttonLabel: { fontSize: 17, fontWeight: '600' },
  chip: { paddingHorizontal: space.md, paddingVertical: space.sm, borderRadius: radius.pill, backgroundColor: colors.surface },
  chipSelected: { backgroundColor: colors.accent },
  chipLabel: { color: colors.text, fontSize: 14, fontWeight: '600' },
  input: {
    height: 56,
    borderRadius: radius.md,
    backgroundColor: colors.surface,
    color: colors.text,
    fontSize: 18,
    paddingHorizontal: space.md,
  },
  card: { backgroundColor: colors.surface, borderRadius: radius.lg, padding: space.md },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
});
