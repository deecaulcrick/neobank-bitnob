import * as Clipboard from 'expo-clipboard';
import { useEffect, useState } from 'react';
import { ActivityIndicator, Share, Text, View } from 'react-native';
import QRCode from 'react-native-qrcode-svg';

import { Button, Chip, Screen, styles } from '../components/ui';
import { api, type CryptoNetwork } from '../lib/api';
import type { Asset } from '../lib/money';
import { colors, space } from '../theme';

const CRYPTO: Asset[] = ['USDT', 'USDC', 'BTC'];

// Show an address to receive USDT, USDC or BTC. The network list is read
// live; the cheapest network is offered first.
export default function Receive() {
  const [asset, setAsset] = useState<Asset>('USDT');
  const [networks, setNetworks] = useState<CryptoNetwork[] | null>(null);
  const [network, setNetwork] = useState('');
  const [address, setAddress] = useState('');
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setNetworks(null);
    setNetwork('');
    setAddress('');
    setError('');
    api
      .cryptoNetworks(asset)
      .then((r) => {
        if (cancelled) return;
        setNetworks(r.networks);
        setNetwork(r.networks[0]?.network ?? '');
      })
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : 'Could not load networks'));
    return () => {
      cancelled = true;
    };
  }, [asset]);

  useEffect(() => {
    if (!network) return;
    let cancelled = false;
    setAddress('');
    setError('');
    api
      .cryptoAddress(asset, network)
      .then((r) => !cancelled && setAddress(r.address))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : 'Could not get an address'));
    return () => {
      cancelled = true;
    };
  }, [asset, network]);

  const label = networks?.find((n) => n.network === network)?.label ?? network;

  async function copy() {
    await Clipboard.setStringAsync(address);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  return (
    <Screen sheet="Receive crypto" style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md }}>
        <View style={{ flexDirection: 'row', gap: space.xs }}>
          {CRYPTO.map((a) => (
            <Chip key={a} label={a} selected={a === asset} onPress={() => setAsset(a)} />
          ))}
        </View>
        {!!networks && networks.length > 1 && (
          <View style={{ gap: space.sm }}>
            <Text style={styles.muted}>Network</Text>
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.xs }}>
              {networks.map((n) => (
                <Chip key={n.network} label={n.label} selected={n.network === network} onPress={() => setNetwork(n.network)} />
              ))}
            </View>
          </View>
        )}

        {!!error && <Text style={styles.error}>{error}</Text>}
        {networks?.length === 0 && !error && <Text style={styles.muted}>{asset} can't be received right now.</Text>}

        {!!network && !error && (
          <View style={[styles.card, { alignItems: 'center', gap: space.md, minHeight: 270, justifyContent: 'center' }]}>
            {address ? (
              <>
                <QRCode value={address} size={150} color={colors.ink} backgroundColor={colors.card} />
                <Text selectable style={[styles.body, { textAlign: 'center' }]}>
                  {address}
                </Text>
              </>
            ) : (
              <ActivityIndicator color={colors.ink} />
            )}
          </View>
        )}
        {!!address && (
          <Text style={styles.muted}>
            Send only {asset} on {label} to this address. Anything else, or the wrong network, may be lost for good.
          </Text>
        )}
      </View>

      {!!address && (
        <View style={{ gap: space.sm, marginTop: space.md }}>
          <Button label="Share address" variant="secondary" onPress={() => Share.share({ message: address })} />
          <Button label={copied ? 'Copied' : 'Copy address'} onPress={copy} />
        </View>
      )}
    </Screen>
  );
}
