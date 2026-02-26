package ccxt

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// UnixTimestamp is a custom type that can unmarshal Unix timestamps (in milliseconds).
type UnixTimestamp time.Time

// UnmarshalJSON implements json.Unmarshaler for UnixTimestamp.
// It handles timestamps in milliseconds.
func (ut *UnixTimestamp) UnmarshalJSON(data []byte) error {
	var timestamp int64
	if err := json.Unmarshal(data, &timestamp); err != nil {
		return fmt.Errorf("failed to unmarshal timestamp: %w", err)
	}
	// If timestamp is 0 or null, use current time instead of 0001-01-01
	if timestamp == 0 {
		*ut = UnixTimestamp(time.Now())
	} else {
		*ut = UnixTimestamp(time.Unix(timestamp/1000, (timestamp%1000)*1000000))
	}
	return nil
}

// MarshalJSON implements json.Marshaler for UnixTimestamp.
// It converts time.Time back to milliseconds timestamp.
func (ut UnixTimestamp) MarshalJSON() ([]byte, error) {
	t := time.Time(ut)
	return json.Marshal(t.Unix() * 1000)
}

// Time returns the underlying time.Time.
func (ut UnixTimestamp) Time() time.Time {
	return time.Time(ut)
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	// Status indicates service health (e.g., "ok").
	Status string `json:"status"`
	// Timestamp is the server time.
	Timestamp string `json:"timestamp"`
	// Service is the service name.
	Service string `json:"service"`
	// Version is the service version.
	Version string `json:"version"`
}

// ExchangeInfo represents information about a cryptocurrency exchange.
type ExchangeInfo struct {
	// ID is the exchange identifier.
	ID string `json:"id"`
	// Name is the human-readable name.
	Name string `json:"name"`
	// Countries is a list of countries where the exchange operates.
	Countries []string `json:"countries"`
	// URLs contains various exchange URLs (api, doc, etc.).
	URLs map[string]interface{} `json:"urls"`
}

// ExchangesResponse represents the response from the exchanges endpoint.
type ExchangesResponse struct {
	// Exchanges is the list of supported exchanges.
	Exchanges []ExchangeInfo `json:"exchanges"`
}

// Ticker represents ticker data from an exchange.
type Ticker struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Last is the last traded price.
	Last decimal.Decimal `json:"last"`
	// Bid is the best bid price.
	Bid decimal.Decimal `json:"bid"`
	// Ask is the best ask price.
	Ask decimal.Decimal `json:"ask"`
	// Volume is the 24h volume.
	Volume decimal.Decimal `json:"volume"`
	// High is the 24h high price.
	High decimal.Decimal `json:"high"`
	// Low is the 24h low price.
	Low decimal.Decimal `json:"low"`
	// Open is the opening price.
	Open decimal.Decimal `json:"open"`
	// Close is the closing price.
	Close decimal.Decimal `json:"close"`
	// Change is the price change.
	Change decimal.Decimal `json:"change"`
	// Percentage is the percentage change.
	Percentage decimal.Decimal `json:"percentage"`
	// Timestamp is the data timestamp.
	Timestamp UnixTimestamp `json:"timestamp"`
}

// TickerResponse represents the response from the ticker endpoint.
type TickerResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Ticker contains the ticker data.
	Ticker Ticker `json:"ticker"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// TickerData represents ticker data with exchange information.
type TickerData struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Ticker contains the ticker data.
	Ticker Ticker `json:"ticker"`
}

// TickersRequest represents the request for multiple tickers.
type TickersRequest struct {
	// Symbols is a list of symbols to fetch.
	Symbols []string `json:"symbols"`
	// Exchanges is a list of exchanges to query (optional).
	Exchanges []string `json:"exchanges,omitempty"`
}

// TickersResponse represents the response from the tickers endpoint.
type TickersResponse struct {
	// Tickers is the list of fetched tickers.
	Tickers []TickerData `json:"tickers"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// OrderBookEntry represents a single order book entry (price and amount).
type OrderBookEntry struct {
	// Price is the order price.
	Price decimal.Decimal `json:"price"`
	// Amount is the order amount.
	Amount decimal.Decimal `json:"amount"`
}

// OrderBook represents order book data.
type OrderBook struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Bids is the list of buy orders.
	Bids []OrderBookEntry `json:"bids"`
	// Asks is the list of sell orders.
	Asks []OrderBookEntry `json:"asks"`
	// Timestamp is the order book timestamp.
	Timestamp time.Time `json:"timestamp"`
	// Nonce is an incrementing integer for synchronization.
	Nonce int64 `json:"nonce"`
}

// OrderBookResponse represents the response from the order book endpoint.
type OrderBookResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// OrderBook contains the order book data.
	OrderBook OrderBook `json:"orderbook"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// OHLCV represents OHLCV (candlestick) data.
type OHLCV struct {
	// Timestamp is the candle open time.
	Timestamp time.Time `json:"timestamp"`
	// Open is the opening price.
	Open decimal.Decimal `json:"open"`
	// High is the highest price.
	High decimal.Decimal `json:"high"`
	// Low is the lowest price.
	Low decimal.Decimal `json:"low"`
	// Close is the closing price.
	Close decimal.Decimal `json:"close"`
	// Volume is the trading volume.
	Volume decimal.Decimal `json:"volume"`
}

// OHLCVResponse represents the response from the OHLCV endpoint.
type OHLCVResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Timeframe is the candle duration (e.g., "1m", "1h").
	Timeframe string `json:"timeframe"`
	// OHLCV is the list of candlestick data.
	OHLCV []OHLCV `json:"ohlcv"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// Trade represents a single trade.
type Trade struct {
	// ID is the unique trade identifier.
	ID string `json:"id"`
	// Timestamp is the trade time.
	Timestamp time.Time `json:"timestamp"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Side indicates 'buy' or 'sell'.
	Side string `json:"side"`
	// Amount is the trade amount.
	Amount decimal.Decimal `json:"amount"`
	// Price is the trade price.
	Price decimal.Decimal `json:"price"`
	// Cost is the total cost (price * amount).
	Cost decimal.Decimal `json:"cost"`
}

// TradesResponse represents the response from the trades endpoint.
type TradesResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Trades is the list of trades.
	Trades []Trade `json:"trades"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// MarketsResponse represents the response from the markets endpoint.
type MarketsResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Symbols is the list of supported trading pairs.
	Symbols []string `json:"symbols"`
	// Count is the number of symbols.
	Count int `json:"count"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// FundingRate represents funding rate data from an exchange.
type FundingRate struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// FundingRate is the rate (percentage or fraction).
	FundingRate float64 `json:"fundingRate"`
	// FundingTimestamp is the time of funding.
	FundingTimestamp UnixTimestamp `json:"fundingTimestamp"`
	// NextFundingTime is the next funding time.
	NextFundingTime UnixTimestamp `json:"nextFundingTime"`
	// MarkPrice is the mark price used for funding.
	MarkPrice float64 `json:"markPrice"`
	// IndexPrice is the index price.
	IndexPrice float64 `json:"indexPrice"`
	// Timestamp is the data timestamp.
	Timestamp UnixTimestamp `json:"timestamp"`
}

// FundingRateResponse represents the response from the funding rates endpoint.
type FundingRateResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// FundingRates is the list of funding rates.
	FundingRates []FundingRate `json:"fundingRates"`
	// Count is the number of rates.
	Count int `json:"count"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// FundingArbitrageOpportunity represents a funding rate arbitrage opportunity.
type FundingArbitrageOpportunity struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// LongExchange is the exchange to go long on.
	LongExchange string `json:"longExchange"`
	// ShortExchange is the exchange to go short on.
	ShortExchange string `json:"shortExchange"`
	// LongFundingRate is the funding rate on the long exchange.
	LongFundingRate float64 `json:"longFundingRate"`
	// ShortFundingRate is the funding rate on the short exchange.
	ShortFundingRate float64 `json:"shortFundingRate"`
	// NetFundingRate is the difference in funding rates.
	NetFundingRate float64 `json:"netFundingRate"`
	// EstimatedProfit8h is the estimated profit over 8 hours.
	EstimatedProfit8h float64 `json:"estimatedProfit8h"`
	// EstimatedProfitDaily is the estimated daily profit.
	EstimatedProfitDaily float64 `json:"estimatedProfitDaily"`
	// EstimatedProfitPercentage is the profit percentage.
	EstimatedProfitPercentage float64 `json:"estimatedProfitPercentage"`
	// LongMarkPrice is the mark price on the long exchange.
	LongMarkPrice float64 `json:"longMarkPrice"`
	// ShortMarkPrice is the mark price on the short exchange.
	ShortMarkPrice float64 `json:"shortMarkPrice"`
	// PriceDifference is the price difference between exchanges.
	PriceDifference float64 `json:"priceDifference"`
	// PriceDifferencePercentage is the price difference percentage.
	PriceDifferencePercentage float64 `json:"priceDifferencePercentage"`
	// RiskScore is the calculated risk score.
	RiskScore float64 `json:"riskScore"`
	// Timestamp is the calculation time.
	Timestamp UnixTimestamp `json:"timestamp"`
}

// ErrorResponse represents an error response from the CCXT service.
type ErrorResponse struct {
	// Error is the error code or short description.
	Error string `json:"error"`
	// Message is the detailed error message.
	Message string `json:"message,omitempty"`
	// Timestamp is the error time.
	Timestamp string `json:"timestamp"`
}

// ExchangeConfig represents the exchange configuration.
type ExchangeConfig struct {
	// BlacklistedExchanges is a list of blacklisted exchanges.
	BlacklistedExchanges []string `json:"blacklistedExchanges"`
	// PriorityExchanges is a list of priority exchanges.
	PriorityExchanges []string `json:"priorityExchanges"`
	// ExchangeConfigs contains specific configurations for exchanges.
	ExchangeConfigs map[string]interface{} `json:"exchangeConfigs"`
}

// ExchangeConfigResponse represents the response from the exchange config endpoint.
type ExchangeConfigResponse struct {
	// Config is the exchange configuration.
	Config ExchangeConfig `json:"config"`
	// ActiveExchanges is the list of active exchanges.
	ActiveExchanges []string `json:"activeExchanges"`
	// AvailableExchanges is the list of all available exchanges.
	AvailableExchanges []string `json:"availableExchanges"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// ExchangeManagementResponse represents the response from exchange management endpoints.
type ExchangeManagementResponse struct {
	// Message is the status message.
	Message string `json:"message"`
	// BlacklistedExchanges is the updated list of blacklisted exchanges.
	BlacklistedExchanges []string `json:"blacklistedExchanges,omitempty"`
	// ActiveExchanges is the updated list of active exchanges.
	ActiveExchanges []string `json:"activeExchanges,omitempty"`
	// AvailableExchanges is the updated list of available exchanges.
	AvailableExchanges []string `json:"availableExchanges,omitempty"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// Order represents an exchange order.
type Order struct {
	// ID is the order ID.
	ID string `json:"id"`
	// ClientOrderID is the client-specified order ID.
	ClientOrderID string `json:"clientOrderId"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Type is the order type (limit, market, etc.).
	Type string `json:"type"`
	// Side is buy or sell.
	Side string `json:"side"`
	// Status is the order status (open, closed, cancelled).
	Status string `json:"status"`
	// Price is the order price.
	Price decimal.Decimal `json:"price"`
	// Amount is the order amount.
	Amount decimal.Decimal `json:"amount"`
	// Filled is the filled amount.
	Filled decimal.Decimal `json:"filled"`
	// Remaining is the remaining amount.
	Remaining decimal.Decimal `json:"remaining"`
	// Cost is the total cost.
	Cost decimal.Decimal `json:"cost"`
	// CreatedAt is the creation time.
	CreatedAt time.Time `json:"datetime"`
	// Timestamp is the update time.
	Timestamp UnixTimestamp `json:"timestamp"`
}

// OpenOrdersResponse represents the response from fetching open orders.
type OpenOrdersResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Orders is the list of open orders.
	Orders []Order `json:"orders"`
	// Count is the number of orders.
	Count int `json:"count"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// OrderResponse represents the response from fetching a single order.
type OrderResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Order is the order data.
	Order Order `json:"order"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}

// Position represents a futures position.
type Position struct {
	// ID is the position ID.
	ID string `json:"id"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Side is long or short.
	Side string `json:"side"`
	// Size is the position size.
	Size decimal.Decimal `json:"contracts"`
	// EntryPrice is the entry price.
	EntryPrice decimal.Decimal `json:"entryPrice"`
	// MarkPrice is the current mark price.
	MarkPrice decimal.Decimal `json:"markPrice"`
	// UnrealizedPnl is the unrealized profit/loss.
	UnrealizedPnl decimal.Decimal `json:"unrealizedPnl"`
// Leverage is the position leverage.
	Leverage int `json:"leverage"`
	// LiquidationPrice is the liquidation price.
	LiquidationPrice decimal.Decimal `json:"liquidationPrice"`
	// MarginMode is cross or isolated.
	MarginMode string `json:"marginMode"`
	// Timestamp is the update time.
	Timestamp UnixTimestamp `json:"timestamp"`
}

// PositionsResponse represents the response from fetching positions.
type PositionsResponse struct {
	// Exchange is the exchange name.
	Exchange string `json:"exchange"`
	// Positions is the list of positions.
	Positions []Position `json:"positions"`
	// Count is the number of positions.
	Count int `json:"count"`
	// Timestamp is the response timestamp.
	Timestamp string `json:"timestamp"`
}
