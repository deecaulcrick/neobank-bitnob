import { useFocusEffect } from 'expo-router';
import { setStatusBarStyle } from 'expo-status-bar';
import type { LucideIcon } from 'lucide-react-native';
import { useCallback, type ReactNode } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View, type ViewStyle } from 'react-native';
import { SafeAreaView, type Edge } from 'react-native-safe-area-context';

import { colors, weight, radius, space } from '../theme';

type Tone = 'light' | 'night' | 'accent';

const TONE_BG: Record<Tone, string> = { light: colors.sheet, night: colors.night, accent: colors.accent };

// Tabs stay mounted, so the status bar follows whichever screen has focus.
export function useStatusBar(style: 'light' | 'dark') {
  useFocusEffect(useCallback(() => setStatusBarStyle(style, true), [style]));
}

type ScreenProps = {
  children: ReactNode;
  tone?: Tone;
  style?: ViewStyle;
  edges?: Edge[];
  // Modal sheets: draws the grabber that signals swipe-down-to-close, and an
  // optional title, in place of a native header.
  sheet?: boolean | string;
};

export function Screen({ children, tone = 'light', style, edges, sheet }: ScreenProps) {
  useStatusBar(tone === 'night' ? 'light' : 'dark');
  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: TONE_BG[tone] }} edges={sheet ? ['bottom'] : edges}>
      {!!sheet && (
        <View style={styles.sheetHeader}>
          <View style={styles.grabber} />
          {typeof sheet === 'string' && <Text style={styles.sheetTitle}>{sheet}</Text>}
        </View>
      )}
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

type IconButtonProps = { icon: LucideIcon; label: string; onPress: () => void; tone?: Tone };

// Round header button.
export function IconButton({ icon: Icon, label, onPress, tone = 'light' }: IconButtonProps) {
  const bg = tone === 'night' ? colors.onNightWash : tone === 'accent' ? colors.onAccentWash : colors.card;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={8}
      style={({ pressed }) => [styles.iconButton, { backgroundColor: bg }, pressed && { opacity: 0.6 }]}>
      <Icon size={22} strokeWidth={2} color={tone === 'night' ? colors.white : colors.ink} />
    </Pressable>
  );
}

type ChipProps = { label: string; selected?: boolean; onPress?: () => void; tone?: Tone | 'sheet' };

export function Chip({ label, selected, onPress, tone = 'light' }: ChipProps) {
  const bg = selected ? colors.ink : tone === 'accent' ? colors.onAccentWash : tone === 'sheet' ? colors.sheet : colors.card;
  return (
    <Pressable onPress={onPress} disabled={!onPress} style={[styles.chip, { backgroundColor: bg }]}>
      <Text style={[styles.chipLabel, selected && { color: colors.white }]}>{label}</Text>
    </Pressable>
  );
}

// Stand-in for screens whose milestone hasn't been built yet.
export function ComingSoon({ title, milestone, sheet }: { title: string; milestone: string; sheet?: boolean }) {
  return (
    <Screen sheet={sheet} style={{ justifyContent: 'center', gap: space.sm }}>
      <Text style={styles.heading}>{title}</Text>
      <Text style={styles.muted}>Lands in {milestone}</Text>
    </Screen>
  );
}

export const styles = StyleSheet.create({
  heading: { color: colors.ink, fontSize: 34, lineHeight: 38, fontWeight: weight.medium, letterSpacing: -0.8 },
  title: { color: colors.ink, fontSize: 22, fontWeight: weight.medium, letterSpacing: -0.3 },
  body: { color: colors.ink, fontSize: 17, fontWeight: weight.regular },
  muted: { color: colors.inkMuted, fontSize: 15, fontWeight: weight.regular },
  error: { color: colors.danger, fontSize: 15, fontWeight: weight.regular },
  amount: { color: colors.ink, fontWeight: weight.semibold, letterSpacing: -2 },
  button: { height: 60, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center', paddingHorizontal: space.lg },
  primary: { backgroundColor: colors.ink },
  secondary: { backgroundColor: colors.card },
  wash: { backgroundColor: colors.onAccentWash },
  buttonLabel: { fontSize: 17, fontWeight: weight.medium },
  iconButton: { width: 48, height: 48, borderRadius: 24, alignItems: 'center', justifyContent: 'center' },
  chip: { paddingHorizontal: space.md, height: 36, justifyContent: 'center', borderRadius: radius.pill },
  chipLabel: { color: colors.ink, fontSize: 14, fontWeight: weight.medium },
  // Borderless, like typing straight onto the sheet.
  input: { color: colors.ink, fontSize: 22, fontWeight: weight.regular, paddingVertical: space.md },
  sheetHeader: { alignItems: 'center', paddingTop: 10, gap: 14 },
  grabber: { width: 44, height: 5, borderRadius: 3, backgroundColor: '#C9CBC5' },
  sheetTitle: { color: colors.ink, fontSize: 17, fontWeight: weight.medium },
  rule: { height: StyleSheet.hairlineWidth, backgroundColor: colors.line },
  card: { backgroundColor: colors.card, borderRadius: radius.lg, padding: space.lg },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
});
