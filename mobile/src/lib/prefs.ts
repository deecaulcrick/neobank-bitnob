import AsyncStorage from '@react-native-async-storage/async-storage';
import { useSyncExternalStore } from 'react';

export type SkyMode = 'day' | 'night';

const KEY = 'prefs.sky';
const HIDE_KEY = 'prefs.hideBalances';
let sky: SkyMode = 'day';
let hideBalances = false;
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

AsyncStorage.getItem(HIDE_KEY)
  .then((stored) => {
    if (stored === '1') {
      hideBalances = true;
      emit();
    }
  })
  .catch(() => {});

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function setHideBalances(hide: boolean) {
  hideBalances = hide;
  emit();
  AsyncStorage.setItem(HIDE_KEY, hide ? '1' : '0').catch(() => {});
}

// Whether amounts are masked, on Home and in the tab bar alike.
export function useHideBalances(): boolean {
  return useSyncExternalStore(subscribe, () => hideBalances);
}

export function setSkyMode(mode: SkyMode) {
  sky = mode;
  emit();
  AsyncStorage.setItem(KEY, mode).catch(() => {});
}

// The sky behind Home. Remembered on this device.
export function useSkyMode(): SkyMode {
  return useSyncExternalStore(subscribe, () => sky);
}
