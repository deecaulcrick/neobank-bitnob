import * as LocalAuthentication from 'expo-local-authentication';
import * as SecureStore from 'expo-secure-store';
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { Modal, Platform, Pressable, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { FullWindowOverlay } from 'react-native-screens';

import { Keypad } from '../components/Keypad';
import { styles } from '../components/ui';
import { colors, space } from '../theme';
import { ApiError } from './api';

const PIN_KEY = 'txn_pin';
const BIOMETRIC_KEY = 'txn_biometric';

// Thrown when the user backs out of confirming.
export class Cancelled extends Error {}

type PinContextValue = {
  // Runs a money movement once the user has confirmed with their PIN, Face ID
  // or fingerprint. A wrong PIN asks again; cancelling throws Cancelled.
  withPin: <T>(action: (pin: string) => Promise<T>) => Promise<T>;
  biometricsAvailable: boolean;
  biometricsOn: boolean;
  setBiometricsOn: (on: boolean) => void;
};

const PinContext = createContext<PinContextValue>({
  withPin: () => Promise.reject(new Cancelled()),
  biometricsAvailable: false,
  biometricsOn: false,
  setBiometricsOn: () => {},
});

export const usePin = () => useContext(PinContext);

export function PinDots({ length }: { length: number }) {
  return (
    <View style={{ flexDirection: 'row', gap: 18, justifyContent: 'center' }}>
      {[0, 1, 2, 3].map((i) => (
        <View
          key={i}
          style={{
            width: 18,
            height: 18,
            borderRadius: 9,
            borderWidth: 2,
            borderColor: colors.ink,
            backgroundColor: i < length ? colors.ink : 'transparent',
          }}
        />
      ))}
    </View>
  );
}

export function PinProvider({ children }: { children: ReactNode }) {
  const [visible, setVisible] = useState(false);
  const [entry, setEntry] = useState('');
  const [message, setMessage] = useState('');
  const [biometricsAvailable, setAvailable] = useState(false);
  const [biometricsOn, setOn] = useState(false);
  const resolver = useRef<((pin: string | null) => void) | null>(null);
  const insets = useSafeAreaInsets();

  useEffect(() => {
    (async () => {
      try {
        const ok = (await LocalAuthentication.hasHardwareAsync()) && (await LocalAuthentication.isEnrolledAsync());
        setAvailable(ok);
        // On by default where the phone supports it.
        setOn(ok && (await SecureStore.getItemAsync(BIOMETRIC_KEY)) !== '0');
      } catch {
        setAvailable(false);
      }
    })();
  }, []);

  const setBiometricsOn = useCallback((on: boolean) => {
    setOn(on);
    SecureStore.setItemAsync(BIOMETRIC_KEY, on ? '1' : '0').catch(() => {});
    if (!on) SecureStore.deleteItemAsync(PIN_KEY).catch(() => {});
  }, []);

  // Asks for the PIN on screen. Resolves null if the user cancels.
  const prompt = useCallback((why: string) => {
    setEntry('');
    setMessage(why);
    setVisible(true);
    return new Promise<string | null>((resolve) => {
      resolver.current = resolve;
    });
  }, []);

  const finish = useCallback((pin: string | null) => {
    setVisible(false);
    resolver.current?.(pin);
    resolver.current = null;
  }, []);

  // Face ID or fingerprint unlocks the PIN kept in the phone's keychain.
  const fromBiometrics = useCallback(async () => {
    if (!biometricsAvailable || !biometricsOn) return null;
    try {
      const saved = await SecureStore.getItemAsync(PIN_KEY);
      if (!saved) return null;
      const result = await LocalAuthentication.authenticateAsync({ promptMessage: 'Confirm it\'s you', cancelLabel: 'Use PIN' });
      return result.success ? saved : null;
    } catch {
      return null;
    }
  }, [biometricsAvailable, biometricsOn]);

  const withPin = useCallback(
    async <T,>(action: (pin: string) => Promise<T>): Promise<T> => {
      let pin = await fromBiometrics();
      let why = '';
      for (;;) {
        if (!pin) pin = await prompt(why);
        if (!pin) throw new Cancelled();
        try {
          const result = await action(pin);
          // Only a PIN the server accepted is remembered for biometrics.
          if (biometricsAvailable && biometricsOn) SecureStore.setItemAsync(PIN_KEY, pin).catch(() => {});
          return result;
        } catch (e) {
          if (e instanceof ApiError && e.code === 'pin_wrong') {
            SecureStore.deleteItemAsync(PIN_KEY).catch(() => {});
            why = e.message;
            pin = null;
            continue;
          }
          if (e instanceof ApiError && e.code === 'pin_locked') SecureStore.deleteItemAsync(PIN_KEY).catch(() => {});
          throw e;
        }
      }
    },
    [fromBiometrics, prompt, biometricsAvailable, biometricsOn],
  );

  function onKey(key: string) {
    if (key === '.') return;
    const next = key === 'back' ? entry.slice(0, -1) : (entry + key).slice(0, 4);
    setEntry(next);
    if (next.length === 4) setTimeout(() => finish(next), 120);
  }

  const pad = (
    // Insets are applied by hand: a SafeAreaView reports none inside the overlay.
    <View style={{ flex: 1, backgroundColor: colors.sheet, paddingTop: insets.top, paddingBottom: insets.bottom }}>
      <View style={{ flex: 1, padding: space.md, justifyContent: 'space-between' }}>
        <View style={{ alignItems: 'flex-start' }}>
          <Pressable onPress={() => finish(null)} hitSlop={12}>
            <Text style={styles.muted}>Cancel</Text>
          </Pressable>
        </View>
        <View style={{ gap: space.lg, alignItems: 'center' }}>
          <Text style={styles.heading}>Enter your PIN</Text>
          <PinDots length={entry.length} />
          <Text style={[styles.error, { minHeight: 20, textAlign: 'center' }]}>{message}</Text>
        </View>
        <Keypad onKey={onKey} decimal={false} />
      </View>
    </View>
  );

  return (
    <PinContext.Provider value={{ withPin, biometricsAvailable, biometricsOn, setBiometricsOn }}>
      {children}
      {Platform.OS === 'ios' ? (
        // Our sheets are native modals, and iOS will not present a Modal from
        // underneath one, so the prompt is drawn over the whole window instead.
        visible && (
          <FullWindowOverlay>
            <View style={StyleSheet.absoluteFill}>{pad}</View>
          </FullWindowOverlay>
        )
      ) : (
        <Modal visible={visible} animationType="slide" onRequestClose={() => finish(null)}>
          {pad}
        </Modal>
      )}
    </PinContext.Provider>
  );
}
