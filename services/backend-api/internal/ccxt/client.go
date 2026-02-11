package ccxt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	pb "github.com/irfandi/celebrum-ai-go/pkg/pb/ccxt"
	"github.com/shopspring/decimal"
)

// Client represents the CCXT HTTP client.
type Client struct {
	HTTPClient *http.Client
	baseURL    string
	grpcClient pb.CcxtServiceClient
	grpcConn   *grpc.ClientConn
	timeout    time.Duration
}

// NewClient creates a new CCXT client instance.
//
// Parameters:
//
//	cfg: CCXT configuration.
//
// Returns:
//
//	*Client: Initialized client.
func NewClient(cfg *config.CCXTConfig) *Client {
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Log the actual service URL being used
	log.Printf("CCXT Service URL from config: %s", cfg.ServiceURL)

	client := &Client{
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
		baseURL: strings.TrimSuffix(cfg.ServiceURL, "/"),
		timeout: timeout,
	}

	if cfg.GrpcAddress != "" {
		// Use insecure credentials for internal communication
		conn, err := grpc.NewClient(cfg.GrpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Failed to connect to CCXT gRPC service at %s: %v", cfg.GrpcAddress, err)
		} else {
			client.grpcClient = pb.NewCcxtServiceClient(conn)
			client.grpcConn = conn
			log.Printf("Connected to CCXT gRPC service at %s", cfg.GrpcAddress)
		}
	}

	log.Printf("DEBUG: CCXT Client initialized with BaseURL: %s", client.BaseURL())
	return client
}

// HealthCheck checks if the CCXT service is healthy.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	*HealthResponse: Health status.
//	error: Error if check fails.
func (c *Client) HealthCheck(ctx context.Context) (*HealthResponse, error) {
	var response HealthResponse
	err := c.makeRequest(ctx, "GET", "/health", nil, &response)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

// GetExchanges retrieves all supported exchanges.
func (c *Client) GetExchanges(ctx context.Context) (*ExchangesResponse, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetExchanges(ctx, &pb.GetExchangesRequest{})
		if err == nil && resp.Error == "" {
			return c.convertGrpcExchangesResponse(resp), nil
		}
		if err != nil {
			log.Printf("Failed to get exchanges via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	var response ExchangesResponse
	err := c.makeRequest(ctx, "GET", "/api/exchanges", nil, &response)
	return &response, err
}

// GetTicker retrieves ticker data for a specific exchange and symbol.
func (c *Client) GetTicker(ctx context.Context, exchange, symbol string) (*TickerResponse, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetTicker(ctx, &pb.GetTickerRequest{
			Exchange: exchange,
			Symbol:   symbol,
		})
		if err == nil && resp.Error == "" {
			return c.convertGrpcTickerResponse(resp), nil
		}
		if err != nil {
			log.Printf("Failed to get ticker via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	// Convert symbol format based on exchange requirements
	ccxtSymbol := c.formatSymbolForExchange(exchange, symbol)
	path := fmt.Sprintf("/api/ticker/%s/%s", exchange, ccxtSymbol)
	var response TickerResponse
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	return &response, err
}

// GetTickers retrieves multiple tickers in a single request.
func (c *Client) GetTickers(ctx context.Context, req *TickersRequest) (*TickersResponse, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetTickers(ctx, &pb.GetTickersRequest{
			Symbols:   req.Symbols,
			Exchanges: req.Exchanges,
		})
		if err == nil && resp.Error == "" {
			return c.convertGrpcTickersResponse(resp), nil
		}
		if err != nil {
			log.Printf("Failed to get tickers via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	var response TickersResponse
	err := c.makeRequest(ctx, "POST", "/api/tickers", req, &response)
	return &response, err
}

// GetOrderBook retrieves order book data for a specific exchange and symbol.
func (c *Client) GetOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetOrderBook(ctx, &pb.GetOrderBookRequest{
			Exchange: exchange,
			Symbol:   symbol,
			Limit:    int32(limit),
		})
		if err == nil && resp.Error == "" {
			return c.convertGrpcOrderBookResponse(resp), nil
		}
		if err != nil {
			log.Printf("Failed to get orderbook via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	// Convert symbol format based on exchange requirements
	ccxtSymbol := c.formatSymbolForExchange(exchange, symbol)
	path := fmt.Sprintf("/api/orderbook/%s/%s", exchange, ccxtSymbol)
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var response OrderBookResponse
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	return &response, err
}

// GetTrades retrieves recent trades for a specific exchange and symbol.
func (c *Client) GetTrades(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetTrades(ctx, &pb.GetTradesRequest{
			Exchange: exchange,
			Symbol:   symbol,
			Limit:    int32(limit),
		})
		if err == nil && resp.Error == "" {
			return c.convertGrpcTradesResponse(resp), nil
		}
		if err != nil {
			log.Printf("Failed to get trades via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	// Convert symbol format based on exchange requirements
	ccxtSymbol := c.formatSymbolForExchange(exchange, symbol)
	path := fmt.Sprintf("/api/trades/%s/%s", exchange, ccxtSymbol)
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	var response TradesResponse
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	return &response, err
}

// GetOHLCV retrieves OHLCV data for a specific exchange and symbol.
func (c *Client) GetOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetOHLCV(ctx, &pb.GetOHLCVRequest{
			Exchange:  exchange,
			Symbol:    symbol,
			Timeframe: timeframe,
			Limit:     int32(limit),
		})
		if err == nil && resp.Error == "" {
			return c.convertGrpcOHLCVResponse(resp), nil
		}
		if err != nil {
			log.Printf("Failed to get OHLCV via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	// Convert symbol format based on exchange requirements
	ccxtSymbol := c.formatSymbolForExchange(exchange, symbol)
	path := fmt.Sprintf("/api/ohlcv/%s/%s", exchange, ccxtSymbol)
	params := url.Values{}
	if timeframe != "" {
		params.Set("timeframe", timeframe)
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var response OHLCVResponse
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	return &response, err
}

// GetMarkets retrieves all trading pairs for a specific exchange.
func (c *Client) GetMarkets(ctx context.Context, exchange string) (*MarketsResponse, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetMarkets(ctx, &pb.GetMarketsRequest{
			Exchange: exchange,
		})
		if err == nil && resp.Error == "" {
			return c.convertGrpcMarketsResponse(resp), nil
		}
		if err != nil {
			log.Printf("Failed to get markets via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	path := fmt.Sprintf("/api/markets/%s", exchange)
	var response MarketsResponse
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	return &response, err
}

// GetFundingRate retrieves funding rate for a specific symbol on an exchange.
func (c *Client) GetFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error) {
	// Try gRPC first for multiple/single rates
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetFundingRates(ctx, &pb.GetFundingRatesRequest{
			Exchange: exchange,
			Symbols:  []string{symbol},
		})
		if err == nil && resp.Error == "" && len(resp.Rates) > 0 {
			return c.convertGrpcFundingRate(resp.Rates[0]), nil
		}
		if err != nil {
			log.Printf("Failed to get funding rate via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	ccxtSymbol := c.formatSymbolForExchange(exchange, symbol)
	path := fmt.Sprintf("/api/funding-rate/%s/%s", exchange, ccxtSymbol)
	var response FundingRate
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	return &response, err
}

// GetFundingRates retrieves funding rates for multiple symbols on an exchange.
func (c *Client) GetFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetFundingRates(ctx, &pb.GetFundingRatesRequest{
			Exchange: exchange,
			Symbols:  symbols,
		})
		if err == nil && resp.Error == "" {
			rates := make([]FundingRate, len(resp.Rates))
			for i, r := range resp.Rates {
				rates[i] = *c.convertGrpcFundingRate(r)
			}
			return rates, nil
		}
		if err != nil {
			log.Printf("Failed to get funding rates via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	if len(symbols) == 0 {
		return []FundingRate{}, nil
	}

	// Format symbols
	formattedSymbols := make([]string, len(symbols))
	for i, symbol := range symbols {
		formattedSymbols[i] = c.formatSymbolForExchange(exchange, symbol)
	}

	// Join symbols with comma
	symbolsParam := strings.Join(formattedSymbols, ",")
	path := fmt.Sprintf("/api/funding-rates/%s?symbols=%s", exchange, url.QueryEscape(symbolsParam))

	var response FundingRateResponse
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	if err != nil {
		return nil, err
	}
	return response.FundingRates, nil
}

// GetAllFundingRates retrieves all available funding rates for an exchange.
func (c *Client) GetAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error) {
	// Try gRPC first
	if c.grpcClient != nil {
		resp, err := c.grpcClient.GetFundingRates(ctx, &pb.GetFundingRatesRequest{
			Exchange: exchange,
		})
		if err == nil && resp.Error == "" {
			rates := make([]FundingRate, len(resp.Rates))
			for i, r := range resp.Rates {
				rates[i] = *c.convertGrpcFundingRate(r)
			}
			return rates, nil
		}
		if err != nil {
			log.Printf("Failed to get all funding rates via gRPC: %v, falling back to HTTP", err)
		} else if resp.Error != "" {
			log.Printf("CCXT gRPC service returned error: %s, falling back to HTTP", resp.Error)
		}
	}

	path := fmt.Sprintf("/api/funding-rates/%s", exchange)
	var response FundingRateResponse
	err := c.makeRequest(ctx, "GET", path, nil, &response)
	if err != nil {
		return nil, err
	}
	return response.FundingRates, nil
}

// Exchange Management Methods

// GetExchangeConfig retrieves the current exchange configuration.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	*ExchangeConfigResponse: Exchange configuration.
//	error: Error if retrieval fails.
func (c *Client) GetExchangeConfig(ctx context.Context) (*ExchangeConfigResponse, error) {
	var response ExchangeConfigResponse
	err := c.makeRequest(ctx, "GET", "/api/admin/exchanges/config", nil, &response)
	return &response, err
}

// AddExchangeToBlacklist adds an exchange to the blacklist.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (c *Client) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	path := fmt.Sprintf("/api/admin/exchanges/blacklist/%s", exchange)
	var response ExchangeManagementResponse
	err := c.makeRequest(ctx, "POST", path, nil, &response)
	return &response, err
}

// RemoveExchangeFromBlacklist removes an exchange from the blacklist.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (c *Client) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	path := fmt.Sprintf("/api/admin/exchanges/blacklist/%s", exchange)
	var response ExchangeManagementResponse
	err := c.makeRequest(ctx, "DELETE", path, nil, &response)
	return &response, err
}

// RefreshExchanges refreshes all exchanges (re-initializes non-blacklisted exchanges).
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (c *Client) RefreshExchanges(ctx context.Context) (*ExchangeManagementResponse, error) {
	var response ExchangeManagementResponse
	err := c.makeRequest(ctx, "POST", "/api/admin/exchanges/refresh", nil, &response)
	return &response, err
}

// AddExchange dynamically adds a new exchange.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (c *Client) AddExchange(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	path := fmt.Sprintf("/api/admin/exchanges/add/%s", exchange)
	var response ExchangeManagementResponse
	err := c.makeRequest(ctx, "POST", path, nil, &response)
	return &response, err
}

// formatSymbolForExchange formats the symbol based on exchange requirements
func (c *Client) formatSymbolForExchange(exchange, symbol string) string {
	switch strings.ToLower(exchange) {
	case "kraken", "okx":
		// Kraken and OKX use URL-encoded slash format
		return url.QueryEscape(symbol)
	case "coinbase", "coinbasepro":
		// Coinbase uses dash format (BTC-USDT)
		return strings.ReplaceAll(symbol, "/", "-")
	default:
		// Most exchanges (like Binance) use concatenated format
		return strings.ReplaceAll(symbol, "/", "")
	}
}

// makeRequest is a helper method to make HTTP requests to the CCXT service
func (c *Client) makeRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NeuraTrade/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errorResp ErrorResponse
		if err := json.Unmarshal(respBody, &errorResp); err == nil {
			return fmt.Errorf("CCXT service error (%d): %s", resp.StatusCode, errorResp.Error)
		}
		return fmt.Errorf("CCXT service error (%d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// Close closes the HTTP client (if needed for cleanup).
//
// Returns:
//
//	error: Always nil.
func (c *Client) Close() error {
	// HTTP client doesn't need explicit closing, but this method
	// is provided for interface compatibility
	return nil
}

// BaseURL returns the base URL of the CCXT service.
//
// Returns:
//
//	string: The base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// decimalFromString safely converts a string to decimal.Decimal.
// Returns decimal.Zero if the string is empty or invalid.
func decimalFromString(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		log.Printf("Warning: failed to parse decimal from string '%s': %v", s, err)
		return decimal.Zero
	}
	return d
}

// convertGrpcTickerResponse converts a gRPC ticker response to the internal TickerResponse struct.
func (c *Client) convertGrpcTickerResponse(resp *pb.GetTickerResponse) *TickerResponse {
	if resp == nil || resp.Ticker == nil {
		return nil
	}

	return &TickerResponse{
		Exchange:  resp.Exchange,
		Symbol:    resp.Symbol,
		Timestamp: fmt.Sprintf("%d", resp.Timestamp),
		Ticker: Ticker{
			Symbol:    resp.Symbol,
			Last:      decimalFromString(resp.Ticker.Last),
			High:      decimalFromString(resp.Ticker.High),
			Low:       decimalFromString(resp.Ticker.Low),
			Bid:       decimalFromString(resp.Ticker.Bid),
			Ask:       decimalFromString(resp.Ticker.Ask),
			Volume:    decimalFromString(resp.Ticker.BaseVolume),
			Timestamp: UnixTimestamp(time.UnixMilli(resp.Timestamp)),
		},
	}
}

// convertGrpcTickersResponse converts a gRPC tickers response to the internal TickersResponse struct.
func (c *Client) convertGrpcTickersResponse(resp *pb.GetTickersResponse) *TickersResponse {
	if resp == nil {
		return nil
	}

	tickers := make([]TickerData, len(resp.Tickers))
	for i, t := range resp.Tickers {
		if t.Ticker == nil {
			continue
		}
		tickers[i] = TickerData{
			Exchange: t.Exchange,
			Ticker: Ticker{
				Symbol:    t.Symbol,
				Last:      decimalFromString(t.Ticker.Last),
				High:      decimalFromString(t.Ticker.High),
				Low:       decimalFromString(t.Ticker.Low),
				Bid:       decimalFromString(t.Ticker.Bid),
				Ask:       decimalFromString(t.Ticker.Ask),
				Volume:    decimalFromString(t.Ticker.BaseVolume),
				Timestamp: UnixTimestamp(time.UnixMilli(t.Timestamp)),
			},
		}
	}

	return &TickersResponse{
		Tickers:   tickers,
		Timestamp: fmt.Sprintf("%d", time.Now().UnixMilli()), // Use current time or from proto if available (not in wrapper)
	}
}

// convertGrpcTradesResponse converts a gRPC trades response to the internal TradesResponse struct.
func (c *Client) convertGrpcTradesResponse(resp *pb.GetTradesResponse) *TradesResponse {
	if resp == nil {
		return nil
	}

	trades := make([]Trade, len(resp.Trades))
	for i, t := range resp.Trades {
		trades[i] = Trade{
			ID:        t.Id,
			Timestamp: time.UnixMilli(t.Timestamp),
			Symbol:    t.Symbol,
			Side:      t.Side,
			Amount:    decimalFromString(t.Amount),
			Price:     decimalFromString(t.Price),
			Cost:      decimalFromString(t.Cost),
		}
	}

	return &TradesResponse{
		Exchange:  resp.Exchange,
		Symbol:    resp.Symbol,
		Trades:    trades,
		Timestamp: fmt.Sprintf("%d", resp.Timestamp),
	}
}

// convertGrpcExchangesResponse converts a gRPC exchanges response to the internal ExchangesResponse struct.
func (c *Client) convertGrpcExchangesResponse(resp *pb.GetExchangesResponse) *ExchangesResponse {
	if resp == nil {
		return nil
	}

	exchanges := make([]ExchangeInfo, len(resp.Exchanges))
	for i, e := range resp.Exchanges {
		exchanges[i] = ExchangeInfo{
			ID:        e.Id,
			Name:      e.Name,
			Countries: e.Countries,
			URLs:      make(map[string]interface{}), // URLs not in proto yet?
		}
	}

	return &ExchangesResponse{
		Exchanges: exchanges,
	}
}

// convertGrpcOrderBookResponse converts a gRPC order book response to the internal OrderBookResponse struct.
func (c *Client) convertGrpcOrderBookResponse(resp *pb.GetOrderBookResponse) *OrderBookResponse {
	if resp == nil || resp.Orderbook == nil {
		return nil
	}

	bids := make([]OrderBookEntry, len(resp.Orderbook.Bids))
	for i, b := range resp.Orderbook.Bids {
		bids[i] = OrderBookEntry{
			Price:  decimalFromString(b.Price),
			Amount: decimalFromString(b.Amount),
		}
	}

	asks := make([]OrderBookEntry, len(resp.Orderbook.Asks))
	for i, a := range resp.Orderbook.Asks {
		asks[i] = OrderBookEntry{
			Price:  decimalFromString(a.Price),
			Amount: decimalFromString(a.Amount),
		}
	}

	return &OrderBookResponse{
		Exchange: resp.Exchange,
		Symbol:   resp.Symbol,
		OrderBook: OrderBook{
			Symbol:    resp.Symbol,
			Bids:      bids,
			Asks:      asks,
			Timestamp: time.UnixMilli(resp.Orderbook.Timestamp),
			Nonce:     resp.Orderbook.Nonce,
		},
		Timestamp: fmt.Sprintf("%d", resp.Timestamp),
	}
}

// convertGrpcOHLCVResponse converts a gRPC OHLCV response to the internal OHLCVResponse struct.
func (c *Client) convertGrpcOHLCVResponse(resp *pb.GetOHLCVResponse) *OHLCVResponse {
	if resp == nil {
		return nil
	}

	ohlcv := make([]OHLCV, len(resp.Candles))
	for i, candle := range resp.Candles {
		ohlcv[i] = OHLCV{
			Timestamp: time.UnixMilli(candle.Timestamp),
			Open:      decimalFromString(candle.Open),
			High:      decimalFromString(candle.High),
			Low:       decimalFromString(candle.Low),
			Close:     decimalFromString(candle.Close),
			Volume:    decimalFromString(candle.Volume),
		}
	}

	return &OHLCVResponse{
		Exchange:  resp.Exchange,
		Symbol:    resp.Symbol,
		Timeframe: resp.Timeframe,
		OHLCV:     ohlcv,
		Timestamp: fmt.Sprintf("%d", resp.Timestamp),
	}
}

// convertGrpcMarketsResponse converts a gRPC markets response to the internal MarketsResponse struct.
func (c *Client) convertGrpcMarketsResponse(resp *pb.GetMarketsResponse) *MarketsResponse {
	if resp == nil {
		return nil
	}

	return &MarketsResponse{
		Exchange:  resp.Exchange,
		Symbols:   resp.Symbols,
		Count:     int(resp.Count),
		Timestamp: fmt.Sprintf("%d", time.Now().UnixMilli()), // Proto doesn't have timestamp?
	}
}

// float64FromString safely converts a string to float64.
// Returns 0.0 if the string is empty or invalid.
func float64FromString(s string) float64 {
	if s == "" {
		return 0.0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Printf("Warning: failed to parse float64 from string '%s': %v", s, err)
		return 0.0
	}
	return f
}

// convertGrpcFundingRate converts a gRPC funding rate to the internal FundingRate struct.
func (c *Client) convertGrpcFundingRate(r *pb.FundingRate) *FundingRate {
	if r == nil {
		return nil
	}

	return &FundingRate{
		Symbol:           r.Symbol,
		FundingRate:      float64FromString(r.FundingRate),
		FundingTimestamp: UnixTimestamp(time.UnixMilli(r.Timestamp)),
		NextFundingTime:  UnixTimestamp(time.UnixMilli(r.NextFundingTime)),
		MarkPrice:        float64FromString(r.MarkPrice),
		IndexPrice:       float64FromString(r.IndexPrice),
		Timestamp:        UnixTimestamp(time.UnixMilli(r.Timestamp)),
	}
}
