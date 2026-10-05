import AsyncStorage from '@react-native-async-storage/async-storage';
import { useSyncExternalStore } from 'react';

export type SkyMode = 'day' | 'night';

const KEY = 'prefs.sky';
let sky: SkyMode = 'day';
const listeners = new Set<() => void>();

function emit() {
  listeners.forEach((l) => l());
}

AsyncStorage.getItem(KEY)
  .then((stored) => {
    if (stored === 'day' || stored === 'night') {
      sky = stored;
      emit();
    }
  })
  .catch(() => {});

export function setSkyMode(mode: SkyMode) {
  sky = mode;
  emit();
  AsyncStorage.setItem(KEY, mode).catch(() => {});
}

// The sky behind Home. Remembered on this device.
export function useSkyMode(): SkyMode {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => sky,
  );
}
