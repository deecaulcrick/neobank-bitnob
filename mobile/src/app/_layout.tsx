import { Stack } from 'expo-router';
import { ActivityIndicator, View } from 'react-native';

import { SessionProvider, useSession } from '../lib/session';
import { colors } from '../theme';

function RootStack() {
  const { session, loading } = useSession();

  if (loading) {
    return (
      <View style={{ flex: 1, backgroundColor: colors.sheet, justifyContent: 'center' }}>
        <ActivityIndicator color={colors.ink} />
      </View>
    );
  }

  const sheet = { presentation: 'modal', headerShown: true } as const;
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        headerStyle: { backgroundColor: colors.sheet },
        headerShadowVisible: false,
        headerTintColor: colors.ink,
        headerTitleStyle: { fontWeight: '700' },
        contentStyle: { backgroundColor: colors.sheet },
      }}>
      <Stack.Protected guard={!session}>
        <Stack.Screen name="(auth)" />
      </Stack.Protected>
      <Stack.Protected guard={!!session}>
        <Stack.Screen name="(tabs)" />
        <Stack.Screen name="add-money" options={{ ...sheet, title: 'Add money' }} />
        <Stack.Screen name="send" options={{ ...sheet, title: '' }} />
        <Stack.Screen name="success" options={{ presentation: 'fullScreenModal', gestureEnabled: false }} />
        <Stack.Screen name="profile" options={{ ...sheet, title: '' }} />
        <Stack.Screen name="swap-review" options={{ ...sheet, title: 'Review swap' }} />
        <Stack.Screen name="payout-setup" options={{ ...sheet, title: 'Send abroad' }} />
        <Stack.Screen name="payout-review" options={{ ...sheet, title: 'Review payout' }} />
        <Stack.Screen name="receive" options={{ ...sheet, title: 'Crypto' }} />
        <Stack.Screen name="transaction/[id]" options={{ headerShown: true, title: 'Transaction' }} />
      </Stack.Protected>
    </Stack>
  );
}

export default function RootLayout() {
  return (
    <SessionProvider>
      <RootStack />
    </SessionProvider>
  );
}
