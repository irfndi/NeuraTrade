package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestLoadScalpingWeightsFromEnv_DefaultsWhenUnset(t *testing.T) {
	for _, name := range []string{
		"NEURATRADE_SCALPING_WEIGHT_SPREAD",
		"NEURATRADE_SCALPING_WEIGHT_IMBALANCE",
		"NEURATRADE_SCALPING_WEIGHT_VOLATILITY",
		"NEURATRADE_SCALPING_WEIGHT_TREND",
		"NEURATRADE_SCALPING_WEIGHT_LIQUIDITY",
		"NEURATRADE_SCALPING_WEIGHT_RSI",
	} {
		t.Setenv(name, "")
	}
	w, err := loadScalpingWeightsFromEnv()
	require.NoError(t, err)
	require.True(t, w.Trend.Equal(decimal.RequireFromString("0.35")))
}

func TestLoadScalpingWeightsFromEnv_AppliesOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_WEIGHT_TREND", "0.20")
	t.Setenv("NEURATRADE_SCALPING_WEIGHT_RSI", "0.30")
	w, err := loadScalpingWeightsFromEnv()
	require.NoError(t, err)
	require.True(t, w.Trend.Equal(decimal.RequireFromString("0.20")))
	require.True(t, w.RSI.Equal(decimal.RequireFromString("0.30")))
}

func TestLoadScalpingWeightsFromEnv_RejectsSumNotOne(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_WEIGHT_TREND", "1.20")
	_, err := loadScalpingWeightsFromEnv()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "sum to 1.0"))
}

func TestLoadScalpingWeightsFromEnv_RejectsInvalidNumber(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_WEIGHT_RSI", "not-a-number")
	_, err := loadScalpingWeightsFromEnv()
	require.Error(t, err)
}

func TestDefaultRiskConfig_DefaultsWhenEnvUnset(t *testing.T) {
	t.Setenv("NEURATRADE_RISK_MAX_DRAWDOWN", "")
	t.Setenv("NEURATRADE_RISK_MAX_DAILY_LOSS", "")
	cfg := DefaultRiskConfig()
	require.True(t, cfg.MaxDrawdown.Equal(decimal.RequireFromString("0.1")))
	require.True(t, cfg.MaxDailyLoss.Equal(decimal.RequireFromString("0.05")))
}

func TestDefaultRiskConfig_AppliesEnvOverrides(t *testing.T) {
	t.Setenv("NEURATRADE_RISK_MAX_DRAWDOWN", "0.08")
	t.Setenv("NEURATRADE_RISK_MAX_DAILY_LOSS", "0.03")
	cfg := DefaultRiskConfig()
	require.True(t, cfg.MaxDrawdown.Equal(decimal.RequireFromString("0.08")))
	require.True(t, cfg.MaxDailyLoss.Equal(decimal.RequireFromString("0.03")))
}

func TestDefaultRiskConfig_RejectsInvalidValues(t *testing.T) {
	t.Setenv("NEURATRADE_RISK_MAX_DRAWDOWN", "not-a-number")
	t.Setenv("NEURATRADE_RISK_MAX_DAILY_LOSS", "2.0")
	cfg := DefaultRiskConfig()
	require.True(t, cfg.MaxDrawdown.Equal(decimal.RequireFromString("0.1")))
	require.True(t, cfg.MaxDailyLoss.Equal(decimal.RequireFromString("0.05")))
}

type noopActor struct{}

func (noopActor) Receive(ctx context.Context, env actor.Envelope) error { return nil }
func (noopActor) ID() string                                            { return "noop" }

func TestSpawnActorAndWait_Success(t *testing.T) {
	a := &Application{ActorSystem: actor.NewSystem(actor.DefaultConfig())}
	sa, err := a.spawnActorAndWait("noop", noopActor{}, actor.DefaultConfig())
	require.NoError(t, err)
	require.NotNil(t, sa.ref)
	require.NotNil(t, sa.runCancel)
	sa.runCancel()
	sa.ref.Stop()
}
