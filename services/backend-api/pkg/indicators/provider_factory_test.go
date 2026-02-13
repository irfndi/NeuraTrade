package indicators

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestNewProvider_Talib(t *testing.T) {
	provider, err := NewProvider(&ProviderConfig{Type: ProviderTypeTalib})
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "talib", provider.Name())
}

func TestNewProvider_Default(t *testing.T) {
	provider, err := NewProvider(nil)
	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "talib", provider.Name())
}

func TestNewProvider_GoFlux_NotImplemented(t *testing.T) {
	provider, err := NewProvider(&ProviderConfig{Type: ProviderTypeGoFlux})
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestNewProvider_UnknownType(t *testing.T) {
	provider, err := NewProvider(&ProviderConfig{Type: "unknown"})
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "unknown provider type")
}

func TestNewDefaultProvider(t *testing.T) {
	provider := NewDefaultProvider()
	assert.NotNil(t, provider)
	assert.Equal(t, "talib", provider.Name())
	assert.Equal(t, "1.0.0", provider.Version())
}

func TestTalibAdapter_SMA(t *testing.T) {
	adapter := NewTalibAdapter()
	prices := []decimal.Decimal{
		decimal.NewFromFloat(10),
		decimal.NewFromFloat(11),
		decimal.NewFromFloat(12),
		decimal.NewFromFloat(13),
		decimal.NewFromFloat(14),
		decimal.NewFromFloat(15),
		decimal.NewFromFloat(16),
		decimal.NewFromFloat(17),
		decimal.NewFromFloat(18),
		decimal.NewFromFloat(19),
		decimal.NewFromFloat(20),
	}

	result := adapter.SMA(prices, 5)
	assert.NotNil(t, result)
	assert.GreaterOrEqual(t, len(result), 1)
}

func TestTalibAdapter_RSI(t *testing.T) {
	adapter := NewTalibAdapter()
	// Generate some test prices
	prices := make([]decimal.Decimal, 30)
	for i := range prices {
		prices[i] = decimal.NewFromFloat(100 + float64(i)*0.5)
	}

	result := adapter.RSI(prices, 14)
	assert.NotNil(t, result)
	// RSI should produce some values after the period
	assert.GreaterOrEqual(t, len(result), 1)
}

func TestTalibAdapter_MACD(t *testing.T) {
	adapter := NewTalibAdapter()
	prices := make([]decimal.Decimal, 50)
	for i := range prices {
		prices[i] = decimal.NewFromFloat(100 + float64(i)*0.3)
	}

	macd, signal, histogram := adapter.MACD(prices, 12, 26, 9)
	assert.NotNil(t, macd)
	assert.NotNil(t, signal)
	assert.NotNil(t, histogram)
}

func TestTalibAdapter_BollingerBands(t *testing.T) {
	adapter := NewTalibAdapter()
	prices := make([]decimal.Decimal, 30)
	for i := range prices {
		prices[i] = decimal.NewFromFloat(100 + float64(i)*0.5)
	}

	upper, middle, lower := adapter.BollingerBands(prices, 20, 2.0)
	assert.NotNil(t, upper)
	assert.NotNil(t, middle)
	assert.NotNil(t, lower)
}
