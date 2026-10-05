import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';

import { api, type Me } from './api';
import { useSession } from './session';
import { clearBalances } from './useBalances';

type MeState = {
  me: Me | null;
  // Set when the profile could not be loaded (API down, no network).
  error: string;
  refresh: () => Promise<void>;
  // Switches the currency Home totals in; applied at once, saved in the background.
  setDisplayCurrency: (currency: 'NGN' | 'USD') => void;
};

const MeContext = createContext<MeState>({
  me: null,
  error: '',
  refresh: async () => {},
  setDisplayCurrency: () => {},
});

// The signed-in user's profile, loaded once per session and shared, so the
// root layout can decide between onboarding and the app.
export function MeProvider({ children }: { children: ReactNode }) {
  const { session } = useSession();
  const userId = session?.user.id;
  const [me, setMe] = useState<Me | null>(null);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    try {
      setMe(await api.me());
      setError('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not load your profile');
    }
  }, []);

  useEffect(() => {
    setMe(null);
    setError('');
    clearBalances();
    if (userId) refresh();
  }, [userId, refresh]);

  const setDisplayCurrency = useCallback((currency: 'NGN' | 'USD') => {
    setMe((cur) => (cur ? { ...cur, display_currency: currency } : cur));
    api.setDisplayCurrency(currency).catch(() => {});
  }, []);

  return <MeContext.Provider value={{ me, error, refresh, setDisplayCurrency }}>{children}</MeContext.Provider>;
}

export const useMeState = () => useContext(MeContext);
