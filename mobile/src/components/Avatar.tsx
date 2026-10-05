import { router } from 'expo-router';
import { Pressable, Text } from 'react-native';

import { colors } from '../theme';

// Opens Profile. Shows the tag's initial until there is a photo.
export function Avatar({ tag }: { tag?: string | null }) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="Profile"
      onPress={() => router.push('/profile')}
      hitSlop={8}
      style={{
        width: 48,
        height: 48,
        borderRadius: 24,
        backgroundColor: colors.accent,
        alignItems: 'center',
        justifyContent: 'center',
        borderWidth: 2,
        borderColor: colors.ink,
      }}>
      <Text style={{ color: colors.ink, fontSize: 18, fontWeight: '800' }}>{(tag?.[0] ?? '·').toUpperCase()}</Text>
    </Pressable>
  );
}
