import { Stack } from 'expo-router';
import * as SplashScreen from 'expo-splash-screen';
import { useEffect } from 'react';

// Hold the splash until the session is known, so the sign-in screen never flashes.
SplashScreen.preventAutoHideAsync();

import { Text, View } from 'react-native';

import { Button, Screen, styles } from '../components/ui';
import { MeProvider, useMeState } from '../lib/me';
import { PinProvider } from '../lib/pin';
import { SessionProvider, useSession } from '../lib/session';
import { supabase } from '../lib/supabase';
import { colors, space, weight } from '../theme';

function RootStack() {
  const { session, loading } = useSession();
  const { me, error, notInvited, refresh } = useMeState();
  // Signed-in users also wait for their profile, which decides where they land.
  const ready = !loading && (!session || !!me || !!error);

  useEffect(() => {
    if (ready) SplashScreen.hideAsync();
  }, [ready]);

  if (!ready) return null;

  if (session && !me && notInvited) {
    return (
      <Screen style={{ justifyContent: 'center', gap: space.md }}>
        <Text style={styles.heading}>You're on the waitlist</Text>
        <Text style={styles.muted}>
          We're letting people in a few at a time. We'll text you when your number is in.
        </Text>
        <View style={{ gap: space.sm, marginTop: space.md }}>
          <Button label="Check again" onPress={refresh} />
          <Button label="Sign out" variant="secondary" onPress={() => supabase.auth.signOut()} />
        </View>
      </Screen>
    );
  }

  if (session && !me) {
    return (
      <Screen style={{ justifyContent: 'center', gap: space.md }}>
        <Text style={styles.heading}>We can't reach the server</Text>
        <Text style={styles.muted}>{error}</Text>
        <View style={{ gap: space.sm, marginTop: space.md }}>
          <Button label="Try again" onPress={refresh} />
          <Button label="Sign out" variant="secondary" onPress={() => supabase.auth.signOut()} />
        </View>
      </Screen>
    );
  }

  // Tier-1 KYC, a tag and a PIN come before anything else.
  const onboarded = !!me && me.kyc_tier >= 1 && !!me.tag && me.has_pin;

  // Sheets draw their own grabber and title (see Screen's `sheet` prop).
  const sheet = { presentation: 'modal' } as const;
  return (
    <Stack
      screenOptions={{
        headerShown: false,
        headerStyle: { backgroundColor: colors.sheet },
        headerShadowVisible: false,
        headerTintColor: colors.ink,
        headerTitleStyle: { fontWeight: weight.medium },
        contentStyle: { backgroundColor: colors.sheet },
      }}>
      <Stack.Protected guard={!session}>
        <Stack.Screen name="(auth)" />
      </Stack.Protected>
      <Stack.Protected guard={!!session && !onboarded}>
        <Stack.Screen name="onboarding" />
      </Stack.Protected>
      <Stack.Protected guard={!!session && onboarded}>
        <Stack.Screen name="(tabs)" />
        <Stack.Screen name="add-money" options={sheet} />
        <Stack.Screen name="send" options={sheet} />
        <Stack.Screen name="success" options={{ presentation: 'fullScreenModal', gestureEnabled: false }} />
        <Stack.Screen name="profile" options={sheet} />
        <Stack.Screen name="swap" options={sheet} />
        <Stack.Screen name="card-setup" options={sheet} />
        <Stack.Screen name="card-move" options={sheet} />
        <Stack.Screen name="card-details" options={sheet} />
        <Stack.Screen name="payout-setup" options={sheet} />
        <Stack.Screen name="payout-review" options={sheet} />
        <Stack.Screen name="receive" options={sheet} />
        <Stack.Screen name="send-crypto" options={sheet} />
        <Stack.Screen name="transaction/[id]" />
      </Stack.Protected>
    </Stack>
  );
}

export default function RootLayout() {
  return (
    <SessionProvider>
      <MeProvider>
        <PinProvider>
          <RootStack />
        </PinProvider>
      </MeProvider>
    </SessionProvider>
  );
}
