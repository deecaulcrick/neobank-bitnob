import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { ActivityIndicator, View } from 'react-native';

import { SessionProvider, useSession } from '../lib/session';
import { colors } from '../theme';

function RootStack() {
  const { session, loading } = useSession();

  if (loading) {
    return (
      <View style={{ flex: 1, backgroundColor: colors.bg, justifyContent: 'center' }}>
        <ActivityIndicator color={colors.accent} />
      </View>
    );
  }

  const modal = { presentation: 'modal', headerShown: true } as const;
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        headerStyle: { backgroundColor: colors.bg },
        headerTintColor: colors.text,
        contentStyle: { backgroundColor: colors.bg },
      }}>
      <Stack.Protected guard={!session}>
        <Stack.Screen name="(auth)" />
      </Stack.Protected>
      <Stack.Protected guard={!!session}>
        <Stack.Screen name="(tabs)" />
        <Stack.Screen name="add-money" options={{ ...modal, title: 'Add money' }} />
        <Stack.Screen name="keypad" options={{ ...modal, title: '' }} />
        <Stack.Screen name="send" options={{ ...modal, title: 'Send' }} />
        <Stack.Screen name="swap-review" options={{ ...modal, title: 'Review swap' }} />
        <Stack.Screen name="payout-setup" options={{ ...modal, title: 'Send abroad' }} />
        <Stack.Screen name="payout-review" options={{ ...modal, title: 'Review payout' }} />
        <Stack.Screen name="receive" options={{ ...modal, title: 'Receive crypto' }} />
        <Stack.Screen name="transaction/[id]" options={{ headerShown: true, title: 'Transaction' }} />
      </Stack.Protected>
    </Stack>
  );
}

export default function RootLayout() {
  return (
    <SessionProvider>
      <StatusBar style="light" />
      <RootStack />
    </SessionProvider>
  );
}
