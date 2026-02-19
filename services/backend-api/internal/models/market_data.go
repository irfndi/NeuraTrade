package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// MarketData contains real-time trading data for a specific trading pair on an exchange.
// It includes price, volume, and 24-hour statistics.
type MarketData struct {
	ID                  string          `json:"id" db:"id"`
	ExchangeID          int             `json:"exchange_id" db:"exchange_id"`
	TradingPairID       int             `json:"trading_pair_id" db:"trading_pair_id"`
	Bid                 decimal.Decimal `json:"bid" db:"bid"`
	BidVolume           decimal.Decimal `json:"bid_volume" db:"bid_volume"`
	Ask                 decimal.Decimal `json:"ask" db:"ask"`
	AskVolume           decimal.Decimal `json:"ask_volume" db:"ask_volume"`
	LastPrice           decimal.Decimal `json:"last_price" db:"last_price"`
	High24h             decimal.Decimal `json:"high_24h" db:"high_24h"`
	Low24h              decimal.Decimal `json:"low_24h" db:"low_24h"`
	Open24h             decimal.Decimal `json:"open_24h" db:"open_24h"`
	Close24h            decimal.Decimal `json:"close_24h" db:"close_24h"`
	Volume24h           decimal.Decimal `json:"volume_24h" db:"volume_24h"`
	QuoteVolume24h      decimal.Decimal `json:"quote_volume_24h" db:"quote_volume_24h"`
	Vwap                decimal.Decimal `json:"vwap" db:"vwap"`
	Change24h           decimal.Decimal `json:"change_24h" db:"change_24h"`
	ChangePercentage24h decimal.Decimal `json:"change_percentage_24h" db:"change_percentage_24h"`
	Timestamp           time.Time       `json:"timestamp" db:"timestamp"`
	CreatedAt           time.Time       `json:"created_at" db:"created_at"`
	Exchange            *Exchange       `json:"exchange,omitempty"`
	TradingPair         *TradingPair    `json:"trading_pair,omitempty"`
}

// MarketPrice represents a simplified view of the current market price for API responses.
//
// Note: BidVolume and AskVolume are set to zero when using ticker data from CCXT,
// as the ticker endpoint does not provide bid/ask volume information.
// Order book data would be required for accurate bid/ask volume values.
type MarketPrice struct {
	ExchangeID   int             `json:"exchange_id"`
	ExchangeName string          `json:"exchange_name"`
	Symbol       string          `json:"symbol"`
	Bid          decimal.Decimal `json:"bid"`
	BidVolume    decimal.Decimal `json:"bid_volume"` // May be zero when using ticker data
	Ask          decimal.Decimal `json:"ask"`
	AskVolume    decimal.Decimal `json:"ask_volume"` // May be zero when using ticker data
	Price        decimal.Decimal `json:"price"`      // Last traded price
	High24h      decimal.Decimal `json:"high_24h"`   // 24h high price
	Low24h       decimal.Decimal `json:"low_24h"`    // 24h low price
	Volume       decimal.Decimal `json:"volume"`
	Timestamp    time.Time       `json:"timestamp"`
}

// GetPrice returns the last traded price as a float64.
func (mp *MarketPrice) GetPrice() float64 {
	return mp.Price.InexactFloat64()
}

// GetVolume returns the trading volume as a float64.
func (mp *MarketPrice) GetVolume() float64 {
	return mp.Volume.InexactFloat64()
}

// GetTimestamp returns the time when the price data was recorded.
func (mp *MarketPrice) GetTimestamp() time.Time {
	return mp.Timestamp
}

// GetExchangeName returns the name of the exchange where the price originates.
func (mp *MarketPrice) GetExchangeName() string {
	return mp.ExchangeName
}

// GetSymbol returns the trading pair symbol.
func (mp *MarketPrice) GetSymbol() string {
	return mp.Symbol
}

// GetBid returns the best bid price as a float64.
func (mp *MarketPrice) GetBid() float64 {
	return mp.Bid.InexactFloat64()
}

// GetAsk returns the best ask price as a float64.
func (mp *MarketPrice) GetAsk() float64 {
	return mp.Ask.InexactFloat64()
}

// GetHigh returns the 24h high price as a float64.
func (mp *MarketPrice) GetHigh() float64 {
	return mp.High24h.InexactFloat64()
}

// GetLow returns the 24h low price as a float64.
func (mp *MarketPrice) GetLow() float64 {
	return mp.Low24h.InexactFloat64()
}

// TickerData represents real-time ticker information retrieved from the CCXT library.
type TickerData struct {
	Symbol    string          `json:"symbol"`
	Bid       decimal.Decimal `json:"bid"`
	Ask       decimal.Decimal `json:"ask"`
	Last      decimal.Decimal `json:"last"`
	High      decimal.Decimal `json:"high"`
	Low       decimal.Decimal `json:"low"`
	Volume    decimal.Decimal `json:"volume"`
	Timestamp time.Time       `json:"timestamp"`
}

// OrderBookData represents the order book structure (bids and asks) from CCXT.
type OrderBookData struct {
	Symbol    string              `json:"symbol"`
	Bids      [][]decimal.Decimal `json:"bids"`
	Asks      [][]decimal.Decimal `json:"asks"`
	Timestamp time.Time           `json:"timestamp"`
}

// MarketDataRequest represents the query parameters for fetching market data via API.
type MarketDataRequest struct {
	Symbols  []string `json:"symbols" form:"symbols"`
	Exchange string   `json:"exchange" form:"exchange"`
	Limit    int      `json:"limit" form:"limit"`
}
