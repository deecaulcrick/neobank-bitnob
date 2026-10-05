// Package money defines the assets we hold and integer minor-unit arithmetic.
// Amounts are always int64 in the asset's smallest unit; floats never touch money.
package money

import (
	"fmt"
	"strings"
)

type Asset string

const (
	NGN  Asset = "NGN"
	USDT Asset = "USDT"
	USDC Asset = "USDC"
	BTC  Asset = "BTC"
)

var Assets = []Asset{NGN, USDT, USDC, BTC}

// Decimals is the number of decimal places between the major unit and the
// smallest unit we store (kobo, micro, sat).
func (a Asset) Decimals() int {
	switch a {
	case NGN:
		return 2
	case USDT, USDC:
		return 6
	case BTC:
		return 8
	}
	return 0
}

func (a Asset) Valid() bool {
	switch a {
	case NGN, USDT, USDC, BTC:
		return true
	}
	return false
}

func ParseAsset(s string) (Asset, error) {
	a := Asset(strings.ToUpper(strings.TrimSpace(s)))
	if !a.Valid() {
		return "", fmt.Errorf("unsupported asset %q", s)
	}
	return a, nil
}

// Parse converts a decimal string in major units ("1500.50") to minor units.
// It rejects more decimal places than the asset supports rather than rounding.
func Parse(a Asset, s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	d := a.Decimals()
	if len(frac) > d {
		return 0, fmt.Errorf("%s supports at most %d decimal places", a, d)
	}
	frac += strings.Repeat("0", d-len(frac))

	var n int64
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid amount %q", s)
		}
		digit := int64(r - '0')
		if n > (1<<63-1-digit)/10 {
			return 0, fmt.Errorf("amount %q overflows", s)
		}
		n = n*10 + digit
	}
	return n, nil
}

// Format renders minor units as a plain decimal string in major units.
func Format(a Asset, minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	d := a.Decimals()
	if d == 0 {
		return fmt.Sprintf("%s%d", sign, minor)
	}
	s := fmt.Sprintf("%0*d", d+1, minor)
	return sign + s[:len(s)-d] + "." + s[len(s)-d:]
}
