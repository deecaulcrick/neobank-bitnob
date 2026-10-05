import { router } from 'expo-router';
import { User } from 'lucide-react-native';
import { Pressable, Text } from 'react-native';

import { colors, weight } from '../theme';

// Opens Profile. Shows the tag's initial, or a person icon before a tag is picked.
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
      {tag ? (
        <Text style={{ color: colors.ink, fontSize: 18, fontWeight: weight.semibold }}>{tag[0].toUpperCase()}</Text>
      ) : (
        <User size={22} strokeWidth={2} color={colors.ink} />
      )}
    </Pressable>
  );
}
