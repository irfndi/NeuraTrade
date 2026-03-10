package autonomy

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestBitgetFuturesMinNotional_UsesEnvOverride(t *testing.T) {
	t.Setenv("NEURATRADE_BITGET_FUTURES_MIN_NOTIONAL_USDT", "7.25")

	require.True(t, BitgetFuturesMinNotional().Equal(decimal.NewFromFloat(7.25)))
}

func TestResolveExecutableSizingConstraints_BitgetFutures(t *testing.T) {
	tests := []struct {
		name                        string
		walletBalance               decimal.Decimal
		expectedMinOrderNotional    decimal.Decimal
		expectedMinInitialMargin    decimal.Decimal
		expectedMinExecutableSize   float64
		expectedNonExecutableWallet bool
	}{
		{
			name:                        "happy_path",
			walletBalance:               decimal.NewFromFloat(46.93),
			expectedMinOrderNotional:    decimal.NewFromFloat(6),
			expectedMinInitialMargin:    decimal.NewFromFloat(1.2),
			expectedMinExecutableSize:   12.7850,
			expectedNonExecutableWallet: false,
		},
		{
			name:                        "ceils_executable_size_pct_to_avoid_rounding_down",
			walletBalance:               decimal.NewFromFloat(46.932),
			expectedMinOrderNotional:    decimal.NewFromFloat(6),
			expectedMinInitialMargin:    decimal.NewFromFloat(1.2),
			expectedMinExecutableSize:   12.7845,
			expectedNonExecutableWallet: false,
		},
		{
			name:                        "zero_wallet_balance",
			walletBalance:               decimal.Zero,
			expectedMinOrderNotional:    decimal.NewFromFloat(6),
			expectedMinInitialMargin:    decimal.NewFromFloat(1.2),
			expectedMinExecutableSize:   0,
			expectedNonExecutableWallet: true,
		},
		{
			name:                        "subminimum_wallet_balance",
			walletBalance:               decimal.NewFromFloat(5),
			expectedMinOrderNotional:    decimal.NewFromFloat(6),
			expectedMinInitialMargin:    decimal.NewFromFloat(1.2),
			expectedMinExecutableSize:   0,
			expectedNonExecutableWallet: true,
		},
		{
			name:                        "tiny_nonzero_wallet_balance",
			walletBalance:               decimal.NewFromFloat(0.0000001),
			expectedMinOrderNotional:    decimal.NewFromFloat(6),
			expectedMinInitialMargin:    decimal.NewFromFloat(1.2),
			expectedMinExecutableSize:   0,
			expectedNonExecutableWallet: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraints := ResolveExecutableSizingConstraints("bitget", tt.walletBalance, 5)

			require.Equal(t, "bitget", constraints.Exchange)
			require.True(t, constraints.MinOrderNotional.Equal(tt.expectedMinOrderNotional))
			require.True(t, constraints.MinInitialMargin.Equal(tt.expectedMinInitialMargin))
			require.InDelta(t, tt.expectedMinExecutableSize, constraints.MinExecutableSizePct, 0.0001)
			require.Equal(t, tt.expectedNonExecutableWallet, constraints.NonExecutableDueToWallet)
		})
	}
}
