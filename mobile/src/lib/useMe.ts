import { useEffect, useState } from 'react';

import { api, type Me } from './api';

export function useMe() {
  const [me, setMe] = useState<Me | null>(null);
  useEffect(() => {
    api.me().then(setMe).catch(() => {});
  }, []);
  return me;
}
