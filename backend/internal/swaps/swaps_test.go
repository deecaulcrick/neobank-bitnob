package swaps

import (
	"testing"

	"github.com/deecaulcrick/neobank/backend/internal/money"
)

func TestToMinorTruncates(t *testing.T) {
	cases := []struct {
		asset money.Asset
		in    string
		want  int64
	}{
		{money.USDT, "0.730393419316333", 730393}, // never rounds up
		{money.NGN, "1352.7726328", 135277},
		{money.BTC, "0.00001691", 1691},
		{money.USDC, "1.00", 1_000_000},
		{money.NGN, "1000", 100_000},
	}
	for _, c := range cases {
		got, err := toMinor(c.asset, c.in, false)
		if err != nil || got != c.want {
			t.Errorf("toMinor(%s, %q) = %d, %v; want %d", c.asset, c.in, got, err, c.want)
		}
	}
	if up, _ := toMinor(money.NGN, "4118.991", true); up != 411_900 {
		t.Errorf("round up = %d, want 411900", up)
	}
	if _, err := toMinor(money.NGN, "-1", false); err == nil {
		t.Error("negative amount accepted")
	}
}

func TestPlain(t *testing.T) {
	cases := map[string]string{
		plain(money.NGN, 100_000): "1000",
		plain(money.NGN, 150_050): "1500.5",
		plain(money.BTC, 1_000):   "0.00001",
		plain(money.USDT, 1):      "0.000001",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("plain = %q, want %q", got, want)
		}
	}
}
