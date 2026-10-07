import * as Clipboard from 'expo-clipboard';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, KeyboardAvoidingView, Platform, Text, TextInput, View } from 'react-native';

import { AmountField } from '../components/AmountField';
import { Button, Chip, Screen, styles } from '../components/ui';
import { api, newKey, type CryptoNetwork, type WithdrawalPreview } from '../lib/api';
import { formatMinor, type Asset } from '../lib/money';
import { Cancelled, usePin } from '../lib/pin';
import { refreshBalances } from '../lib/useBalances';
import { colors, space } from '../theme';

function Row({ label, value }: { label: string; value: string }) {
  return (
    <View style={styles.row}>
      <Text style={styles.muted}>{label}</Text>
      <Text style={styles.body}>{value}</Text>
    </View>
  );
}

// Send crypto to an external address. The fee and the total leaving the
// balance are shown before anything is confirmed.
export default function SendCrypto() {
  const params = useLocalSearchParams<{ asset: Asset; amount: string }>();
  const [asset, setAsset] = useState<Asset>(params.asset);
  const [amount, setAmount] = useState(params.amount);
  const { withPin } = usePin();
  const [networks, setNetworks] = useState<CryptoNetwork[] | null>(null);
  const [network, setNetwork] = useState('');
  const [address, setAddress] = useState('');
  const [preview, setPreview] = useState<WithdrawalPreview | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState('');
  const [sending, setSending] = useState(false);
  // One key per attempt, so a retry after a dropped response can't send twice.
  const idempotencyKey = useRef('');

  useEffect(() => {
    setNetworks(null);
    setNetwork('');
    api
      .cryptoNetworks(asset)
      .then((r) => {
        setNetworks(r.networks);
        setNetwork(r.networks[0]?.network ?? '');
      })
      .catch((e) => setError(e instanceof Error ? e.message : 'Could not load networks'));
  }, [asset]);

  // Price it once there is a plausible address, a moment after typing stops.
  useEffect(() => {
    setPreview(null);
    setError('');
    const to = address.trim();
    if (!network || to.length < 20) return;
    let cancelled = false;
    setChecking(true);
    const timer = setTimeout(() => {
      api
        .cryptoPreview({ asset, network, address: to, amount })
        .then((p) => !cancelled && setPreview(p))
        .catch((e) => !cancelled && setError(e instanceof Error ? e.message : 'Could not check that address'))
        .finally(() => !cancelled && setChecking(false));
    }, 500);
    return () => {
      cancelled = true;
      clearTimeout(timer);
      setChecking(false);
    };
  }, [asset, network, address, amount]);

  async function send() {
    setSending(true);
    setError('');
    try {
      const t = await withPin((pin) =>
        api.cryptoWithdraw(
          { asset, network, address: address.trim(), amount, idempotency_key: (idempotencyKey.current ||= newKey()) },
          pin,
        ),
      );
      refreshBalances();
      const what = `${formatMinor(t.asset, t.amount)} ${t.asset}`;
      router.replace({
        pathname: '/success',
        params:
          t.status === 'success'
            ? { message: `You sent ${what}` }
            : { message: `Sending ${what}. It's done when the network confirms; if it fails, the money goes back to your balance.`, pending: '1' },
      });
    } catch (e) {
      if (e instanceof Cancelled) return;
      setError(e instanceof Error ? e.message : 'Something went wrong');
      refreshBalances();
      // A refused withdrawal is final for that key; the next try is a new one.
      idempotencyKey.current = '';
    } finally {
      setSending(false);
    }
  }

  const label = networks?.find((n) => n.network === network)?.label ?? network;

  return (
    <Screen sheet="Send crypto">
      <KeyboardAvoidingView
        style={{ flex: 1, justifyContent: 'space-between' }}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        keyboardVerticalOffset={40}>
        <View style={{ gap: space.md }}>
          <View>
            <Text style={styles.muted}>You send</Text>
            <AmountField
              asset={asset}
              amount={amount}
              assets={['USDT', 'USDC', 'BTC']}
              onChange={(a, v) => {
                setAsset(a);
                setAmount(v);
              }}
            />
          </View>
          {!!networks && networks.length > 0 && (
            <View style={{ gap: space.sm }}>
              <Text style={styles.muted}>Network</Text>
              <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.xs }}>
                {networks.map((n) => (
                  <Chip key={n.network} label={n.label} selected={n.network === network} onPress={() => setNetwork(n.network)} />
                ))}
              </View>
            </View>
          )}
          {networks?.length === 0 && <Text style={styles.muted}>{asset} can't be sent out right now.</Text>}

          <View>
            <View style={styles.row}>
              <Text style={styles.muted}>To address</Text>
              <Chip label="Paste" onPress={async () => setAddress(await Clipboard.getStringAsync())} />
            </View>
            <TextInput
              style={[styles.input, { fontSize: 17 }]}
              value={address}
              onChangeText={setAddress}
              placeholder={`${label} address`}
              placeholderTextColor={colors.inkMuted}
              autoCapitalize="none"
              autoCorrect={false}
              multiline
              selectionColor={colors.ink}
            />
            <View style={styles.rule} />
          </View>

          {checking && <ActivityIndicator color={colors.ink} style={{ alignSelf: 'flex-start' }} />}
          {!!error && <Text style={styles.error}>{error}</Text>}
          {preview && (
            <View style={[styles.card, { gap: space.md }]}>
              <Row label="They get" value={`${formatMinor(preview.asset, preview.amount)} ${preview.asset}`} />
              <Row label="Fee" value={formatMinor(preview.asset, preview.fee)} />
              <View style={styles.rule} />
              <Row label="Total" value={`${formatMinor(preview.asset, preview.total)} ${preview.asset}`} />
              <Text style={styles.muted}>
                Crypto sends can't be reversed. Check the address and that it is on {label}.
              </Text>
            </View>
          )}
          {preview && !preview.enough_funds && (
            <Text style={styles.error}>
              You need {formatMinor(preview.asset, preview.total)} {preview.asset} including the fee.
            </Text>
          )}
        </View>

        {/* TODO: PIN or biometric before real money. */}
        <Button label="Send" onPress={send} loading={sending} disabled={!preview || !preview.enough_funds || checking} />
      </KeyboardAvoidingView>
    </Screen>
  );
}
