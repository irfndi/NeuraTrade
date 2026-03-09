package autonomy

import (
	"os"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	defaultBitgetFuturesMinNotionalUSDT = "6.0"
	bitgetFuturesMinNotionalEnv         = "NEURATRADE_BITGET_FUTURES_MIN_NOTIONAL_USDT"
)

type ExecutableSizingConstraints struct {
	Exchange                 string
	MinOrderNotional         decimal.Decimal
	MinInitialMargin         decimal.Decimal
	MinExecutableSizePct     float64
	NonExecutableDueToWallet bool
}

func BitgetFuturesMinNotional() decimal.Decimal {
	if raw := strings.TrimSpace(os.Getenv(bitgetFuturesMinNotionalEnv)); raw != "" {
		if configured, err := decimal.NewFromString(raw); err == nil && configured.GreaterThan(decimal.Zero) {
			return configured
		}
	}
	configured, err := decimal.NewFromString(defaultBitgetFuturesMinNotionalUSDT)
	if err != nil {
		return decimal.NewFromInt(6)
	}
	return configured
}

func ResolveExecutableSizingConstraints(exchange string, walletBalance decimal.Decimal, leverage int) ExecutableSizingConstraints {
	normalizedExchange := strings.ToLower(strings.TrimSpace(exchange))
	constraints := ExecutableSizingConstraints{
		Exchange: normalizedExchange,
	}

	switch normalizedExchange {
	case "bitget":
		constraints.MinOrderNotional = BitgetFuturesMinNotional()
	}

	if constraints.MinOrderNotional.LessThanOrEqual(decimal.Zero) {
		return constraints
	}

	if leverage <= 0 {
		leverage = 1
	}
	constraints.MinInitialMargin = constraints.MinOrderNotional.Div(decimal.NewFromInt(int64(leverage)))

	if !walletBalance.GreaterThan(decimal.Zero) {
		constraints.NonExecutableDueToWallet = true
		return constraints
	}
	if walletBalance.LessThan(constraints.MinOrderNotional) {
		constraints.NonExecutableDueToWallet = true
		return constraints
	}

	constraints.MinExecutableSizePct = constraints.MinOrderNotional.
		Div(walletBalance).
		Mul(decimal.NewFromInt(100)).
		Mul(decimal.NewFromInt(10000)).
		Ceil().
		Div(decimal.NewFromInt(10000)).
		InexactFloat64()

	return constraints
}
