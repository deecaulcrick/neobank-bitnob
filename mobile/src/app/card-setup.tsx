import { router } from 'expo-router';
import { useState } from 'react';
import { KeyboardAvoidingView, Platform, Pressable, ScrollView, Text, TextInput, View } from 'react-native';

import { Button, Chip, Screen, styles } from '../components/ui';
import { api } from '../lib/api';
import { colors, space } from '../theme';

// Bitnob takes these as lower-case codes. The lists are our own shortlist;
// its full set of accepted values isn't documented.
const OCCUPATIONS = [
  ['software_engineer', 'Tech'],
  ['trader', 'Trading'],
  ['creative', 'Creative'],
  ['civil_servant', 'Public sector'],
  ['student', 'Student'],
  ['other', 'Other'],
];
const EMPLOYMENT = [
  ['employed', 'Employed'],
  ['self_employed', 'Self-employed'],
  ['student', 'Student'],
  ['unemployed', 'Not working'],
];
const PURPOSES = [
  ['online_shopping', 'Shopping'],
  ['subscriptions', 'Subscriptions'],
  ['travel', 'Travel'],
  ['business', 'Business'],
];
// Whole dollars a year / a month, sent as the top of the chosen band.
const INCOME = [
  ['5000', 'Under $5k'],
  ['20000', '$5k–20k'],
  ['50000', '$20k–50k'],
  ['100000', 'Over $50k'],
];
const SPEND = [
  ['200', 'Under $200'],
  ['1000', '$200–1k'],
  ['5000', '$1k–5k'],
  ['10000', 'Over $5k'],
];

function Choice({ label, options, value, onChange }: { label: string; options: string[][]; value: string; onChange: (v: string) => void }) {
  return (
    <View style={{ gap: space.sm }}>
      <Text style={styles.muted}>{label}</Text>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.xs }}>
        {options.map(([v, text]) => (
          <Chip key={v} label={text} selected={v === value} onPress={() => onChange(v)} />
        ))}
      </View>
    </View>
  );
}

// Card verification. The issuer asks for more than account opening did:
// home address, work, and what the card is for.
export default function CardSetup() {
  const [form, setForm] = useState({
    line1: '',
    city: '',
    state: '',
    postal_code: '',
    bvn: '',
    occupation: '',
    employment_status: '',
    account_purpose: '',
    annual_salary: '',
    expected_monthly_volume: '',
  });
  const [accepted, setAccepted] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const set = (k: keyof typeof form) => (v: string) => setForm((f) => ({ ...f, [k]: v }));

  const ready = accepted && /^\d{11}$/.test(form.bvn) && Object.values(form).every((v) => v.trim().length > 0);

  async function submit() {
    setLoading(true);
    setError('');
    try {
      await api.cardKyc({ ...form, accept_terms: true });
      router.back();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong');
    } finally {
      setLoading(false);
    }
  }

  const field = (k: keyof typeof form, placeholder: string, numeric = false) => (
    <View>
      <TextInput
        style={[styles.input, { fontSize: 18, paddingVertical: 12 }]}
        value={form[k]}
        onChangeText={(t) => set(k)(numeric ? t.replace(/\D/g, '') : t)}
        placeholder={placeholder}
        placeholderTextColor={colors.inkMuted}
        keyboardType={numeric ? 'number-pad' : 'default'}
        maxLength={k === 'bvn' ? 11 : 80}
        autoCorrect={false}
        selectionColor={colors.ink}
      />
      <View style={styles.rule} />
    </View>
  );

  return (
    <Screen sheet="Get your card">
      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined} keyboardVerticalOffset={40}>
        <ScrollView keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false} contentContainerStyle={{ gap: space.md, paddingBottom: space.lg }}>
          <Text style={styles.muted}>The card issuer needs a few more details than opening your account did.</Text>
          <View>
            <Text style={styles.muted}>Home address</Text>
            {field('line1', 'Street address')}
            {field('city', 'City')}
            {field('state', 'State')}
            {field('postal_code', 'Postal code')}
          </View>
          <Choice label="What do you do?" options={OCCUPATIONS} value={form.occupation} onChange={set('occupation')} />
          <Choice label="Work status" options={EMPLOYMENT} value={form.employment_status} onChange={set('employment_status')} />
          <Choice label="What's the card for?" options={PURPOSES} value={form.account_purpose} onChange={set('account_purpose')} />
          <Choice label="Yearly income" options={INCOME} value={form.annual_salary} onChange={set('annual_salary')} />
          <Choice label="Expected monthly spend" options={SPEND} value={form.expected_monthly_volume} onChange={set('expected_monthly_volume')} />
          <View>
            <Text style={styles.muted}>Your BVN, to confirm it's you. We don't keep it.</Text>
            {field('bvn', '11 digits', true)}
          </View>
          <Pressable onPress={() => setAccepted((a) => !a)} style={{ flexDirection: 'row', gap: space.sm, alignItems: 'center' }}>
            <View
              style={{
                width: 24,
                height: 24,
                borderRadius: 6,
                borderWidth: 2,
                borderColor: colors.ink,
                backgroundColor: accepted ? colors.ink : 'transparent',
              }}
            />
            <Text style={[styles.body, { flexShrink: 1 }]}>I accept the card terms of service</Text>
          </Pressable>
          {!!error && <Text style={styles.error}>{error}</Text>}
        </ScrollView>
        <Button label="Continue" onPress={submit} loading={loading} disabled={!ready} />
      </KeyboardAvoidingView>
    </Screen>
  );
}
