// Package prices serves indicative rates for valuing balances on screen.
// They are never used to move money: swaps always run on a locked quote.
package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/money"
)

var ErrUnavailable = errors.New("prices: not available")

// Rates values one major unit of each asset in naira and in US dollars.
// USDT stands in for the dollar.
type Rates struct {
	NGN  map[money.Asset]float64 `json:"ngn"`
	USD  map[money.Asset]float64 `json:"usd"`
	AsOf time.Time               `json:"as_of"`
}

type Service struct {
	Bitnob *bitnob.Client
	// TTL is how long a set of rates is reused before asking Bitnob again.
	TTL time.Duration

	mu     sync.Mutex
	cached *Rates
}

func (s *Service) Rates(ctx context.Context) (Rates, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.cached.AsOf) < s.TTL {
		return *s.cached, nil
	}
	fresh, err := s.fetch(ctx)
	if err != nil {
		// Slightly old rates beat none for a display-only number.
		if s.cached != nil && time.Since(s.cached.AsOf) < 15*time.Minute {
			return *s.cached, nil
		}
		return Rates{}, err
	}
	s.cached = &fresh
	return fresh, nil
}

func (s *Service) fetch(ctx context.Context) (Rates, error) {
	if !s.Bitnob.Configured() {
		return Rates{}, ErrUnavailable
	}

	// Crypto pairs come from Get Prices.
	raw, err := s.Bitnob.Raw(ctx, http.MethodGet, "/api/trading/prices", nil)
	if err != nil {
		return Rates{}, err
	}
	var body struct {
		Data struct {
			Prices []struct {
				Base  string `json:"base_currency"`
				Quote string `json:"quote_currency"`
				Price string `json:"price"`
			} `json:"prices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Rates{}, err
	}
	pair := map[string]float64{}
	for _, p := range body.Data.Prices {
		if v, err := strconv.ParseFloat(p.Price, 64); err == nil && v > 0 {
			pair[p.Base+"/"+p.Quote] = v
		}
	}
	btcUSD, usdcUSD := pair["BTC/USDT"], pair["USDC/USDT"]
	if btcUSD == 0 || usdcUSD == 0 {
		return Rates{}, fmt.Errorf("prices: Bitnob returned no BTC/USDT or USDC/USDT price")
	}

	ngnPerUSD, err := s.nairaPerDollar(ctx)
	if err != nil {
		return Rates{}, err
	}

	return Rates{
		USD: map[money.Asset]float64{
			money.NGN: 1 / ngnPerUSD, money.USDT: 1, money.USDC: usdcUSD, money.BTC: btcUSD,
		},
		NGN: map[money.Asset]float64{
			money.NGN: 1, money.USDT: ngnPerUSD, money.USDC: ngnPerUSD * usdcUSD, money.BTC: ngnPerUSD * btcUSD,
		},
		AsOf: time.Now().UTC(),
	}, nil
}

// nairaPerDollar has no price feed, so it reads the rate off a quote that is
// never executed. Selling USDT is the honest valuation of a dollar balance;
// if Bitnob won't quote that (it checks our balance there), the buy-side
// rate is close enough for display.
func (s *Service) nairaPerDollar(ctx context.Context) (float64, error) {
	q, _, err := s.Bitnob.CreateTradingQuote(ctx, bitnob.TradingQuoteRequest{
		BaseCurrency: "USDT", QuoteCurrency: "NGN", Side: "sell", Quantity: "1",
	})
	if err == nil {
		if v, perr := strconv.ParseFloat(q.Price, 64); perr == nil && v > 0 {
			return v, nil
		}
	}
	q, _, err = s.Bitnob.CreateTradingQuote(ctx, bitnob.TradingQuoteRequest{
		BaseCurrency: "NGN", QuoteCurrency: "USDT", Side: "sell", Quantity: "1000",
	})
	if err != nil {
		return 0, err
	}
	v, perr := strconv.ParseFloat(q.Price, 64)
	if perr != nil || v <= 0 {
		return 0, fmt.Errorf("prices: unusable NGN/USDT price %q", q.Price)
	}
	return 1 / v, nil
}
