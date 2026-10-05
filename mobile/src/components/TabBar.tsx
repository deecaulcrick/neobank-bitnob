import { ArrowLeftRight, Clock, House } from 'lucide-react-native';
import type { ComponentProps } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import type { Tabs } from 'expo-router';

import { colors, radius } from '../theme';

type TabBarProps = Parameters<NonNullable<ComponentProps<typeof Tabs>['tabBar']>>[0];

// Floating pill tab bar. It tints itself for the accent-coloured Pay tab.
export function TabBar({ state, descriptors, navigation }: TabBarProps) {
  const insets = useSafeAreaInsets();
  const onAccent = state.routes[state.index].name === 'pay';

  return (
    <View pointerEvents="box-none" style={[styles.wrap, { bottom: Math.max(insets.bottom, 12) }]}>
      <View style={[styles.bar, onAccent ? styles.barOnAccent : styles.barOnSheet]}>
        {state.routes.map((route, index) => {
          const focused = state.index === index;
          const label = descriptors[route.key].options.title ?? route.name;
          return (
            <Pressable
              key={route.key}
              accessibilityRole="tab"
              accessibilityLabel={label}
              accessibilityState={{ selected: focused }}
              onPress={() => {
                const event = navigation.emit({ type: 'tabPress', target: route.key, canPreventDefault: true });
                if (!focused && !event.defaultPrevented) navigation.navigate(route.name);
              }}
              style={[styles.item, focused && (onAccent ? styles.itemFocusedOnAccent : styles.itemFocused)]}>
              <View style={!focused && { opacity: 0.5 }}>
                {route.name === 'index' ? (
                  <House size={26} strokeWidth={2} color={colors.ink} />
                ) : route.name === 'activity' ? (
                  <Clock size={26} strokeWidth={2} color={colors.ink} />
                ) : (
                  <ArrowLeftRight size={26} strokeWidth={2} color={colors.ink} />
                )}
              </View>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { position: 'absolute', left: 0, right: 0, alignItems: 'center' },
  bar: { flexDirection: 'row', padding: 6, borderRadius: radius.pill, gap: 2 },
  barOnSheet: {
    backgroundColor: colors.card,
    shadowColor: '#000',
    shadowOpacity: 0.12,
    shadowRadius: 18,
    shadowOffset: { width: 0, height: 6 },
    elevation: 8,
  },
  barOnAccent: { backgroundColor: 'transparent' },
  item: { height: 48, minWidth: 92, paddingHorizontal: 18, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center' },
  itemFocused: { backgroundColor: colors.sheet },
  itemFocusedOnAccent: { backgroundColor: colors.onAccentWash },
});
