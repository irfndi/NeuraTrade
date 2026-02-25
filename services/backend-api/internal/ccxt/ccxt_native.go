package ccxt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

// parseDecimal safely parses a string to decimal, returning zero on error
func parseDecimal(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

// rateLimiter implements simple rate limiting for API calls
type rateLimiter struct {
	mu       sync.Mutex
	lastCall time.Time
	minDelay time.Duration
}

func newRateLimiter(callsPerSecond int) *rateLimiter {
	return &rateLimiter{
		minDelay: time.Duration(float64(time.Second) / float64(callsPerSecond)),
	}
}

func (r *rateLimiter) Wait() {
	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.lastCall)
	if elapsed < r.minDelay {
		time.Sleep(r.minDelay - elapsed)
	}
	r.lastCall = time.Now()
}

// NativeCCXTService implements CCXTService using direct exchange API calls
type NativeCCXTService struct {
	mu            sync.RWMutex
	httpClient    *http.Client
	exchanges     map[string]*ExchangeConnection
	timeout       time.Duration
	retryAttempts int
	rateLimiter   *rateLimiter
}

// ExchangeConnection holds exchange-specific configuration
type ExchangeConnection struct {
	Name       string
	BaseURL    string
	APIKey     string
	Secret     string
	Testnet    bool
	LastUpdate time.Time
}

// NewNativeCCXTService creates a new native CCXT service
func NewNativeCCXTService(timeout time.Duration, retryAttempts int) *NativeCCXTService {
	return &NativeCCXTService{
		rateLimiter: newRateLimiter(10), // 10 requests per second
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		exchanges:     make(map[string]*ExchangeConnection),
		timeout:       timeout,
		retryAttempts: retryAttempts,
	}
}

// Initialize prepares the service for use
func (s *NativeCCXTService) Initialize(ctx context.Context) error {
	log.Println("[CCXT Native] Initializing native exchange connections")

	// Initialize default exchanges
	defaultExchanges := []string{"binance", "bybit", "okx"}
	for _, exchange := range defaultExchanges {
		if err := s.initializeExchange(exchange); err != nil {
			log.Printf("[CCXT Native] Failed to initialize %s: %v", exchange, err)
			continue
		}
	}

	log.Printf("[CCXT Native] Initialized %d exchanges", len(s.exchanges))
	return nil
}

// initializeExchange sets up a single exchange connection
func (s *NativeCCXTService) initializeExchange(exchangeID string) error {
	baseURL, ok := s.getExchangeBaseURL(exchangeID)
	if !ok {
		return fmt.Errorf("unknown exchange: %s", exchangeID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.exchanges[exchangeID] = &ExchangeConnection{
		Name:       exchangeID,
		BaseURL:    baseURL,
		LastUpdate: time.Now(),
	}

	log.Printf("[CCXT Native] Initialized exchange: %s (%s)", exchangeID, baseURL)
	return nil
}

// getExchangeBaseURL returns the base URL for an exchange
func (s *NativeCCXTService) getExchangeBaseURL(exchangeID string) (string, bool) {
	urls := map[string]string{
		"binance":   "https://api.binance.com",
		"bybit":     "https://api.bybit.com",
		"okx":       "https://www.okx.com",
		"kraken":    "https://api.kraken.com",
		"kucoin":    "https://api.kucoin.com",
		"gateio":    "https://api.gateio.ws",
		"mexc":      "https://api.mexc.com",
		"bitget":    "https://api.bitget.com",
		"coinbase":  "https://api.coinbase.com",
		"bingx":     "https://open-api.bingx.com",
		"cryptocom": "https://api.crypto.com",
	}

	url, ok := urls[exchangeID]
	return url, ok
}

// IsHealthy checks if the service is operational
func (s *NativeCCXTService) IsHealthy(ctx context.Context) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	healthy := len(s.exchanges) > 0
	if !healthy {
		return false
	}

	// Check at least one exchange is reachable
	for exchangeID := range s.exchanges {
		if s.testExchangeConnection(ctx, exchangeID) {
			return true
		}
	}

	return false
}

// testExchangeConnection tests connectivity to an exchange
func (s *NativeCCXTService) testExchangeConnection(ctx context.Context, exchangeID string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := s.getExchangePingURL(exchangeID)
	if url == "" {
		return false
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// getExchangePingURL returns a ping/health endpoint for an exchange
func (s *NativeCCXTService) getExchangePingURL(exchangeID string) string {
	pings := map[string]string{
		"binance":   "https://api.binance.com/api/v3/ping",
		"bybit":     "https://api.bybit.com/v5/public/linear",
		"okx":       "https://www.okx.com/api/v5/public/time",
		"kraken":    "https://api.kraken.com/0/public/Time",
		"kucoin":    "https://api.kucoin.com/api/v1/timestamp",
		"gateio":    "https://api.gateio.ws/api/v4/spot/currencies",
		"mexc":      "https://api.mexc.com/api/v3/time",
		"bitget":    "https://api.bitget.com/api/v2/public/time",
		"coinbase":  "https://api.coinbase.com/api/v3/brokerage/time",
		"bingx":     "https://open-api.bingx.com/openApi/quote/v1/time",
		"cryptocom": "https://api.crypto.com/exchange/v1/public/time",
	}

	return pings[exchangeID]
}

// Close terminates the service
func (s *NativeCCXTService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.exchanges = make(map[string]*ExchangeConnection)
	log.Println("[CCXT Native] Service closed")
	return nil
}

// GetServiceURL returns the base URL of the underlying service
func (s *NativeCCXTService) GetServiceURL() string {
	return "native"
}

// GetSupportedExchanges returns a list of supported exchange IDs
func (s *NativeCCXTService) GetSupportedExchanges() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exchanges := make([]string, 0, len(s.exchanges))
	for exchangeID := range s.exchanges {
		exchanges = append(exchanges, exchangeID)
	}

	return exchanges
}

// GetExchangeInfo retrieves detailed info for a specific exchange
func (s *NativeCCXTService) GetExchangeInfo(exchangeID string) (ExchangeInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conn, ok := s.exchanges[exchangeID]
	if !ok {
		return ExchangeInfo{}, false
	}

	return ExchangeInfo{
		ID:        exchangeID,
		Name:      conn.Name,
		Countries: []string{},
		URLs:      map[string]interface{}{"api": conn.BaseURL},
	}, true
}

// FetchSingleTicker retrieves a single ticker - KEY METHOD FOR SCALPING
func (s *NativeCCXTService) FetchSingleTicker(ctx context.Context, exchange, symbol string) (MarketPriceInterface, error) {
	s.mu.RLock()
	_, ok := s.exchanges[exchange]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("exchange %s not initialized", exchange)
	}

	// Normalize symbol (remove "/" for API calls)
	apiSymbol := strings.ReplaceAll(symbol, "/", "")

	// Build exchange-specific URL
	url := s.buildTickerURL(exchange, apiSymbol)
	if url == "" {
		return nil, fmt.Errorf("ticker endpoint not supported for %s", exchange)
	}

	// Fetch with retry
	var tickerData *TickerData
	var err error

	for attempt := 0; attempt < s.retryAttempts; attempt++ {
		tickerData, err = s.fetchTickerFromURL(ctx, url, exchange, symbol)
		if err == nil {
			break
		}

		if attempt < s.retryAttempts-1 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch ticker for %s:%s: %w", exchange, symbol, err)
	}

	return &TickerMarketPriceAdapter{data: tickerData}, nil
}

// buildTickerURL builds the ticker URL for an exchange
func (s *NativeCCXTService) buildTickerURL(exchange, symbol string) string {
	switch exchange {
	case "binance":
		return fmt.Sprintf("https://api.binance.com/api/v3/ticker/24hr?symbol=%s", symbol)
	case "bybit":
		return fmt.Sprintf("https://api.bybit.com/v5/market/tickers?category=linear&symbol=%s", symbol)
	case "okx":
		return fmt.Sprintf("https://www.okx.com/api/v5/market/ticker?instId=%s", symbol)
	case "kraken":
		return fmt.Sprintf("https://api.kraken.com/0/public/Ticker?pair=%s", symbol)
	case "kucoin":
		return fmt.Sprintf("https://api.kucoin.com/api/v1/market/orderbook/level1?symbol=%s", symbol)
	case "gateio":
		return fmt.Sprintf("https://api.gateio.ws/api/v4/spot/tickers?currency_pair=%s", symbol)
	case "mexc":
		return fmt.Sprintf("https://api.mexc.com/api/v3/ticker/24hr?symbol=%s", symbol)
	case "bitget":
		return fmt.Sprintf("https://api.bitget.com/api/v2/market/tickers?productType=USDT-FUTURES&symbol=%s", symbol)
	default:
		return ""
	}
}

// buildOrderBookURL builds the orderbook URL for an exchange
func (s *NativeCCXTService) buildOrderBookURL(exchange, symbol, apiSymbol string, limit int) string {
	switch exchange {
	case "binance":
		return fmt.Sprintf("https://api.binance.com/api/v3/depth?symbol=%s&limit=%d", apiSymbol, limit)
	case "bybit":
		return fmt.Sprintf("https://api.bybit.com/v5/market/orderbook?category=linear&symbol=%s&limit=%d", apiSymbol, limit)
	case "okx":
		return fmt.Sprintf("https://www.okx.com/api/v5/market/books?instId=%s&sz=%d", apiSymbol, limit)
	default:
		return ""
	}
}

// parseOrderBookResponse parses orderbook data from API response
func (s *NativeCCXTService) parseOrderBookResponse(exchange, symbol string, body []byte, limit int) (*OrderBookResponse, error) {
	switch exchange {
	case "binance":
		return s.parseBinanceOrderBook(symbol, body, limit)
	case "bybit":
		return s.parseBybitOrderBook(symbol, body, limit)
	case "okx":
		return s.parseOKXOrderBook(symbol, body, limit)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// fetchTickerFromURL fetches and parses ticker data from a URL
func (s *NativeCCXTService) fetchTickerFromURL(ctx context.Context, url, exchange, symbol string) (*TickerData, error) {
	// Rate limit API calls
	s.rateLimiter.Wait()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "NeuraTrade/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse exchange-specific response
	ticker, err := s.parseTickerResponse(exchange, symbol, body)
	if err != nil {
		return nil, err
	}

	return &TickerData{
		Exchange: exchange,
		Ticker:   *ticker,
	}, nil
}

// parseTickerResponse parses exchange-specific ticker responses
func (s *NativeCCXTService) parseTickerResponse(exchange, symbol string, body []byte) (*Ticker, error) {
	switch exchange {
	case "binance":
		return s.parseBinanceTicker(symbol, body)
	case "bybit":
		return s.parseBybitTicker(symbol, body)
	case "okx":
		return s.parseOKXTicker(symbol, body)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// parseBinanceTicker parses Binance ticker response
func (s *NativeCCXTService) parseBinanceTicker(symbol string, body []byte) (*Ticker, error) {
	var raw struct {
		Symbol         string `json:"symbol"`
		LastPrice      string `json:"lastPrice"`
		BidPrice       string `json:"bidPrice"`
		AskPrice       string `json:"askPrice"`
		High24h        string `json:"highPrice"`
		Low24h         string `json:"lowPrice"`
		Volume24h      string `json:"volume"`
		OpenPrice      string `json:"openPrice"`
		PrevClosePrice string `json:"prevClosePrice"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Binance response: %w", err)
	}

	return &Ticker{
		Symbol:    symbol,
		Last:      parseDecimal(raw.LastPrice),
		Bid:       parseDecimal(raw.BidPrice),
		Ask:       parseDecimal(raw.AskPrice),
		High:      parseDecimal(raw.High24h),
		Low:       parseDecimal(raw.Low24h),
		Volume:    parseDecimal(raw.Volume24h),
		Open:      parseDecimal(raw.OpenPrice),
		Close:     parseDecimal(raw.PrevClosePrice),
		Timestamp: UnixTimestamp(time.Now()),
	}, nil
}

// parseBybitTicker parses Bybit ticker response
func (s *NativeCCXTService) parseBybitTicker(symbol string, body []byte) (*Ticker, error) {
	var raw struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol    string `json:"symbol"`
				LastPrice string `json:"lastPrice"`
				Bid1Price string `json:"bid1Price"`
				Ask1Price string `json:"ask1Price"`
				High24h   string `json:"highPrice24h"`
				Low24h    string `json:"lowPrice24h"`
				Volume24h string `json:"volume24h"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit response: %w", err)
	}

	if raw.RetCode != 0 {
		return nil, fmt.Errorf("Bybit API error: %s", raw.RetMsg)
	}

	if len(raw.Result.List) == 0 {
		return nil, fmt.Errorf("no ticker data for %s", symbol)
	}

	t := raw.Result.List[0]
	return &Ticker{
		Symbol:    t.Symbol,
		Last:      parseDecimal(t.LastPrice),
		Bid:       parseDecimal(t.Bid1Price),
		Ask:       parseDecimal(t.Ask1Price),
		High:      parseDecimal(t.High24h),
		Low:       parseDecimal(t.Low24h),
		Volume:    parseDecimal(t.Volume24h),
		Timestamp: UnixTimestamp(time.Now()),
	}, nil
}

// parseOKXTicker parses OKX ticker response
func (s *NativeCCXTService) parseOKXTicker(symbol string, body []byte) (*Ticker, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID  string `json:"instId"`
			Last    string `json:"last"`
			BidPx   string `json:"bidPx"`
			AskPx   string `json:"askPx"`
			High24h string `json:"high24h"`
			Low24h  string `json:"low24h"`
			Vol24h  string `json:"vol24h"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse OKX response: %w", err)
	}

	if raw.Code != "0" {
		return nil, fmt.Errorf("OKX API error: %s", raw.Msg)
	}

	if len(raw.Data) == 0 {
		return nil, fmt.Errorf("no ticker data for %s", symbol)
	}

	t := raw.Data[0]
	return &Ticker{
		Symbol:    t.InstID,
		Last:      parseDecimal(t.Last),
		Bid:       parseDecimal(t.BidPx),
		Ask:       parseDecimal(t.AskPx),
		High:      parseDecimal(t.High24h),
		Low:       parseDecimal(t.Low24h),
		Volume:    parseDecimal(t.Vol24h),
		Timestamp: UnixTimestamp(time.Now()),
	}, nil
}

// TickerMarketPriceAdapter adapts TickerData to MarketPriceInterface
type TickerMarketPriceAdapter struct {
	data *TickerData
}

func (a *TickerMarketPriceAdapter) GetPrice() float64 {
	v, _ := a.data.Ticker.Last.Float64()
	return v
}
func (a *TickerMarketPriceAdapter) GetVolume() float64 {
	v, _ := a.data.Ticker.Volume.Float64()
	return v
}
func (a *TickerMarketPriceAdapter) GetTimestamp() time.Time {
	return time.Time(a.data.Ticker.Timestamp)
}
func (a *TickerMarketPriceAdapter) GetExchangeName() string {
	return a.data.Exchange
}
func (a *TickerMarketPriceAdapter) GetSymbol() string {
	return a.data.Ticker.Symbol
}
func (a *TickerMarketPriceAdapter) GetBid() float64 {
	v, _ := a.data.Ticker.Bid.Float64()
	return v
}
func (a *TickerMarketPriceAdapter) GetAsk() float64 {
	v, _ := a.data.Ticker.Ask.Float64()
	return v
}
func (a *TickerMarketPriceAdapter) GetHigh() float64 {
	v, _ := a.data.Ticker.High.Float64()
	return v
}
func (a *TickerMarketPriceAdapter) GetLow() float64 {
	v, _ := a.data.Ticker.Low.Float64()
	return v
}

// Stub implementations for other required methods
func (s *NativeCCXTService) GetExchangeConfig(ctx context.Context) (*ExchangeConfigResponse, error) {
	return &ExchangeConfigResponse{}, nil
}

func (s *NativeCCXTService) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	return &ExchangeManagementResponse{}, nil
}

func (s *NativeCCXTService) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	return &ExchangeManagementResponse{}, nil
}

func (s *NativeCCXTService) RefreshExchanges(ctx context.Context) (*ExchangeManagementResponse, error) {
	return &ExchangeManagementResponse{}, nil
}

func (s *NativeCCXTService) AddExchange(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	return &ExchangeManagementResponse{}, nil
}

func (s *NativeCCXTService) FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]MarketPriceInterface, error) {
	var allTickers []MarketPriceInterface

	for _, exchange := range exchanges {
		for _, symbol := range symbols {
			ticker, err := s.FetchSingleTicker(ctx, exchange, symbol)
			if err != nil {
				log.Printf("[CCXT Native] Failed to fetch %s:%s: %v", exchange, symbol, err)
				continue
			}
			allTickers = append(allTickers, ticker)
		}
	}

	return allTickers, nil
}

func (s *NativeCCXTService) FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error) {
	// Check if exchange is initialized
	s.mu.RLock()
	_, ok := s.exchanges[exchange]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("exchange %s not initialized", exchange)
	}

	// Build orderbook URL
	url := s.buildOrderBookURL(exchange, symbol, symbol, limit)
	if url == "" {
		return nil, fmt.Errorf("orderbook endpoint not supported for %s", exchange)
	}

	// Rate limit API calls
	s.rateLimiter.Wait()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "NeuraTrade/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch orderbook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return s.parseOrderBookResponse(exchange, symbol, body, limit)
}

func (s *NativeCCXTService) CalculateOrderBookMetrics(ctx context.Context, exchange, symbol string, limit int) (*OrderBookMetrics, error) {
	return &OrderBookMetrics{}, nil
}

func (s *NativeCCXTService) FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error) {
	return &OHLCVResponse{}, nil
}

func (s *NativeCCXTService) FetchTrades(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error) {
	return &TradesResponse{}, nil
}

func (s *NativeCCXTService) FetchMarkets(ctx context.Context, exchange string) (*MarketsResponse, error) {
	s.mu.RLock()
	_, ok := s.exchanges[exchange]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("exchange %s not initialized", exchange)
	}

	url := s.buildMarketsURL(exchange)
	if url == "" {
		return nil, fmt.Errorf("markets endpoint not supported for %s", exchange)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NeuraTrade/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	symbols, err := s.parseMarketsResponse(exchange, body)
	if err != nil {
		return nil, err
	}

	return &MarketsResponse{
		Exchange:  exchange,
		Symbols:   symbols,
		Count:     len(symbols),
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

func (s *NativeCCXTService) buildMarketsURL(exchange string) string {
	switch exchange {
	case "binance":
		return "https://api.binance.com/api/v3/exchangeInfo"
	case "bybit":
		return "https://api.bybit.com/v5/market/instruments-info?category=linear"
	case "okx":
		return "https://www.okx.com/api/v5/public/instruments?instType=SWAP"
	default:
		return ""
	}
}

func (s *NativeCCXTService) parseMarketsResponse(exchange string, body []byte) ([]string, error) {
	switch exchange {
	case "binance":
		return s.parseBinanceMarkets(body)
	case "bybit":
		return s.parseBybitMarkets(body)
	case "okx":
		return s.parseOKXMarkets(body)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

func (s *NativeCCXTService) parseBinanceMarkets(body []byte) ([]string, error) {
	var raw struct {
		Symbols []struct {
			Symbol     string `json:"symbol"`
			BaseAsset  string `json:"baseAsset"`
			QuoteAsset string `json:"quoteAsset"`
			Status     string `json:"status"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Binance markets: %w", err)
	}
	var symbols []string
	for _, sym := range raw.Symbols {
		if sym.Status != "TRADING" || sym.QuoteAsset != "USDT" {
			continue
		}
		// Convert BTCUSDT format to BTC/USDT format
		formatted := sym.BaseAsset + "/" + sym.QuoteAsset
		symbols = append(symbols, formatted)
	}
	return symbols, nil
}

func (s *NativeCCXTService) parseBybitMarkets(body []byte) ([]string, error) {
	var raw struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol    string `json:"symbol"`
				BaseCoin  string `json:"baseCoin"`
				QuoteCoin string `json:"quoteCoin"`
				Status    string `json:"status"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit markets: %w", err)
	}
	if raw.RetCode != 0 {
		return nil, fmt.Errorf("Bybit API error")
	}
	var symbols []string
	for _, sym := range raw.Result.List {
		if sym.Status != "Trading" || sym.QuoteCoin != "USDT" {
			continue
		}
		// Convert BTCUSDT format to BTC/USDT format
		formatted := sym.BaseCoin + "/" + sym.QuoteCoin
		symbols = append(symbols, formatted)
	}
	return symbols, nil
}

func (s *NativeCCXTService) parseOKXMarkets(body []byte) ([]string, error) {
	var raw struct {
		Code string `json:"code"`
		Data []struct {
			InstId   string `json:"instId"`
			BaseCcy  string `json:"baseCcy"`
			QuoteCcy string `json:"quoteCcy"`
			State    string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse OKX markets: %w", err)
	}
	if raw.Code != "0" {
		return nil, fmt.Errorf("OKX API error")
	}
	var symbols []string
	for _, inst := range raw.Data {
		if inst.State != "live" || inst.QuoteCcy != "USDT" {
			continue
		}
		// Convert BTC-USDT format to BTC/USDT format
		formatted := inst.BaseCcy + "/" + inst.QuoteCcy
		symbols = append(symbols, formatted)
	}
	return symbols, nil
}

// parseBinanceOrderBook parses Binance orderbook response
func (s *NativeCCXTService) parseBinanceOrderBook(symbol string, body []byte, limit int) (*OrderBookResponse, error) {
	var raw struct {
		LastUpdateID int64      `json:"lastUpdateId"`
		Bids         [][]string `json:"bids"`
		Asks         [][]string `json:"asks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Binance orderbook: %w", err)
	}

	bids := make([]OrderBookEntry, 0, len(raw.Bids))
	for _, bid := range raw.Bids {
		if len(bid) >= 2 {
			bids = append(bids, OrderBookEntry{
				Price:  parseDecimal(bid[0]),
				Amount: parseDecimal(bid[1]),
			})
		}
	}

	asks := make([]OrderBookEntry, 0, len(raw.Asks))
	for _, ask := range raw.Asks {
		if len(ask) >= 2 {
			asks = append(asks, OrderBookEntry{
				Price:  parseDecimal(ask[0]),
				Amount: parseDecimal(ask[1]),
			})
		}
	}

	return &OrderBookResponse{
		Exchange: "binance",
		Symbol:   symbol,
		OrderBook: OrderBook{
			Symbol:    symbol,
			Bids:      bids,
			Asks:      asks,
			Timestamp: time.Now(),
			Nonce:     raw.LastUpdateID,
		},
	}, nil
}

// parseBybitOrderBook parses Bybit orderbook response
func (s *NativeCCXTService) parseBybitOrderBook(symbol string, body []byte, limit int) (*OrderBookResponse, error) {
	var raw struct {
		RetCode int `json:"retCode"`
		Result  struct {
			Bids       [][]string `json:"b"`
			Asks       [][]string `json:"a"`
			Timestamp  int64      `json:"ts"`
			UpdateID   int64      `json:"u"`
			Sequence   int64      `json:"seq"`
		} `json:"result"`
		RetMsg string `json:"retMsg"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit orderbook: %w", err)
	}
	if raw.RetCode != 0 {
		return nil, fmt.Errorf("Bybit API error: %s", raw.RetMsg)
	}

	bids := make([]OrderBookEntry, 0, len(raw.Result.Bids))
	for _, bid := range raw.Result.Bids {
		if len(bid) >= 2 {
			bids = append(bids, OrderBookEntry{
				Price:  parseDecimal(bid[0]),
				Amount: parseDecimal(bid[1]),
			})
		}
	}

	asks := make([]OrderBookEntry, 0, len(raw.Result.Asks))
	for _, ask := range raw.Result.Asks {
		if len(ask) >= 2 {
			asks = append(asks, OrderBookEntry{
				Price:  parseDecimal(ask[0]),
				Amount: parseDecimal(ask[1]),
			})
		}
	}

	return &OrderBookResponse{
		Exchange: "bybit",
		Symbol:   symbol,
		OrderBook: OrderBook{
			Symbol:    symbol,
			Bids:      bids,
			Asks:      asks,
			Timestamp: time.UnixMilli(raw.Result.Timestamp),
			Nonce:     raw.Result.UpdateID,
		},
	}, nil
}

// parseOKXOrderBook parses OKX orderbook response
func (s *NativeCCXTService) parseOKXOrderBook(symbol string, body []byte, limit int) (*OrderBookResponse, error) {
	var raw struct {
		Code string `json:"code"`
		Data []struct {
			Asks      [][]string `json:"asks"`
			Bids      [][]string `json:"bids"`
			Timestamp string     `json:"ts"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse OKX orderbook: %w", err)
	}
	if raw.Code != "0" {
		return nil, fmt.Errorf("OKX API error: %s", raw.Msg)
	}
	if len(raw.Data) == 0 {
		return nil, fmt.Errorf("OKX returned empty orderbook")
	}

	data := raw.Data[0]
	bids := make([]OrderBookEntry, 0, len(data.Bids))
	for _, bid := range data.Bids {
		if len(bid) >= 2 {
			bids = append(bids, OrderBookEntry{
				Price:  parseDecimal(bid[0]),
				Amount: parseDecimal(bid[1]),
			})
		}
	}

	asks := make([]OrderBookEntry, 0, len(data.Asks))
	for _, ask := range data.Asks {
		if len(ask) >= 2 {
			asks = append(asks, OrderBookEntry{
				Price:  parseDecimal(ask[0]),
				Amount: parseDecimal(ask[1]),
			})
		}
	}

	ts, _ := strconv.ParseInt(data.Timestamp, 10, 64)

	return &OrderBookResponse{
		Exchange: "okx",
		Symbol:   symbol,
		OrderBook: OrderBook{
			Symbol:    symbol,
			Bids:      bids,
			Asks:      asks,
			Timestamp: time.UnixMilli(ts),
		},
	}, nil
}

func (s *NativeCCXTService) FetchBalance(ctx context.Context, exchange string) (*BalanceResponse, error) {
	// Return empty balance for dry-run mode
	return &BalanceResponse{
		Exchange: exchange,
		Total:    make(map[string]float64),
		Free:     make(map[string]float64),
		Used:     make(map[string]float64),
	}, nil
}

func (s *NativeCCXTService) FetchFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error) {
	return &FundingRate{}, nil
}

func (s *NativeCCXTService) FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
	return []FundingRate{}, nil
}

func (s *NativeCCXTService) FetchAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error) {
	return []FundingRate{}, nil
}

func (s *NativeCCXTService) CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	return []models.ArbitrageOpportunityResponse{}, nil
}

func (s *NativeCCXTService) CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]FundingArbitrageOpportunity, error) {
	return []FundingArbitrageOpportunity{}, nil
}
