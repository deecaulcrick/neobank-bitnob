import { router, useLocalSearchParams } from 'expo-router';
import { ArrowLeft } from 'lucide-react-native';
import { useEffect, useMemo, useState } from 'react';
import { ActivityIndicator, KeyboardAvoidingView, Platform, Pressable, ScrollView, Text, TextInput, View } from 'react-native';

import { Button, Chip, IconButton, Screen, styles } from '../components/ui';
import {
  api,
  type PayoutCountry,
  type PayoutCountryDetails,
  type PayoutField,
  type SavedBeneficiary,
} from '../lib/api';
import { formatInput, type Asset } from '../lib/money';
import { colors, space } from '../theme';

const REASONS = [
  { value: 'family_support', label: 'Family' },
  { value: 'education', label: 'Education' },
  { value: 'salary', label: 'Salary' },
  { value: 'vendor_payment', label: 'Supplier' },
];

const RAIL_LABEL: Record<string, string> = {
  bank: 'Bank',
  mobile_money: 'Mobile money',
  paybill: 'Paybill',
  paytill: 'Till',
};

// The provider a lookup needs: the bank for bank transfers, the network for mobile money.
const providerOf = (values: Record<string, string>) => values.bank_code ?? values.network ?? '';

function Row({ title, detail, onPress }: { title: string; detail?: string; onPress: () => void }) {
  return (
    <Pressable onPress={onPress} style={({ pressed }) => [styles.row, { paddingVertical: 14 }, pressed && { opacity: 0.5 }]}>
      <Text style={styles.body}>{title}</Text>
      {!!detail && <Text style={styles.muted}>{detail}</Text>}
    </Pressable>
  );
}

// Where the money goes: country, then the recipient's details. The country
// list and each rail's form come live from Bitnob, never a hardcoded table.
export default function PayoutSetup() {
  const { asset, amount } = useLocalSearchParams<{ asset: Asset; amount: string }>();
  const [countries, setCountries] = useState<PayoutCountry[] | null>(null);
  const [recent, setRecent] = useState<SavedBeneficiary[]>([]);
  const [search, setSearch] = useState('');
  const [country, setCountry] = useState<PayoutCountry | null>(null);
  const [details, setDetails] = useState<PayoutCountryDetails | null>(null);
  const [rail, setRail] = useState('');
  const [values, setValues] = useState<Record<string, string>>({});
  const [bankSearch, setBankSearch] = useState('');
  const [resolvedName, setResolvedName] = useState('');
  const [lookingUp, setLookingUp] = useState(false);
  const [reason, setReason] = useState('family_support');
  const [error, setError] = useState('');

  useEffect(() => {
    api
      .payoutCountries()
      .then((r) => setCountries(r.countries))
      .catch((e) => setError(e instanceof Error ? e.message : 'Could not load destinations'));
    api
      .beneficiaries()
      .then((r) => setRecent(r.beneficiaries))
      .catch(() => {});
  }, []);

  const currency = country?.corridors.find((c) => c.rails.includes(rail))?.currency ?? '';
  const form = details?.rails[rail];
  const set = (key: string, value: string) => setValues((cur) => ({ ...cur, [key]: value }));

  async function choose(c: PayoutCountry) {
    setCountry(c);
    setDetails(null);
    setValues({});
    setResolvedName('');
    setError('');
    try {
      const d = await api.payoutCountry(c.code);
      const first = Object.keys(d.rails)[0] ?? '';
      setDetails(d);
      pickRail(d, first);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not load that destination');
    }
  }

  // Single-option selects (one network, say) are filled in rather than asked.
  function pickRail(d: PayoutCountryDetails, next: string) {
    setRail(next);
    setResolvedName('');
    setBankSearch('');
    const preset: Record<string, string> = {};
    for (const f of d.rails[next]?.fields ?? []) {
      if (f.component === 'select' && f.options.length === 1) preset[f.key] = f.options[0].value;
    }
    setValues(preset);
  }

  const fieldOk = (f: PayoutField) => {
    const v = (values[f.key] ?? '').trim();
    if (!v) return !f.required;
    return !f.pattern || new RegExp(f.pattern).test(v);
  };
  const fieldsOk = !!form && form.fields.every(fieldOk);

  // Some rails ask for the recipient's name; where they don't, we resolve it
  // from the account (Nigerian banks, Ghanaian mobile money) or ask.
  const asksName = !!form?.fields.some((f) => f.key === 'account_name');
  const account = (values.account_number ?? '').trim();
  const provider = providerOf(values);
  const canLookUp = !asksName && (country?.code === 'NG' || country?.code === 'GH') && !!provider;

  useEffect(() => {
    setResolvedName('');
    if (!canLookUp || !fieldsOk || !country) return;
    let cancelled = false;
    setLookingUp(true);
    const timer = setTimeout(() => {
      api
        .payoutLookup(country.code, rail, provider, account)
        .then((r) => !cancelled && setResolvedName(r.account_name))
        .catch(() => {})
        .finally(() => !cancelled && setLookingUp(false));
    }, 500);
    return () => {
      cancelled = true;
      clearTimeout(timer);
      setLookingUp(false);
    };
  }, [canLookUp, fieldsOk, country, rail, provider, account]);

  const name = (asksName ? values.account_name : resolvedName || values.account_name) ?? '';
  const ready = fieldsOk && name.trim().length > 1 && !lookingUp;

  function review(to: { country: string; currency: string; rail: string; name: string; fields: Record<string, string> }) {
    router.push({
      pathname: '/payout-review',
      params: { asset, amount, reason, ...to, fields: JSON.stringify(to.fields) },
    });
  }

  const matches = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (countries ?? []).filter((c) => !q || c.name.toLowerCase().includes(q));
  }, [countries, search]);

  const banks = useMemo(() => {
    const q = bankSearch.trim().toLowerCase();
    if (!q) return [];
    return (form?.banks ?? []).filter((b) => b.name.toLowerCase().includes(q)).slice(0, 6);
  }, [form, bankSearch]);

  if (!country) {
    return (
      <Screen sheet={`Send ${formatInput(asset, amount)} ${asset}`}>
        <TextInput
          style={styles.input}
          value={search}
          onChangeText={setSearch}
          placeholder="Search countries"
          placeholderTextColor={colors.inkMuted}
          autoCorrect={false}
          selectionColor={colors.ink}
        />
        <View style={styles.rule} />
        {!!error && <Text style={[styles.error, { marginTop: space.sm }]}>{error}</Text>}
        {!countries && !error && <ActivityIndicator color={colors.ink} style={{ marginTop: space.lg }} />}
        <ScrollView keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false}>
          {!search && recent.length > 0 && (
            <View style={{ marginTop: space.md }}>
              <Text style={styles.muted}>Recent</Text>
              {recent.map((b) => (
                <Row
                  key={b.id}
                  title={b.account_name}
                  detail={`${RAIL_LABEL[b.rail] ?? b.rail} · ${b.currency}`}
                  onPress={() => review({ country: b.country, currency: b.currency, rail: b.rail, name: b.account_name, fields: b.fields })}
                />
              ))}
              <View style={styles.rule} />
            </View>
          )}
          {matches.map((c) => (
            <Row
              key={c.code}
              title={`${c.flag}  ${c.name}`}
              detail={c.corridors.map((k) => k.currency).join(', ')}
              onPress={() => choose(c)}
            />
          ))}
        </ScrollView>
      </Screen>
    );
  }

  return (
    <Screen sheet={`${country.flag}  ${country.name}`}>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === 'ios' ? 'padding' : undefined} keyboardVerticalOffset={40}>
        <ScrollView keyboardShouldPersistTaps="handled" showsVerticalScrollIndicator={false} contentContainerStyle={{ gap: space.md, paddingBottom: space.lg }}>
          <IconButton icon={ArrowLeft} label="Choose another country" onPress={() => setCountry(null)} />
          {!!error && <Text style={styles.error}>{error}</Text>}
          {!details && !error && <ActivityIndicator color={colors.ink} />}

          {details && Object.keys(details.rails).length > 1 && (
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.xs }}>
              {Object.keys(details.rails).map((r) => (
                <Chip key={r} label={RAIL_LABEL[r] ?? details.rails[r].label} selected={r === rail} onPress={() => pickRail(details, r)} />
              ))}
            </View>
          )}

          {form?.fields.map((f) => {
            if (f.component === 'select' && f.options_ref === 'banks') {
              const chosen = form.banks?.find((b) => b.code === values[f.key]);
              return (
                <View key={f.key}>
                  <Text style={styles.muted}>{f.label}</Text>
                  <TextInput
                    style={styles.input}
                    value={chosen ? chosen.name : bankSearch}
                    onChangeText={(text) => {
                      set(f.key, '');
                      setBankSearch(text);
                    }}
                    placeholder="Search banks"
                    placeholderTextColor={colors.inkMuted}
                    autoCorrect={false}
                    selectionColor={colors.ink}
                  />
                  <View style={styles.rule} />
                  {!chosen &&
                    banks.map((b) => (
                      <Row
                        key={b.code}
                        title={b.name}
                        onPress={() => {
                          set(f.key, b.code);
                          setBankSearch('');
                        }}
                      />
                    ))}
                </View>
              );
            }
            if (f.component === 'select') {
              // A lone option was filled in already; nothing to ask.
              if (f.options.length <= 1) return null;
              return (
                <View key={f.key} style={{ gap: space.sm }}>
                  <Text style={styles.muted}>{f.label}</Text>
                  <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.xs }}>
                    {f.options.map((o) => (
                      <Chip key={o.value} label={o.label} selected={values[f.key] === o.value} onPress={() => set(f.key, o.value)} />
                    ))}
                  </View>
                </View>
              );
            }
            const bad = !!values[f.key] && !fieldOk(f);
            return (
              <View key={f.key}>
                <Text style={styles.muted}>{f.label}</Text>
                <TextInput
                  style={styles.input}
                  value={values[f.key] ?? ''}
                  onChangeText={(text) => set(f.key, text)}
                  placeholder={f.placeholder?.replace(/^e\.g\.\s*/, '')}
                  placeholderTextColor={colors.inkMuted}
                  keyboardType={/number|phone/i.test(f.label + (f.description ?? '')) && f.key !== 'account_name' ? 'number-pad' : 'default'}
                  autoCorrect={false}
                  selectionColor={colors.ink}
                />
                <View style={styles.rule} />
                {bad && !!f.description && <Text style={[styles.error, { marginTop: space.xs }]}>{f.description}</Text>}
              </View>
            );
          })}

          {form && !asksName && fieldsOk && (
            <View>
              <Text style={styles.muted}>Recipient</Text>
              {lookingUp ? (
                <ActivityIndicator color={colors.ink} style={{ alignSelf: 'flex-start', marginVertical: space.md }} />
              ) : resolvedName ? (
                <Text style={[styles.title, { marginVertical: space.sm }]}>{resolvedName}</Text>
              ) : (
                <>
                  <TextInput
                    style={styles.input}
                    value={values.account_name ?? ''}
                    onChangeText={(text) => set('account_name', text)}
                    placeholder="Recipient's full name"
                    placeholderTextColor={colors.inkMuted}
                    autoCorrect={false}
                    selectionColor={colors.ink}
                  />
                  <View style={styles.rule} />
                </>
              )}
            </View>
          )}

          {form && (
            <View style={{ gap: space.sm }}>
              <Text style={styles.muted}>What's it for?</Text>
              <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.xs }}>
                {REASONS.map((r) => (
                  <Chip key={r.value} label={r.label} selected={reason === r.value} onPress={() => setReason(r.value)} />
                ))}
              </View>
            </View>
          )}
        </ScrollView>

        <Button
          label="Continue"
          disabled={!ready}
          onPress={() => {
            const { account_name: _name, ...fields } = values;
            review({ country: country.code, currency, rail, name: name.trim(), fields: asksName ? values : fields });
          }}
        />
      </KeyboardAvoidingView>
    </Screen>
  );
}
