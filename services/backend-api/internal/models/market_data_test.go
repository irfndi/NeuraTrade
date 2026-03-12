package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMarketPrice_NilReceiverAccessors(t *testing.T) {
	var mp *MarketPrice

	assert.Equal(t, 0.0, mp.GetPrice())
	assert.Equal(t, 0.0, mp.GetVolume())
	assert.Equal(t, time.Time{}, mp.GetTimestamp())
	assert.Equal(t, "", mp.GetExchangeName())
	assert.Equal(t, "", mp.GetSymbol())
	assert.Equal(t, 0.0, mp.GetBid())
	assert.Equal(t, 0.0, mp.GetAsk())
	assert.Equal(t, 0.0, mp.GetHigh())
	assert.Equal(t, 0.0, mp.GetLow())
	assert.Equal(t, 0.0, mp.GetPriceChange24h())
}
