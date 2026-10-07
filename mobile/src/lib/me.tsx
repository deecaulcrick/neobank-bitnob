import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';

import { api, ApiError, type Me } from './api';
import { registerForPush } from './push';
import { useSession } from './session';
import { clearBalances } from './useBalances';

type MeState = {
  me: Me | null;
  // Set when the profile could not be loaded (API down, no network).
  error: string;
  // True when the closed beta is on and this number isn't invited.
  notInvited: boolean;
  refresh: () => Promise<void>;
  // Switches the currency Home totals in; applied at once, saved in the background.
  setDisplayCurrency: (currency: 'NGN' | 'USD') => void;
};

const MeContext = createContext<MeState>({
  me: null,
  error: '',
  notInvited: false,
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
  const [notInvited, setNotInvited] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const next = await api.me();
      setMe(next);
      setError('');
      setNotInvited(false);
      if (next.kyc_tier >= 1) registerForPush();
    } catch (e) {
      setNotInvited(e instanceof ApiError && e.code === 'not_invited');
      setError(e instanceof Error ? e.message : 'Could not load your profile');
    }
  }, []);

  useEffect(() => {
    setMe(null);
    setError('');
    setNotInvited(false);
    clearBalances();
    if (userId) refresh();
  }, [userId, refresh]);

  const setDisplayCurrency = useCallback((currency: 'NGN' | 'USD') => {
    setMe((cur) => (cur ? { ...cur, display_currency: currency } : cur));
    api.setDisplayCurrency(currency).catch(() => {});
  }, []);

  return <MeContext.Provider value={{ me, error, notInvited, refresh, setDisplayCurrency }}>{children}</MeContext.Provider>;
}

export const useMeState = () => useContext(MeContext);
