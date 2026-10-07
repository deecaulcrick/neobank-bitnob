import { ArrowLeft } from 'lucide-react-native';
import { useState } from 'react';
import { KeyboardAvoidingView, Platform, Pressable, Text, TextInput, View, type TextInputProps } from 'react-native';

import { Keypad } from '../components/Keypad';
import { Button, IconButton, Screen, styles } from '../components/ui';
import { PinDots } from '../lib/pin';
import { api } from '../lib/api';
import { useMeState } from '../lib/me';
import { supabase } from '../lib/supabase';
import { colors, space } from '../theme';

type Step = 'name' | 'email' | 'dob' | 'bvn' | 'tag' | 'pin' | 'pin2';
const KYC_STEPS: Step[] = ['name', 'email', 'dob', 'bvn'];

// Typing 01021994 reads as 01/02/1994.
function formatDob(input: string) {
  const d = input.replace(/\D/g, '').slice(0, 8);
  return [d.slice(0, 2), d.slice(2, 4), d.slice(4)].filter(Boolean).join('/');
}

// DD/MM/YYYY -> YYYY-MM-DD, or null if it isn't a real date.
function toIsoDate(dob: string) {
  const [dd, mm, yyyy] = dob.split('/');
  if (!dd || !mm || yyyy?.length !== 4) return null;
  const date = new Date(Date.UTC(+yyyy, +mm - 1, +dd));
  if (date.getUTCDate() !== +dd || date.getUTCMonth() !== +mm - 1) return null;
  return `${yyyy}-${mm}-${dd}`;
}

function Field(props: TextInputProps) {
  return (
    <>
      <TextInput
        style={[styles.input, { fontSize: 26 }]}
        placeholderTextColor={colors.inkMuted}
        selectionColor={colors.ink}
        autoCorrect={false}
        {...props}
      />
      <View style={styles.rule} />
    </>
  );
}

// Tier-1 onboarding, one question per screen: legal name, email, date of
// birth and BVN open the NGN account; then the user picks a tag.
export default function Onboarding() {
  const { me, refresh } = useMeState();
  const [step, setStep] = useState<Step>(!me || me.kyc_tier < 1 ? 'name' : !me.tag ? 'tag' : 'pin');
  const [pin, setPin] = useState('');
  const [pin2, setPin2] = useState('');
  const [firstName, setFirstName] = useState('');
  const [lastName, setLastName] = useState('');
  const [email, setEmail] = useState('');
  const [dob, setDob] = useState('');
  const [bvn, setBvn] = useState('');
  const [tag, setTag] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const isoDob = toIsoDate(dob);
  const cleanTag = tag.trim().replace(/^@/, '').toLowerCase();
  const index = KYC_STEPS.indexOf(step);

  const valid: Record<Step, boolean> = {
    name: firstName.trim().length > 0 && lastName.trim().length > 0,
    email: /^\S+@\S+\.\S+$/.test(email.trim()),
    dob: isoDob !== null,
    bvn: /^\d{11}$/.test(bvn),
    tag: /^[a-z0-9_]{3,20}$/.test(cleanTag),
    pin: pin.length === 4,
    pin2: pin2.length === 4,
  };

  async function run(action: () => Promise<unknown>) {
    setLoading(true);
    setError('');
    try {
      await action();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong');
    } finally {
      setLoading(false);
    }
  }

  function next() {
    setError('');
    if (step === 'bvn') {
      return run(async () => {
        await api.submitKyc({
          first_name: firstName.trim(),
          last_name: lastName.trim(),
          email: email.trim(),
          date_of_birth: isoDob!,
          bvn,
        });
        setStep('tag');
        await refresh();
      });
    }
    if (step === 'tag') {
      return run(async () => {
        await api.setTag(cleanTag);
        setStep('pin');
        await refresh();
      });
    }
    if (step === 'pin') return setStep('pin2');
    if (step === 'pin2') {
      if (pin !== pin2) {
        setPin('');
        setPin2('');
        setStep('pin');
        return setError("Those didn't match. Try again.");
      }
      // Once the PIN is saved the root layout swaps onboarding for the app.
      return run(async () => {
        try {
          await api.setPin(pin);
        } catch (e) {
          setPin('');
          setPin2('');
          setStep('pin');
          throw e;
        }
        await refresh();
      });
    }
    setStep(KYC_STEPS[index + 1]);
  }

  return (
    <Screen>
      <KeyboardAvoidingView
        style={{ flex: 1, justifyContent: 'space-between' }}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
        <View style={{ gap: space.sm }}>
          <View style={[styles.row, { height: 48 }]}>
            {index > 0 ? (
              <IconButton
                icon={ArrowLeft}
                label="Back"
                onPress={() => {
                  setError('');
                  setStep(KYC_STEPS[index - 1]);
                }}
              />
            ) : (
              <View />
            )}
            <Pressable onPress={() => supabase.auth.signOut()} hitSlop={12}>
              <Text style={styles.muted}>Sign out</Text>
            </Pressable>
          </View>

          {step === 'name' && (
            <>
              <Text style={[styles.heading, { marginTop: space.md }]}>What's your legal name?</Text>
              <Text style={styles.muted}>Exactly as it appears on your BVN.</Text>
              <Field value={firstName} onChangeText={setFirstName} placeholder="First name" autoComplete="given-name" autoFocus />
              <Field value={lastName} onChangeText={setLastName} placeholder="Last name" autoComplete="family-name" />
            </>
          )}
          {step === 'email' && (
            <>
              <Text style={[styles.heading, { marginTop: space.md }]}>What's your email?</Text>
              <Text style={styles.muted}>For receipts and account notices.</Text>
              <Field
                value={email}
                onChangeText={setEmail}
                placeholder="you@example.com"
                keyboardType="email-address"
                autoCapitalize="none"
                autoComplete="email"
                autoFocus
              />
            </>
          )}
          {step === 'dob' && (
            <>
              <Text style={[styles.heading, { marginTop: space.md }]}>When were you born?</Text>
              <Text style={styles.muted}>It has to match your BVN. You must be 18 or older.</Text>
              <Field
                value={dob}
                onChangeText={(text) => setDob(formatDob(text))}
                placeholder="DD/MM/YYYY"
                keyboardType="number-pad"
                maxLength={10}
                autoFocus
              />
            </>
          )}
          {step === 'bvn' && (
            <>
              <Text style={[styles.heading, { marginTop: space.md }]}>Enter your BVN</Text>
              <Text style={styles.muted}>
                We use it once to verify you and open your account number. We don't keep it. Dial *565*0# to find yours.
              </Text>
              <Field
                value={bvn}
                onChangeText={(text) => setBvn(text.replace(/\D/g, ''))}
                placeholder="11 digits"
                keyboardType="number-pad"
                maxLength={11}
                autoFocus
              />
            </>
          )}
          {step === 'tag' && (
            <>
              <Text style={[styles.heading, { marginTop: space.md }]}>Pick your tag</Text>
              <Text style={styles.muted}>How people find and pay you. Letters, numbers and underscores.</Text>
              <Field value={tag} onChangeText={setTag} placeholder="@yourname" autoCapitalize="none" maxLength={21} autoFocus />
            </>
          )}
          {(step === 'pin' || step === 'pin2') && (
            <View style={{ gap: space.lg, marginTop: space.md }}>
              <View style={{ gap: space.sm }}>
                <Text style={styles.heading}>{step === 'pin' ? 'Create a PIN' : 'Enter it again'}</Text>
                <Text style={styles.muted}>4 digits. You'll use it to confirm every payment, swap and send.</Text>
              </View>
              <PinDots length={(step === 'pin' ? pin : pin2).length} />
            </View>
          )}
          {!!error && <Text style={styles.error}>{error}</Text>}
        </View>

        {(step === 'pin' || step === 'pin2') && (
          <Keypad
            decimal={false}
            compact
            onKey={(key) => {
              if (key === '.') return;
              const edit = (cur: string) => (key === 'back' ? cur.slice(0, -1) : (cur + key).slice(0, 4));
              if (step === 'pin') setPin(edit);
              else setPin2(edit);
            }}
          />
        )}

        <Button
          label={step === 'bvn' ? 'Open my account' : step === 'pin2' ? 'Finish' : 'Continue'}
          onPress={next}
          loading={loading}
          disabled={!valid[step]}
        />
      </KeyboardAvoidingView>
    </Screen>
  );
}
