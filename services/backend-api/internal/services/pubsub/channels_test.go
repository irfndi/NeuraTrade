package pubsub

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTickerChannel(t *testing.T) {
	assert.Equal(t, "market:ticker:binance:BTC/USDT", TickerChannel("binance", "BTC/USDT"))
	assert.Equal(t, "market:ticker:kraken:ETH/USD", TickerChannel("kraken", "ETH/USD"))
}

func TestOrderBookChannel(t *testing.T) {
	assert.Equal(t, "market:orderbook:binance:BTC/USDT", OrderBookChannel("binance", "BTC/USDT"))
}

func TestTradeChannel(t *testing.T) {
	assert.Equal(t, "market:trade:coinbase:SOL/USD", TradeChannel("coinbase", "SOL/USD"))
}

func TestSignalChannel(t *testing.T) {
	assert.Equal(t, "market:signal:aggregated", SignalChannel(QualifierAggregated))
	assert.Equal(t, "market:signal:technical", SignalChannel(QualifierTechnical))
	assert.Equal(t, "market:signal:arbitrage", SignalChannel(QualifierArbitrage))
	assert.Equal(t, "market:signal:risk", SignalChannel(QualifierRisk))
}

func TestFundingChannel(t *testing.T) {
	assert.Equal(t, "market:funding:binance:BTC/USDT", FundingChannel("binance", "BTC/USDT"))
}

func TestExchangeTickerChannel(t *testing.T) {
	assert.Equal(t, "market:ticker:binance:*", ExchangeTickerChannel("binance"))
}

func TestChannelConstants(t *testing.T) {
	assert.Equal(t, "market:ticker:*", ChannelAllTickers)
	assert.Equal(t, "market:orderbook:*", ChannelAllOrderBooks)
	assert.Equal(t, "market:trade:*", ChannelAllTrades)
	assert.Equal(t, "market:signal:*", ChannelAllSignals)
	assert.Equal(t, "market:funding:*", ChannelAllFunding)
}

func TestParseChannel(t *testing.T) {
	tests := []struct {
		name       string
		channel    string
		wantDomain string
		wantEntity string
		wantQuals  []string
	}{
		{
			name:       "ticker with exchange and symbol",
			channel:    "market:ticker:binance:BTC/USDT",
			wantDomain: "market",
			wantEntity: "ticker",
			wantQuals:  []string{"binance", "BTC/USDT"},
		},
		{
			name:       "signal with qualifier",
			channel:    "market:signal:aggregated",
			wantDomain: "market",
			wantEntity: "signal",
			wantQuals:  []string{"aggregated"},
		},
		{
			name:       "domain and entity only",
			channel:    "market:ticker",
			wantDomain: "market",
			wantEntity: "ticker",
			wantQuals:  nil,
		},
		{
			name:       "malformed single segment",
			channel:    "market",
			wantDomain: "",
			wantEntity: "",
			wantQuals:  nil,
		},
		{
			name:       "empty string",
			channel:    "",
			wantDomain: "",
			wantEntity: "",
			wantQuals:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, entity, quals := ParseChannel(tt.channel)
			assert.Equal(t, tt.wantDomain, domain)
			assert.Equal(t, tt.wantEntity, entity)
			assert.Equal(t, tt.wantQuals, quals)
		})
	}
}

func TestMessageTypeValues(t *testing.T) {
	assert.Equal(t, MessageType("ticker"), MessageTypeTicker)
	assert.Equal(t, MessageType("orderbook"), MessageTypeOrderBook)
	assert.Equal(t, MessageType("trade"), MessageTypeTrade)
	assert.Equal(t, MessageType("signal"), MessageTypeSignal)
	assert.Equal(t, MessageType("funding"), MessageTypeFunding)
}
