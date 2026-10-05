import * as Clipboard from 'expo-clipboard';
import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { Share, Text, View } from 'react-native';

import { Button, Screen, styles } from '../components/ui';
import { api, type VirtualAccount } from '../lib/api';
import { colors, space, weight } from '../theme';

// Fund NGN by bank transfer to the user's own account number. The balance
// updates when Bitnob reports the deposit.
export default function AddMoney() {
  const [account, setAccount] = useState<VirtualAccount | null>(null);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);
  const [simulating, setSimulating] = useState(false);
  const [note, setNote] = useState('');

  useEffect(() => {
    api
      .virtualAccount()
      .then(setAccount)
      .catch((e) => setError(e instanceof Error ? e.message : 'Could not load your account'));
  }, []);

  async function copy() {
    await Clipboard.setStringAsync(account!.account_number);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  async function simulate() {
    setSimulating(true);
    setNote('');
    try {
      await api.devSimulateDeposit();
      setNote('₦1,000 test deposit added.');
    } catch (e) {
      setNote(e instanceof Error ? e.message : 'Simulated deposit failed');
    } finally {
      setSimulating(false);
    }
  }

  return (
    <Screen sheet="Add money" style={{ justifyContent: 'space-between' }}>
      <View style={{ gap: space.md }}>
        <Text style={styles.muted}>Transfer from any Nigerian bank to your account. It arrives in your naira balance.</Text>
        {!!error && <Text style={styles.error}>{error}</Text>}
        {account && (
          <View style={[styles.card, { gap: space.md }]}>
            <View>
              <Text style={styles.muted}>Account number</Text>
              <Text selectable style={{ color: colors.ink, fontSize: 34, fontWeight: weight.semibold, letterSpacing: 1 }}>
                {account.account_number}
              </Text>
            </View>
            <View style={styles.rule} />
            <View style={styles.row}>
              <Text style={styles.muted}>Bank</Text>
              <Text style={styles.body}>{account.bank_name}</Text>
            </View>
            <View style={styles.row}>
              <Text style={styles.muted}>Name</Text>
              <Text style={styles.body}>{account.account_name}</Text>
            </View>
          </View>
        )}
        {!!note && <Text style={styles.muted}>{note}</Text>}
      </View>

      {account && (
        <View style={{ gap: space.sm }}>
          {__DEV__ && (
            <Button label="Simulate ₦1,000 deposit" variant="secondary" onPress={simulate} loading={simulating} />
          )}
          <Button label="Receive crypto instead" variant="secondary" onPress={() => router.push('/receive')} />
          <Button
            label="Share details"
            variant="secondary"
            onPress={() =>
              Share.share({
                message: `${account.account_name}\n${account.account_number}\n${account.bank_name}`,
              })
            }
          />
          <Button label={copied ? 'Copied' : 'Copy account number'} onPress={copy} />
        </View>
      )}
    </Screen>
  );
}
