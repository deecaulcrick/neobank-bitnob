import { Lock } from 'lucide-react-native';
import { Text, View, type ViewStyle } from 'react-native';

import { CARD_THEMES, useCardTheme } from '../lib/prefs';
import { radius, weight } from '../theme';

type Props = {
  tag?: string | null;
  last4?: string;
  locked?: boolean;
  // A slight tilt, as if held in the hand.
  tilt?: boolean;
  // Smaller text for the copy that peeks out on Home.
  compact?: boolean;
  style?: ViewStyle;
};

// The user's card, drawn in the colour they picked. Shows the tag rather
// than a number: the number only ever appears on the details screen.
export function CardVisual({ tag, last4, locked, tilt, compact, style }: Props) {
  const theme = CARD_THEMES[useCardTheme()];
  return (
    <View
      style={[
        {
          aspectRatio: 1.586,
          borderRadius: radius.lg,
          backgroundColor: theme.bg,
          padding: compact ? 16 : 22,
          justifyContent: 'space-between',
          shadowColor: '#000',
          shadowOpacity: 0.25,
          shadowRadius: 20,
          shadowOffset: { width: 0, height: 10 },
          elevation: 10,
        },
        tilt && { transform: [{ rotate: '-4deg' }] },
        style,
      ]}>
      <View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' }}>
        {/* Chip */}
        <View
          style={{
            width: compact ? 34 : 44,
            height: compact ? 26 : 34,
            borderRadius: 7,
            backgroundColor: '#D9D4C7',
            borderWidth: 1,
            borderColor: 'rgba(0,0,0,0.15)',
          }}
        />
        <Text style={{ color: theme.fg, fontSize: compact ? 16 : 19, fontWeight: weight.medium, opacity: 0.9 }}>
          {tag ? `@${tag}` : ''}
        </Text>
      </View>
      <View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-end' }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          {locked && <Lock size={16} strokeWidth={2.25} color={theme.fg} />}
          <Text style={{ color: theme.fg, fontSize: 15, letterSpacing: 2, opacity: 0.85 }}>
            {locked ? 'Locked' : last4 ? `•••• ${last4}` : ''}
          </Text>
        </View>
        <Text style={{ color: theme.fg, fontSize: 18, fontWeight: weight.semibold, fontStyle: 'italic', letterSpacing: 1 }}>
          VISA
        </Text>
      </View>
    </View>
  );
}
