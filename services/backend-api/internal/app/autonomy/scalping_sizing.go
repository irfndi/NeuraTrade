package autonomy

import (
	"strings"

	"github.com/shopspring/decimal"
)

const BitgetFuturesMinNotionalUSDT = 6.0

type ExecutableSizingConstraints struct {
	Exchange                 string
	MinOrderNotional         decimal.Decimal
	MinInitialMargin         decimal.Decimal
	MinExecutableSizePct     float64
	NonExecutableDueToWallet bool
}

func BitgetFuturesMinNotional() decimal.Decimal {
	return decimal.NewFromFloat(BitgetFuturesMinNotionalUSDT)
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
		Round(4).
		InexactFloat64()

	return constraints
}
