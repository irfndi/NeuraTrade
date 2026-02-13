package ccxt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/irfndi/neuratrade/internal/config"
	pb "github.com/irfndi/neuratrade/pkg/pb/ccxt"
	"github.com/shopspring/decimal"
)

// SymbolNotFoundError represents an error when a symbol is not found on an exchange.
// This error should not be retried as the symbol doesn't exist.
type SymbolNotFoundError struct {
	Exchange string
	Symbol   string
	Message  string
}

func (e *SymbolNotFoundError) Error() string {
	return fmt.Sprintf("symbol %s not found on %s: %s", e.Symbol, e.Exchange, e.Message)
}

// IsSymbolNotFoundError returns true if the error is a symbol not found error.
// Uses errors.As to correctly handle wrapped errors.
func IsSymbolNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var symbolErr *SymbolNotFoundError
	return errors.As(err, &symbolErr)
}

// ExchangeUnavailableError represents an error when an exchange is temporarily unavailable.
// This error can potentially be retried.
type ExchangeUnavailableError struct {
	Exchange string
	Message  string
}

func (e *ExchangeUnavailableError) Error() string {
	return fmt.Sprintf("exchange %s unavailable: %s", e.Exchange, e.Message)
}

// IsExchangeUnavailableError returns true if the error is an exchange unavailable error.
// Uses errors.As to correctly handle wrapped errors.
func IsExchangeUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	var exchangeErr *ExchangeUnavailableError
	return errors.As(err, &exchangeErr)
}

// UnsupportedOperationError represents an error when an exchange doesn't support a specific operation.
// This error should not be retried as the operation is not supported.
type UnsupportedOperationError struct {
	Exchange  string
	Operation string
	Message   string
}

func (e *UnsupportedOperationError) Error() string {
	return fmt.Sprintf("exchange %s does not support %s: %s", e.Exchange, e.Operation, e.Message)
}

// IsUnsupportedOperationError returns true if the error is an unsupported operation error.
// Uses errors.As to correctly handle wrapped errors.
func IsUnsupportedOperationError(err error) bool {
	if err == nil {
		return false
	}
	var opErr *UnsupportedOperationError
	return errors.As(err, &opErr)
}

// GRPCConnectionError represents a gRPC connection failure.
// This error indicates gRPC is unavailable but HTTP fallback should be attempted.
type GRPCConnectionError struct {
	Address string
	Message string
}

func (e *GRPCConnectionError) Error() string {
	return fmt.Sprintf("gRPC connection failed to %s: %s", e.Address, e.Message)
}

// IsGRPCConnectionError returns true if the error is a gRPC connection error.
// Uses errors.As to correctly handle wrapped errors.
func IsGRPCConnectionError(err error) bool {
	if err == nil {
		return false
	}
	var grpcErr *GRPCConnectionError
	return errors.As(err, &grpcErr)
}

// Client represents the CCXT HTTP client.
type Client struct {
	HTTPClient  *http.Client
	baseURL     string
	grpcClient  pb.CcxtServiceClient
	grpcConn    *grpc.ClientConn
	grpcAddress string
	grpcEnabled bool
	timeout     time.Duration
	adminAPIKey string
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
		baseURL:     strings.TrimSuffix(cfg.ServiceURL, "/"),
		grpcAddress: cfg.GrpcAddress,
		grpcEnabled: false,
		timeout:     timeout,
		adminAPIKey: cfg.AdminAPIKey,
	}

	if cfg.GrpcAddress != "" {
		// Use insecure credentials for internal communication with connection timeout
		// Use non-blocking dial to avoid startup delays when gRPC service is unavailable
		conn, err := grpc.NewClient(
			cfg.GrpcAddress,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Printf("Failed to create CCXT gRPC client at %s: %v (HTTP fallback available)", cfg.GrpcAddress, err)
		} else {
			client.grpcClient = pb.NewCcxtServiceClient(conn)
			client.grpcConn = conn
			client.grpcEnabled = true
			log.Printf("Created CCXT gRPC client for %s (connection will be established on first use)", cfg.GrpcAddress)
		}
	}

	log.Printf("DEBUG: CCXT Client initialized with BaseURL: %s, gRPC: %v", client.BaseURL(), client.grpcEnabled)
	return client
}

// IsGRPCEnabled returns whether the gRPC client is configured and enabled.
func (c *Client) IsGRPCEnabled() bool {
	return c.grpcEnabled && c.grpcClient != nil
}

// DisableGRPC disables gRPC and forces HTTP-only mode.
// This is useful when gRPC is consistently failing.
func (c *Client) DisableGRPC() {
	c.grpcEnabled = false
	log.Printf("CCXT gRPC disabled, using HTTP-only mode")
}

// EnableGRPC re-enables gRPC if the client was initialized with gRPC support.
func (c *Client) EnableGRPC() {
	if c.IsGRPCEnabled() {
		c.grpcEnabled = true
		log.Printf("CCXT gRPC re-enabled")
	}
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
	// Try gRPC first if enabled
	if c.IsGRPCEnabled() {
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
	// Try gRPC first if enabled
	if c.IsGRPCEnabled() {
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
	// Try gRPC first if enabled
	if c.IsGRPCEnabled() {
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
	if c.IsGRPCEnabled() {
		resp, err := c.grpcClient.GetOrderBook(ctx, &pb.GetOrderBookRequest{
			Exchange: exchange,
			Symbol:   symbol,
			Limit:    toSafeInt32(limit),
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
	if c.IsGRPCEnabled() {
		resp, err := c.grpcClient.GetTrades(ctx, &pb.GetTradesRequest{
			Exchange: exchange,
			Symbol:   symbol,
			Limit:    toSafeInt32(limit),
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
	if c.IsGRPCEnabled() {
		resp, err := c.grpcClient.GetOHLCV(ctx, &pb.GetOHLCVRequest{
			Exchange:  exchange,
			Symbol:    symbol,
			Timeframe: timeframe,
			Limit:     toSafeInt32(limit),
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
	if c.IsGRPCEnabled() {
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
	if c.IsGRPCEnabled() {
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
	if c.IsGRPCEnabled() {
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
	if c.IsGRPCEnabled() {
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
	reqURL := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NeuraTrade/1.0")

	// Add API key for admin endpoints
	if strings.Contains(path, "/admin/") && c.adminAPIKey != "" {
		req.Header.Set("X-API-Key", c.adminAPIKey)
	}

	// #nosec G704 -- URL is built from trusted service config and fixed API paths
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
		errorMsg := string(respBody)
		if jsonErr := json.Unmarshal(respBody, &errorResp); jsonErr == nil {
			errorMsg = errorResp.Error
		}

		// Extract exchange and symbol from path for typed errors
		exchange, symbol := c.extractExchangeSymbolFromPath(path)

		// Extract operation from path (e.g., "funding-rate", "ticker", "orderbook")
		operation := c.extractOperationFromPath(path)

		// Return typed errors based on HTTP status code
		switch resp.StatusCode {
		case http.StatusNotFound:
			// Symbol not found - don't retry
			return &SymbolNotFoundError{
				Exchange: exchange,
				Symbol:   symbol,
				Message:  errorMsg,
			}
		case http.StatusNotImplemented:
			// Operation not supported by this exchange - don't retry
			return &UnsupportedOperationError{
				Exchange:  exchange,
				Operation: operation,
				Message:   errorMsg,
			}
		case http.StatusBadRequest:
			// Check if error message indicates unsupported operation
			errLower := strings.ToLower(errorMsg)
			if strings.Contains(errLower, "not supported") ||
				strings.Contains(errLower, "not implemented") ||
				strings.Contains(errLower, "not available") ||
				strings.Contains(errLower, "does not support") {
				return &UnsupportedOperationError{
					Exchange:  exchange,
					Operation: operation,
					Message:   errorMsg,
				}
			}
			return fmt.Errorf("CCXT service error (%d): %s", resp.StatusCode, errorMsg)
		case http.StatusServiceUnavailable:
			// Exchange temporarily unavailable - can retry
			return &ExchangeUnavailableError{
				Exchange: exchange,
				Message:  errorMsg,
			}
		default:
			return fmt.Errorf("CCXT service error (%d): %s", resp.StatusCode, errorMsg)
		}
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// extractExchangeSymbolFromPath extracts exchange and symbol from API paths.
//
// Expected path formats:
//   - /api/ticker/{exchange}/{symbol}     -> returns exchange, symbol
//   - /api/orderbook/{exchange}/{symbol}  -> returns exchange, symbol
//   - /api/ohlcv/{exchange}/{symbol}      -> returns exchange, symbol
//   - /api/trades/{exchange}/{symbol}     -> returns exchange, symbol
//   - /api/markets/{exchange}             -> returns exchange, ""
//   - /api/exchanges                      -> returns "", ""
//
// For paths with slashes in the symbol (e.g., /api/ticker/binance/BTC/USDT),
// the symbol parts are joined back together (BTC/USDT).
//
// Returns empty strings for paths that don't match the expected format.
func (c *Client) extractExchangeSymbolFromPath(path string) (exchange, symbol string) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) >= 3 {
		exchange = parts[2]
		if len(parts) >= 4 {
			symbol = strings.Join(parts[3:], "/")
		}
	}
	return
}

// ExtractExchangeSymbolFromPath is exported for testing purposes only.
// It wraps the internal extractExchangeSymbolFromPath function.
// Do not use in production code.
func (c *Client) ExtractExchangeSymbolFromPath(path string) (exchange, symbol string) {
	return c.extractExchangeSymbolFromPath(path)
}

// extractOperationFromPath extracts the operation type from API paths.
//
// Expected path formats:
//   - /api/ticker/{exchange}/{symbol}       -> returns "ticker"
//   - /api/orderbook/{exchange}/{symbol}    -> returns "orderbook"
//   - /api/ohlcv/{exchange}/{symbol}        -> returns "ohlcv"
//   - /api/trades/{exchange}/{symbol}       -> returns "trades"
//   - /api/funding-rate/{exchange}/{symbol} -> returns "funding-rate"
//   - /api/funding-rates/{exchange}         -> returns "funding-rates"
//   - /api/markets/{exchange}               -> returns "markets"
//   - /api/exchanges                        -> returns "exchanges"
//
// Returns "unknown" for paths that don't match the expected format.
func (c *Client) extractOperationFromPath(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "api" {
		return parts[1]
	}
	return "unknown"
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

func toSafeInt32(value int) int32 {
	if value <= 0 {
		return 0
	}
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	// #nosec G115 -- Value is bounds-checked above to prevent overflow
	return int32(value)
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

// CalculateOrderBookMetrics computes liquidity metrics from raw order book data.
// This provides bid-ask spread, depth analysis, and slippage estimates.
func (c *Client) CalculateOrderBookMetrics(resp *OrderBookResponse) *OrderBookMetrics {
	if resp == nil || len(resp.OrderBook.Bids) == 0 || len(resp.OrderBook.Asks) == 0 {
		return nil
	}

	ob := resp.OrderBook
	metrics := &OrderBookMetrics{
		Exchange:          resp.Exchange,
		Symbol:            resp.Symbol,
		BidLevels:         len(ob.Bids),
		AskLevels:         len(ob.Asks),
		Timestamp:         ob.Timestamp,
		SlippageEstimates: make(map[string]SlippageEstimate),
	}

	// Best bid and ask
	metrics.BestBid = ob.Bids[0].Price
	metrics.BestAsk = ob.Asks[0].Price

	// Mid price
	metrics.MidPrice = metrics.BestBid.Add(metrics.BestAsk).Div(decimal.NewFromInt(2))

	// Bid-ask spread as percentage
	if !metrics.MidPrice.IsZero() {
		spread := metrics.BestAsk.Sub(metrics.BestBid)
		metrics.BidAskSpread = spread.Div(metrics.MidPrice).Mul(decimal.NewFromInt(100))
	}

	// Calculate depth within 1% and 2% of mid price
	onePercent := metrics.MidPrice.Mul(decimal.NewFromFloat(0.01))
	twoPercent := metrics.MidPrice.Mul(decimal.NewFromFloat(0.02))

	bidDepth1 := decimal.Zero
	bidDepth2 := decimal.Zero
	for _, bid := range ob.Bids {
		priceDiff := metrics.MidPrice.Sub(bid.Price)
		value := bid.Price.Mul(bid.Amount)
		if priceDiff.LessThanOrEqual(onePercent) {
			bidDepth1 = bidDepth1.Add(value)
		}
		if priceDiff.LessThanOrEqual(twoPercent) {
			bidDepth2 = bidDepth2.Add(value)
		}
	}
	metrics.BidDepth1Pct = bidDepth1
	metrics.BidDepth2Pct = bidDepth2

	askDepth1 := decimal.Zero
	askDepth2 := decimal.Zero
	for _, ask := range ob.Asks {
		priceDiff := ask.Price.Sub(metrics.MidPrice)
		value := ask.Price.Mul(ask.Amount)
		if priceDiff.LessThanOrEqual(onePercent) {
			askDepth1 = askDepth1.Add(value)
		}
		if priceDiff.LessThanOrEqual(twoPercent) {
			askDepth2 = askDepth2.Add(value)
		}
	}
	metrics.AskDepth1Pct = askDepth1
	metrics.AskDepth2Pct = askDepth2

	// Calculate imbalance
	totalDepth1 := bidDepth1.Add(askDepth1)
	if !totalDepth1.IsZero() {
		metrics.Imbalance1Pct = bidDepth1.Sub(askDepth1).Div(totalDepth1)
	}

	totalDepth2 := bidDepth2.Add(askDepth2)
	if !totalDepth2.IsZero() {
		metrics.Imbalance2Pct = bidDepth2.Sub(askDepth2).Div(totalDepth2)
	}

	threshold := decimal.NewFromFloat(0.2)
	if metrics.Imbalance1Pct.GreaterThan(threshold) {
		metrics.ImbalanceSignal = "bullish"
	} else if metrics.Imbalance1Pct.LessThan(threshold.Neg()) {
		metrics.ImbalanceSignal = "bearish"
	} else {
		metrics.ImbalanceSignal = "neutral"
	}

	// Calculate slippage estimates for common position sizes
	positionSizes := []decimal.Decimal{
		decimal.NewFromInt(10000),  // $10k
		decimal.NewFromInt(50000),  // $50k
		decimal.NewFromInt(100000), // $100k
	}

	for _, size := range positionSizes {
		buySlippage := c.calculateSlippage(ob.Asks, size, metrics.MidPrice)
		sellSlippage := c.calculateSlippage(ob.Bids, size, metrics.MidPrice)

		metrics.SlippageEstimates[size.String()] = SlippageEstimate{
			PositionSize: size,
			BuySlippage:  buySlippage.Slippage,
			SellSlippage: sellSlippage.Slippage,
			AvgBuyPrice:  buySlippage.AvgPrice,
			AvgSellPrice: sellSlippage.AvgPrice,
			IsFillable:   buySlippage.IsFillable && sellSlippage.IsFillable,
		}
	}

	// Calculate liquidity score (0-100)
	metrics.LiquidityScore = c.calculateLiquidityScore(metrics)

	return metrics
}

// slippageResult holds the result of a slippage calculation.
type slippageResult struct {
	Slippage   decimal.Decimal
	AvgPrice   decimal.Decimal
	IsFillable bool
}

// calculateSlippage walks through order book levels to estimate slippage.
func (c *Client) calculateSlippage(levels []OrderBookEntry, positionSize decimal.Decimal, midPrice decimal.Decimal) slippageResult {
	if len(levels) == 0 || positionSize.IsZero() || midPrice.IsZero() {
		return slippageResult{IsFillable: false}
	}

	remaining := positionSize
	totalCost := decimal.Zero
	totalQuantity := decimal.Zero

	for _, level := range levels {
		levelValue := level.Price.Mul(level.Amount)

		if levelValue.GreaterThanOrEqual(remaining) {
			// This level can fill the rest
			quantity := remaining.Div(level.Price)
			totalCost = totalCost.Add(remaining)
			totalQuantity = totalQuantity.Add(quantity)
			remaining = decimal.Zero
			break
		}

		// Consume entire level
		totalCost = totalCost.Add(levelValue)
		totalQuantity = totalQuantity.Add(level.Amount)
		remaining = remaining.Sub(levelValue)
	}

	if totalQuantity.IsZero() {
		return slippageResult{IsFillable: false}
	}

	avgPrice := totalCost.Div(totalQuantity)
	slippage := avgPrice.Sub(midPrice).Div(midPrice).Abs().Mul(decimal.NewFromInt(100))

	return slippageResult{
		Slippage:   slippage,
		AvgPrice:   avgPrice,
		IsFillable: remaining.IsZero(),
	}
}

// calculateLiquidityScore computes an overall liquidity score (0-100).
func (c *Client) calculateLiquidityScore(metrics *OrderBookMetrics) decimal.Decimal {
	if metrics == nil {
		return decimal.Zero
	}

	// Spread score: lower spread = higher score
	// 0.01% spread = 100, 1% spread = 0
	spreadScore := decimal.NewFromInt(100).Sub(metrics.BidAskSpread.Mul(decimal.NewFromInt(100)))
	if spreadScore.LessThan(decimal.Zero) {
		spreadScore = decimal.Zero
	}

	// Depth score: more depth = higher score
	// $1M depth = 100, $10k depth = 10
	totalDepth := metrics.BidDepth1Pct.Add(metrics.AskDepth1Pct)
	depthScore := totalDepth.Div(decimal.NewFromInt(10000)).Mul(decimal.NewFromInt(10))
	if depthScore.GreaterThan(decimal.NewFromInt(100)) {
		depthScore = decimal.NewFromInt(100)
	}

	// Imbalance penalty: higher imbalance = lower score
	imbalancePenalty := metrics.Imbalance1Pct.Abs().Mul(decimal.NewFromInt(20))

	// Weighted average
	score := spreadScore.Mul(decimal.NewFromFloat(0.4)).
		Add(depthScore.Mul(decimal.NewFromFloat(0.5))).
		Sub(imbalancePenalty.Mul(decimal.NewFromFloat(0.1)))

	if score.LessThan(decimal.Zero) {
		score = decimal.Zero
	}
	if score.GreaterThan(decimal.NewFromInt(100)) {
		score = decimal.NewFromInt(100)
	}

	return score
}

// OrderBookMetrics contains calculated metrics derived from raw order book data.
type OrderBookMetrics struct {
	Exchange     string          `json:"exchange"`
	Symbol       string          `json:"symbol"`
	BidAskSpread decimal.Decimal `json:"bid_ask_spread"`
	MidPrice     decimal.Decimal `json:"mid_price"`
	BestBid      decimal.Decimal `json:"best_bid"`
	BestAsk      decimal.Decimal `json:"best_ask"`

	BidDepth1Pct decimal.Decimal `json:"bid_depth_1pct"`
	AskDepth1Pct decimal.Decimal `json:"ask_depth_1pct"`
	BidDepth2Pct decimal.Decimal `json:"bid_depth_2pct"`
	AskDepth2Pct decimal.Decimal `json:"ask_depth_2pct"`

	Imbalance1Pct decimal.Decimal `json:"imbalance_1pct"`
	Imbalance2Pct decimal.Decimal `json:"imbalance_2pct"`

	ImbalanceSignal   string                      `json:"imbalance_signal"` // bullish/bearish/neutral based on imbalance
	SlippageEstimates map[string]SlippageEstimate `json:"slippage_estimates"`
	LiquidityScore    decimal.Decimal             `json:"liquidity_score"`

	BidLevels int       `json:"bid_levels"`
	AskLevels int       `json:"ask_levels"`
	Timestamp time.Time `json:"timestamp"`
}

// SlippageEstimate represents estimated slippage for a given position size.
type SlippageEstimate struct {
	PositionSize decimal.Decimal `json:"position_size"`
	BuySlippage  decimal.Decimal `json:"buy_slippage"`
	SellSlippage decimal.Decimal `json:"sell_slippage"`
	AvgBuyPrice  decimal.Decimal `json:"avg_buy_price"`
	AvgSellPrice decimal.Decimal `json:"avg_sell_price"`
	IsFillable   bool            `json:"is_fillable"`
}
