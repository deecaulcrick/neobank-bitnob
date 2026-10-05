package money

import "testing"

func TestParseFormat(t *testing.T) {
	cases := []struct {
		asset Asset
		in    string
		minor int64
		out   string
	}{
		{NGN, "1500.5", 150050, "1500.50"},
		{NGN, "0.01", 1, "0.01"},
		{USDT, "12", 12_000_000, "12.000000"},
		{USDC, ".5", 500_000, "0.500000"},
		{BTC, "0.00000001", 1, "0.00000001"},
		{BTC, "21000000", 2_100_000_000_000_000, "21000000.00000000"},
	}
	for _, c := range cases {
		got, err := Parse(c.asset, c.in)
		if err != nil {
			t.Fatalf("Parse(%s, %q): %v", c.asset, c.in, err)
		}
		if got != c.minor {
			t.Errorf("Parse(%s, %q) = %d, want %d", c.asset, c.in, got, c.minor)
		}
		if s := Format(c.asset, got); s != c.out {
			t.Errorf("Format(%s, %d) = %q, want %q", c.asset, got, s, c.out)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{"", "-1", "+1", "1.234", "1e3", "abc", "1,000", "99999999999999999999"} {
		if _, err := Parse(NGN, in); err == nil {
			t.Errorf("Parse(NGN, %q) should fail", in)
		}
	}
}
