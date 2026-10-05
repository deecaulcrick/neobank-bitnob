import { useFocusEffect } from 'expo-router';
import { useCallback, useState } from 'react';

import { api, type Balance } from './api';

// Refetches whenever the screen regains focus, e.g. after a send completes.
export function useBalances() {
  const [balances, setBalances] = useState<Balance[] | null>(null);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    try {
      // /v1/me creates the user's row and ledger accounts on first call.
      await api.me();
      setBalances((await api.balances()).balances);
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not load balances');
    }
  }, []);

  useFocusEffect(
    useCallback(() => {
      refresh();
    }, [refresh]),
  );

  return { balances, error, refresh };
}
