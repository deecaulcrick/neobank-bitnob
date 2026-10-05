import { useFocusEffect } from 'expo-router';
import { useCallback, useSyncExternalStore } from 'react';

import { api, type Balance } from './api';

type State = { balances: Balance[] | null; error: string };

let state: State = { balances: null, error: '' };
const listeners = new Set<() => void>();

function set(next: State) {
  state = next;
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export async function refreshBalances() {
  try {
    set({ balances: (await api.balances()).balances, error: '' });
  } catch (e) {
    set({ ...state, error: e instanceof Error ? e.message : 'Could not load balances' });
  }
}

// Called on sign-out so the next user never sees the last one's numbers.
export function clearBalances() {
  set({ balances: null, error: '' });
}

// Shared balances; reading them does not fetch.
export function useBalancesState() {
  return useSyncExternalStore(subscribe, () => state);
}

// Balances that refetch whenever the screen regains focus, e.g. after a send.
export function useBalances() {
  useFocusEffect(
    useCallback(() => {
      refreshBalances();
    }, []),
  );
  return { ...useBalancesState(), refresh: refreshBalances };
}
