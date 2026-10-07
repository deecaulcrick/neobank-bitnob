import { Pencil } from 'lucide-react-native';
import { useState } from 'react';
import { Modal, Pressable, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import {
  appendDigit,
  ASSETS,
  DECIMALS,
  formatBalance,
  formatFiatInput,
  formatInput,
  minorToInput,
  type Asset,
} from '../lib/money';
import { useBalancesState } from '../lib/useBalances';
import { colors, radius, space } from '../theme';
import { Keypad } from './Keypad';
import { Button, Chip, styles } from './ui';

type Props = {
  // What the amount is in: one of our assets, or (with `fiat`) a payout
  // currency such as GHS.
  asset: Asset;
  fiat?: string;
  amount: string;
  onChange: (asset: Asset, amount: string) => void;
  // Assets the amount may be in; defaults to all four. Ignored with `fiat`.
  assets?: Asset[];
  fontSize?: number;
  // Shown in place of the figure when there isn't one yet.
  placeholder?: string;
};

// An amount on a send or review screen. Tapping it opens a keypad right
// there, so a figure can be fixed without going back to the Pay tab.
export function AmountField({ asset, fiat, amount, onChange, assets = ASSETS, fontSize = 34, placeholder }: Props) {
  const [open, setOpen] = useState(false);
  const [draftAsset, setDraftAsset] = useState(asset);
  const [draft, setDraft] = useState(amount);
  const balance = useBalancesState().balances?.find((b) => b.asset === draftAsset)?.available;
  const available = fiat ? undefined : balance;

  const show = (a: Asset, v: string) => (fiat ? formatFiatInput(fiat, v) : formatInput(a, v));
  const decimals = fiat ? 2 : DECIMALS[draftAsset];

  return (
    <>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`Amount ${show(asset, amount)}. Tap to change.`}
        onPress={() => {
          setDraftAsset(asset);
          setDraft(amount);
          setOpen(true);
        }}
        style={({ pressed }) => [{ flexDirection: 'row', alignItems: 'center', gap: space.sm }, pressed && { opacity: 0.6 }]}>
        <Text
          style={[styles.amount, { fontSize, letterSpacing: -1, flexShrink: 1 }, !amount && { color: colors.inkMuted }]}
          adjustsFontSizeToFit
          numberOfLines={1}>
          {!amount && placeholder ? placeholder : show(asset, amount)}
        </Text>
        <View
          style={{
            width: 32,
            height: 32,
            borderRadius: radius.pill,
            backgroundColor: colors.card,
            alignItems: 'center',
            justifyContent: 'center',
          }}>
          <Pencil size={15} strokeWidth={2} color={colors.ink} />
        </View>
      </Pressable>

      <Modal visible={open} animationType="slide" presentationStyle="pageSheet" onRequestClose={() => setOpen(false)}>
        <SafeAreaView style={{ flex: 1, backgroundColor: colors.sheet }}>
          <View style={{ flex: 1, padding: space.md, justifyContent: 'space-between' }}>
            <View style={[styles.row, { minHeight: 36 }]}>
              <View style={{ flexDirection: 'row', gap: space.xs }}>
                {!fiat &&
                  assets.length > 1 &&
                  assets.map((a) => (
                    <Chip
                      key={a}
                      label={a}
                      selected={a === draftAsset}
                      onPress={() => {
                        setDraftAsset(a);
                        setDraft('');
                      }}
                    />
                  ))}
              </View>
              <Pressable onPress={() => setOpen(false)} hitSlop={12}>
                <Text style={styles.muted}>Cancel</Text>
              </Pressable>
            </View>

            <View style={{ alignItems: 'center', gap: space.sm }}>
              <Text style={[styles.amount, { fontSize: 72 }]} adjustsFontSizeToFit numberOfLines={1}>
                {show(draftAsset, draft)}
              </Text>
              {available !== undefined && (
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: space.sm }}>
                  <Text style={styles.muted}>{formatBalance(draftAsset, available)} available</Text>
                  {available > 0 && <Chip label="Max" onPress={() => setDraft(minorToInput(draftAsset, available))} />}
                </View>
              )}
            </View>

            <View style={{ gap: space.md }}>
              <Keypad onKey={(key) => setDraft((cur) => appendDigit(decimals, cur, key))} />
              <Button
                label="Done"
                disabled={!(Number(draft) > 0)}
                onPress={() => {
                  onChange(draftAsset, draft);
                  setOpen(false);
                }}
              />
            </View>
          </View>
        </SafeAreaView>
      </Modal>
    </>
  );
}
