import { StatusBar } from 'expo-status-bar';
import type { ReactNode } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View, type ViewStyle } from 'react-native';
import { SafeAreaView, type Edge } from 'react-native-safe-area-context';

import { colors, radius, space } from '../theme';

type Tone = 'light' | 'night' | 'accent';

const TONE_BG: Record<Tone, string> = { light: colors.sheet, night: colors.night, accent: colors.accent };

type ScreenProps = { children: ReactNode; tone?: Tone; style?: ViewStyle; edges?: Edge[] };

export function Screen({ children, tone = 'light', style, edges }: ScreenProps) {
  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: TONE_BG[tone] }} edges={edges}>
      <StatusBar style={tone === 'night' ? 'light' : 'dark'} />
      <View style={[{ flex: 1, padding: space.md }, style]}>{children}</View>
    </SafeAreaView>
  );
}

type ButtonProps = {
  label: string;
  onPress: () => void;
  // primary: black pill. secondary: white pill. wash: tinted pill for the accent screen.
  variant?: 'primary' | 'secondary' | 'wash';
  disabled?: boolean;
  loading?: boolean;
  style?: ViewStyle;
};

export function Button({ label, onPress, variant = 'primary', disabled, loading, style }: ButtonProps) {
  const fg = variant === 'primary' ? colors.white : colors.ink;
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      disabled={disabled || loading}
      style={({ pressed }) => [
        styles.button,
        styles[variant],
        (disabled || loading) && { opacity: 0.35 },
        pressed && { opacity: 0.75 },
        style,
      ]}>
      {loading ? <ActivityIndicator color={fg} /> : <Text style={[styles.buttonLabel, { color: fg }]}>{label}</Text>}
    </Pressable>
  );
}

type IconButtonProps = { glyph: string; label: string; onPress: () => void; tone?: Tone };

// Round header button. Glyphs are text so no icon font is needed yet.
export function IconButton({ glyph, label, onPress, tone = 'light' }: IconButtonProps) {
  const bg = tone === 'night' ? colors.onNightWash : tone === 'accent' ? colors.onAccentWash : colors.card;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={8}
      style={({ pressed }) => [styles.iconButton, { backgroundColor: bg }, pressed && { opacity: 0.6 }]}>
      <Text style={{ fontSize: 18, fontWeight: '700', color: tone === 'night' ? colors.white : colors.ink }}>
        {glyph}
      </Text>
    </Pressable>
  );
}

type ChipProps = { label: string; selected?: boolean; onPress?: () => void; tone?: Tone };

export function Chip({ label, selected, onPress, tone = 'light' }: ChipProps) {
  const bg = selected ? colors.ink : tone === 'accent' ? colors.onAccentWash : colors.card;
  return (
    <Pressable onPress={onPress} disabled={!onPress} style={[styles.chip, { backgroundColor: bg }]}>
      <Text style={[styles.chipLabel, selected && { color: colors.white }]}>{label}</Text>
    </Pressable>
  );
}

// Stand-in for screens whose milestone hasn't been built yet.
export function ComingSoon({ title, milestone }: { title: string; milestone: string }) {
  return (
    <Screen style={{ justifyContent: 'center', gap: space.sm }}>
      <Text style={styles.heading}>{title}</Text>
      <Text style={styles.muted}>Lands in {milestone}</Text>
    </Screen>
  );
}

export const styles = StyleSheet.create({
  heading: { color: colors.ink, fontSize: 34, lineHeight: 38, fontWeight: '700', letterSpacing: -0.8 },
  title: { color: colors.ink, fontSize: 22, fontWeight: '700', letterSpacing: -0.3 },
  body: { color: colors.ink, fontSize: 17, fontWeight: '500' },
  muted: { color: colors.inkMuted, fontSize: 15 },
  error: { color: colors.danger, fontSize: 15 },
  amount: { color: colors.ink, fontWeight: '800', letterSpacing: -2 },
  button: { height: 60, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center', paddingHorizontal: space.lg },
  primary: { backgroundColor: colors.ink },
  secondary: { backgroundColor: colors.card },
  wash: { backgroundColor: colors.onAccentWash },
  buttonLabel: { fontSize: 17, fontWeight: '600' },
  iconButton: { width: 48, height: 48, borderRadius: 24, alignItems: 'center', justifyContent: 'center' },
  chip: { paddingHorizontal: space.md, height: 36, justifyContent: 'center', borderRadius: radius.pill },
  chipLabel: { color: colors.ink, fontSize: 14, fontWeight: '600' },
  // Borderless, like typing straight onto the sheet.
  input: { color: colors.ink, fontSize: 22, fontWeight: '500', paddingVertical: space.md },
  rule: { height: StyleSheet.hairlineWidth, backgroundColor: colors.line },
  card: { backgroundColor: colors.card, borderRadius: radius.lg, padding: space.lg },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
});
