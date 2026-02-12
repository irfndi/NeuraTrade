package interfaces

import (
	"time"

	"github.com/shopspring/decimal"
)

// MarketPrice represents the current market price of a symbol on a specific exchange.
// It includes price, volume, and timestamp information.
type MarketPrice struct {
	// ExchangeID is the internal identifier for the exchange.
	ExchangeID int `json:"exchange_id"`
	// ExchangeName is the string name of the exchange (e.g., "binance").
	ExchangeName string `json:"exchange_name"`
	// Symbol is the trading pair symbol (e.g., "BTC/USD").
	Symbol string `json:"symbol"`
	// Price is the current price of the asset.
	Price decimal.Decimal `json:"price"`
	// Bid is the best bid price.
	Bid decimal.Decimal `json:"bid"`
	// Ask is the best ask price.
	Ask decimal.Decimal `json:"ask"`
	// Volume is the 24-hour trading volume.
	Volume decimal.Decimal `json:"volume"`
	// Timestamp is the time at which this price data was recorded.
	Timestamp time.Time `json:"timestamp"`
}

// MarketPriceInterface defines the contract for accessing market price data.
type MarketPriceInterface interface {
	// GetPrice retrieves the price of the asset as a float64.
	//
	// Returns:
	//   float64: The price.
	GetPrice() float64

	// GetVolume retrieves the trading volume of the asset as a float64.
	//
	// Returns:
	//   float64: The volume.
	GetVolume() float64

	// GetTimestamp retrieves the time when the price data was recorded.
	//
	// Returns:
	//   time.Time: The timestamp.
	GetTimestamp() time.Time

	// GetExchangeName retrieves the name of the exchange.
	//
	// Returns:
	//   string: The exchange name.
	GetExchangeName() string

	// GetSymbol retrieves the trading pair symbol.
	//
	// Returns:
	//   string: The symbol.
	GetSymbol() string

	// GetBid retrieves the best bid price as a float64.
	//
	// Returns:
	//   float64: The bid price.
	GetBid() float64

	// GetAsk retrieves the best ask price as a float64.
	//
	// Returns:
	//   float64: The ask price.
	GetAsk() float64
}

// GetPrice returns the price of the asset as a float64.
// Note: This converts the underlying decimal.Decimal to a float64, which may involve precision loss.
//
// Returns:
//
//	float64: The price value.
func (mp *MarketPrice) GetPrice() float64 {
	return mp.Price.InexactFloat64()
}

// GetVolume returns the trading volume of the asset as a float64.
// Note: This converts the underlying decimal.Decimal to a float64, which may involve precision loss.
//
// Returns:
//
//	float64: The volume value.
func (mp *MarketPrice) GetVolume() float64 {
	return mp.Volume.InexactFloat64()
}

// GetTimestamp returns the timestamp when the price was recorded.
//
// Returns:
//
//	time.Time: The recording timestamp.
func (mp *MarketPrice) GetTimestamp() time.Time {
	return mp.Timestamp
}

// GetExchangeName returns the name of the exchange providing this price.
//
// Returns:
//
//	string: The exchange name.
func (mp *MarketPrice) GetExchangeName() string {
	return mp.ExchangeName
}

// GetSymbol returns the trading pair symbol for this price data.
//
// Returns:
//
//	string: The symbol (e.g., "BTC/USDT").
func (mp *MarketPrice) GetSymbol() string {
	return mp.Symbol
}

// GetBid returns the best bid price as a float64.
// Note: This converts the underlying decimal.Decimal to a float64, which may involve precision loss.
//
// Returns:
//
//	float64: The bid price value.
func (mp *MarketPrice) GetBid() float64 {
	return mp.Bid.InexactFloat64()
}

// GetAsk returns the best ask price as a float64.
// Note: This converts the underlying decimal.Decimal to a float64, which may involve precision loss.
//
// Returns:
//
//	float64: The ask price value.
func (mp *MarketPrice) GetAsk() float64 {
	return mp.Ask.InexactFloat64()
}
