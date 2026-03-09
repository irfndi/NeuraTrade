package autonomy

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestResolveExecutableSizingConstraints_BitgetFutures(t *testing.T) {
	constraints := ResolveExecutableSizingConstraints("bitget", decimal.NewFromFloat(46.93), 5)

	require.Equal(t, "bitget", constraints.Exchange)
	require.True(t, constraints.MinOrderNotional.Equal(BitgetFuturesMinNotional()))
	require.InDelta(t, 1.20, constraints.MinInitialMargin.InexactFloat64(), 0.0001)
	require.InDelta(t, 12.7850, constraints.MinExecutableSizePct, 0.0001)
}
