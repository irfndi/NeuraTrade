// Package pubsub provides typed Redis pub/sub messaging for market data.
//
// Channel naming convention: {domain}:{entity}:{qualifier}
// Examples: market:ticker:binance:BTC/USDT, market:signal:aggregated
package pubsub

import (
	"fmt"
	"strings"
	"time"
)

const (
	DomainMarket = "market"
)

const (
	EntityTicker    = "ticker"
	EntityOrderBook = "orderbook"
	EntityTrade     = "trade"
	EntitySignal    = "signal"
	EntityFunding   = "funding"
)

const (
	QualifierAggregated = "aggregated"
	QualifierTechnical  = "technical"
	QualifierArbitrage  = "arbitrage"
	QualifierRisk       = "risk"
)

const (
	ChannelAllTickers    = DomainMarket + ":" + EntityTicker + ":*"
	ChannelAllOrderBooks = DomainMarket + ":" + EntityOrderBook + ":*"
	ChannelAllTrades     = DomainMarket + ":" + EntityTrade + ":*"
	ChannelAllSignals    = DomainMarket + ":" + EntitySignal + ":*"
	ChannelAllFunding    = DomainMarket + ":" + EntityFunding + ":*"
)

func TickerChannel(exchange, symbol string) string {
	return fmt.Sprintf("%s:%s:%s:%s", DomainMarket, EntityTicker, exchange, symbol)
}

func OrderBookChannel(exchange, symbol string) string {
	return fmt.Sprintf("%s:%s:%s:%s", DomainMarket, EntityOrderBook, exchange, symbol)
}

func TradeChannel(exchange, symbol string) string {
	return fmt.Sprintf("%s:%s:%s:%s", DomainMarket, EntityTrade, exchange, symbol)
}

func SignalChannel(qualifier string) string {
	return fmt.Sprintf("%s:%s:%s", DomainMarket, EntitySignal, qualifier)
}

func FundingChannel(exchange, symbol string) string {
	return fmt.Sprintf("%s:%s:%s:%s", DomainMarket, EntityFunding, exchange, symbol)
}

func ExchangeTickerChannel(exchange string) string {
	return fmt.Sprintf("%s:%s:%s:*", DomainMarket, EntityTicker, exchange)
}

// ParseChannel extracts domain, entity, and qualifiers from a channel name.
// Channel format is {domain}:{entity}[:{q1}:{q2}:...].
func ParseChannel(channel string) (domain, entity string, qualifiers []string) {
	parts := strings.SplitN(channel, ":", 3)
	if len(parts) < 2 {
		return "", "", nil
	}
	domain = parts[0]
	entity = parts[1]
	if len(parts) == 3 {
		qualifiers = strings.Split(parts[2], ":")
	}
	return domain, entity, qualifiers
}

type MessageType string

const (
	MessageTypeTicker    MessageType = "ticker"
	MessageTypeOrderBook MessageType = "orderbook"
	MessageTypeTrade     MessageType = "trade"
	MessageTypeSignal    MessageType = "signal"
	MessageTypeFunding   MessageType = "funding"
)

type Envelope struct {
	Type      MessageType `json:"type"`
	Channel   string      `json:"channel"`
	Exchange  string      `json:"exchange,omitempty"`
	Symbol    string      `json:"symbol,omitempty"`
	Data      []byte      `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
	TraceID   string      `json:"trace_id,omitempty"`
}

type TickerPayload struct {
	Bid       string `json:"bid"`
	Ask       string `json:"ask"`
	Last      string `json:"last"`
	Volume    string `json:"volume"`
	High24h   string `json:"high_24h,omitempty"`
	Low24h    string `json:"low_24h,omitempty"`
	Change24h string `json:"change_24h,omitempty"`
}

type OrderBookPayload struct {
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
	Timestamp time.Time    `json:"timestamp"`
}

type PriceLevel struct {
	Price  string `json:"price"`
	Amount string `json:"amount"`
}

type TradePayload struct {
	ID        string `json:"id,omitempty"`
	Price     string `json:"price"`
	Amount    string `json:"amount"`
	Side      string `json:"side"` // buy | sell
	Timestamp int64  `json:"timestamp"`
}

type SignalPayload struct {
	SignalType string                 `json:"signal_type"`
	Direction  string                 `json:"direction"` // long | short | neutral
	Strength   float64                `json:"strength"`
	Confidence float64                `json:"confidence"`
	Source     string                 `json:"source"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type FundingPayload struct {
	Rate          string `json:"rate"`
	NextFundingAt int64  `json:"next_funding_at"`
	PredictedRate string `json:"predicted_rate,omitempty"`
	IntervalHours int    `json:"interval_hours"`
}
