package ccxt

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
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
	Passphrase string
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

// NewNativeCCXTServiceWithConfig creates a native CCXT service with exchange credentials from config
func NewNativeCCXTServiceWithConfig(timeout time.Duration, retryAttempts int, exchangeCreds map[string]config.ExchangeCredentials) *NativeCCXTService {
	s := NewNativeCCXTService(timeout, retryAttempts)
	
	// Populate exchange credentials from config
	for name, creds := range exchangeCreds {
		baseURL, ok := s.getExchangeBaseURL(name)
		if !ok {
			log.Printf("[CCXT Native] Unknown exchange in config: %s", name)
			continue
		}
		s.exchanges[name] = &ExchangeConnection{
			Name:       name,
			BaseURL:    baseURL,
			APIKey:     creds.APIKey,
			Secret:     creds.Secret,
			Passphrase: creds.Passphrase,
			Testnet:    creds.Testnet,
			LastUpdate: time.Now(),
		}
		log.Printf("[CCXT Native] Configured exchange: %s with API key", name)
	}
	return s
}

// Initialize prepares the service for use
func (s *NativeCCXTService) Initialize(ctx context.Context) error {
	log.Println("[CCXT Native] Initializing native exchange connections")

	// Initialize default exchanges
	defaultExchanges := []string{"binance", "bybit", "okx", "bitget"}
	
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
	defer func() { _ = resp.Body.Close() }()

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

// BaseURL returns the base URL for the native CCXT service (empty for direct API calls)
func (s *NativeCCXTService) BaseURL() string {
	return ""
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
		return fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/tickers?symbol=%s", symbol)
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
	case "bitget":
		return fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/orderbook?symbol=%s&limit=%d", apiSymbol, limit)
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
	case "bitget":
		return s.parseBitgetOrderBook(symbol, body, limit)
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
	defer func() { _ = resp.Body.Close() }()

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

func (s *NativeCCXTService) parseTickerResponse(exchange, symbol string, body []byte) (*Ticker, error) {
	switch exchange {
	case "binance":
		return s.parseBinanceTicker(symbol, body)
	case "bybit":
		return s.parseBybitTicker(symbol, body)
	case "okx":
		return s.parseOKXTicker(symbol, body)
	case "bitget":
		return s.parseBitgetTicker(symbol, body)
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
		return nil, fmt.Errorf("bybit API error: %s", raw.RetMsg)
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


func (s *NativeCCXTService) parseBitgetTicker(symbol string, body []byte) (*Ticker, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol      string `json:"symbol"`
			LastPr      string `json:"lastPr"`
			BidPr       string `json:"bidPr"`
			AskPr       string `json:"askPr"`
			High24h     string `json:"high24h"`
			Low24h      string `json:"low24h"`
			BaseVolume  string `json:"baseVolume"`
			Change24h   string `json:"change24h"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget response: %w", err)
	}

	if raw.Code != "00000" {
		return nil, fmt.Errorf("Bitget API error: %s", raw.Msg)
	}

	if len(raw.Data) == 0 {
		return nil, fmt.Errorf("no ticker data for %s", symbol)
	}

	t := raw.Data[0]
	return &Ticker{
		Symbol:    symbol,
		Last:      parseDecimal(t.LastPr),
		Bid:       parseDecimal(t.BidPr),
		Ask:       parseDecimal(t.AskPr),
		High:      parseDecimal(t.High24h),
		Low:       parseDecimal(t.Low24h),
		Volume:    parseDecimal(t.BaseVolume),
		Timestamp: UnixTimestamp(time.Now()),
	}, nil
}

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
	// Convert symbol format for Bitget (BTC/USDT -> BTCUSDT)
	apiSymbol := symbol
	if exchange == "bitget" {
		apiSymbol = strings.ReplaceAll(symbol, "/", "")
	}
	url := s.buildOrderBookURL(exchange, symbol, apiSymbol, limit)
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
	defer func() { _ = resp.Body.Close() }()

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
	s.mu.RLock()
	_, ok := s.exchanges[exchange]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("exchange %s not initialized", exchange)
	}

	if limit <= 0 {
		limit = 100
	}

	// Normalize symbol for API calls
	apiSymbol := strings.ReplaceAll(symbol, "/", "")

	// Build OHLCV URL based on exchange
	url := s.buildOHLCVURL(exchange, apiSymbol, timeframe, limit)
	if url == "" {
		return nil, fmt.Errorf("OHLCV endpoint not supported for %s", exchange)
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
		return nil, fmt.Errorf("failed to fetch OHLCV: %w", err)
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

	return s.parseOHLCVResponse(exchange, symbol, timeframe, body)
}

// buildOHLCVURL builds the OHLCV URL for an exchange
func (s *NativeCCXTService) buildOHLCVURL(exchange, symbol, timeframe string, limit int) string {
	switch exchange {
	case "binance":
		// Binance intervals: 1m, 3m, 5m, 15m, 30m, 1h, 2h, 4h, 6h, 8h, 12h, 1d, 3d, 1w, 1M
		return fmt.Sprintf("https://api.binance.com/api/v3/klines?symbol=%s&interval=%s&limit=%d", symbol, timeframe, limit)
	case "bybit":
		// Bybit intervals: 1, 3, 5, 15, 30, 60, 120, 240, 360, 720, D, W, M
		interval := s.convertTimeframeToBybit(timeframe)
		return fmt.Sprintf("https://api.bybit.com/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d", symbol, interval, limit)
	case "okx":
		// OKX bar: 1m, 5m, 15m, 30m, 1H, 4H, 1D, 1W, 1M
		instId := strings.ReplaceAll(symbol, "USDT", "-USDT")
		return fmt.Sprintf("https://www.okx.com/api/v5/market/candles?instId=%s&bar=%s&limit=%d", instId, timeframe, limit)
	case "bitget":
		// Bitget intervals: 1m, 5m, 15m, 30m, 1H, 4H, 1D, 1W, 1M
		return fmt.Sprintf("https://api.bitget.com/api/v2/spot/market/candles?symbol=%s&granularity=%s&limit=%d", symbol, timeframe, limit)
	default:
		return ""
	}
}

// convertTimeframeToBybit converts standard timeframe to Bybit format
func (s *NativeCCXTService) convertTimeframeToBybit(timeframe string) string {
	switch timeframe {
	case "1m":
		return "1"
	case "3m":
		return "3"
	case "5m":
		return "5"
	case "15m":
		return "15"
	case "30m":
		return "30"
	case "1h":
		return "60"
	case "2h":
		return "120"
	case "4h":
		return "240"
	case "6h":
		return "360"
	case "12h":
		return "720"
	case "1d":
		return "D"
	case "1w":
		return "W"
	case "1M":
		return "M"
	default:
		return "60" // Default to 1h
	}
}

// parseOHLCVResponse parses OHLCV response from exchange
func (s *NativeCCXTService) parseOHLCVResponse(exchange, symbol, timeframe string, body []byte) (*OHLCVResponse, error) {
	switch exchange {
	case "binance":
		return s.parseBinanceOHLCV(symbol, timeframe, body)
	case "bybit":
		return s.parseBybitOHLCV(symbol, timeframe, body)
	case "okx":
		return s.parseOKXOHLCV(symbol, timeframe, body)
	case "bitget":
		return s.parseBitgetOHLCV(symbol, timeframe, body)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// parseBinanceOHLCV parses Binance klines response
func (s *NativeCCXTService) parseBinanceOHLCV(symbol, timeframe string, body []byte) (*OHLCVResponse, error) {
	var raw [][]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Binance OHLCV: %w", err)
	}

	ohlcv := make([]OHLCV, 0, len(raw))
	for _, candle := range raw {
		if len(candle) < 6 {
			continue
		}
		
		timestamp := time.UnixMilli(int64(candle[0].(float64)))
		open, _ := decimal.NewFromString(candle[1].(string))
		high, _ := decimal.NewFromString(candle[2].(string))
		low, _ := decimal.NewFromString(candle[3].(string))
		close, _ := decimal.NewFromString(candle[4].(string))
		volume, _ := decimal.NewFromString(candle[5].(string))

		ohlcv = append(ohlcv, OHLCV{
			Timestamp: timestamp,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		})
	}

	return &OHLCVResponse{
		Exchange:  "binance",
		Symbol:    symbol,
		Timeframe: timeframe,
		OHLCV:     ohlcv,
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// parseBybitOHLCV parses Bybit kline response
func (s *NativeCCXTService) parseBybitOHLCV(symbol, timeframe string, body []byte) (*OHLCVResponse, error) {
	var raw struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List [][]string `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit OHLCV: %w", err)
	}
	if raw.RetCode != 0 {
		return nil, fmt.Errorf("Bybit API error: %s", raw.RetMsg)
	}

	ohlcv := make([]OHLCV, 0, len(raw.Result.List))
	for _, candle := range raw.Result.List {
		if len(candle) < 7 {
			continue
		}

		ts, _ := strconv.ParseInt(candle[0], 10, 64)
		open, _ := decimal.NewFromString(candle[1])
		high, _ := decimal.NewFromString(candle[2])
		low, _ := decimal.NewFromString(candle[3])
		close, _ := decimal.NewFromString(candle[4])
		volume, _ := decimal.NewFromString(candle[5])

		ohlcv = append(ohlcv, OHLCV{
			Timestamp: time.UnixMilli(ts),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		})
	}

	return &OHLCVResponse{
		Exchange:  "bybit",
		Symbol:    symbol,
		Timeframe: timeframe,
		OHLCV:     ohlcv,
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// parseOKXOHLCV parses OKX candles response
func (s *NativeCCXTService) parseOKXOHLCV(symbol, timeframe string, body []byte) (*OHLCVResponse, error) {
	var raw struct {
		Code string     `json:"code"`
		Msg  string     `json:"msg"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse OKX OHLCV: %w", err)
	}
	if raw.Code != "0" {
		return nil, fmt.Errorf("OKX API error: %s", raw.Msg)
	}

	ohlcv := make([]OHLCV, 0, len(raw.Data))
	for _, candle := range raw.Data {
		if len(candle) < 6 {
			continue
		}

		ts, _ := strconv.ParseInt(candle[0], 10, 64)
		open, _ := decimal.NewFromString(candle[1])
		high, _ := decimal.NewFromString(candle[2])
		low, _ := decimal.NewFromString(candle[3])
		close, _ := decimal.NewFromString(candle[4])
		volume, _ := decimal.NewFromString(candle[5])

		ohlcv = append(ohlcv, OHLCV{
			Timestamp: time.UnixMilli(ts),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		})
	}

	return &OHLCVResponse{
		Exchange:  "okx",
		Symbol:    symbol,
		Timeframe: timeframe,
		OHLCV:     ohlcv,
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// parseBitgetOHLCV parses Bitget candles response
func (s *NativeCCXTService) parseBitgetOHLCV(symbol, timeframe string, body []byte) (*OHLCVResponse, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget OHLCV: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("Bitget API error: %s", raw.Msg)
	}

	ohlcv := make([]OHLCV, 0, len(raw.Data))
	for _, candle := range raw.Data {
		if len(candle) < 6 {
			continue
		}

		ts, _ := strconv.ParseInt(candle[0], 10, 64)
		open, _ := decimal.NewFromString(candle[1])
		high, _ := decimal.NewFromString(candle[2])
		low, _ := decimal.NewFromString(candle[3])
		close, _ := decimal.NewFromString(candle[4])
		volume, _ := decimal.NewFromString(candle[5])

		ohlcv = append(ohlcv, OHLCV{
			Timestamp: time.UnixMilli(ts),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		})
	}

	return &OHLCVResponse{
		Exchange:  "bitget",
		Symbol:    symbol,
		Timeframe: timeframe,
		OHLCV:     ohlcv,
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
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
	defer func() { _ = resp.Body.Close() }()

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
	case "bitget":
		return "https://api.bitget.com/api/v2/spot/public/symbols"
		return "https://api.bitget.com/api/v2/public/symbols?productType=USDT-FUTURES"
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
	case "bitget":
		return s.parseBitgetMarkets(body)
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
		return nil, fmt.Errorf("bybit API error")
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

// parseBitgetMarkets parses Bitget markets response
func (s *NativeCCXTService) parseBitgetMarkets(body []byte) ([]string, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol     string `json:"symbol"`
			BaseCoin   string `json:"baseCoin"`
			QuoteCoin  string `json:"quoteCoin"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget markets: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("Bitget API error: %s", raw.Msg)
	}
	var symbols []string
	for _, inst := range raw.Data {
		if inst.Status != "online" || inst.QuoteCoin != "USDT" {
			continue
		}
		// Convert from BTCUSDT to BTC/USDT format
		formatted := inst.BaseCoin + "/" + inst.QuoteCoin
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
			Bids      [][]string `json:"b"`
			Asks      [][]string `json:"a"`
			Timestamp int64      `json:"ts"`
			UpdateID  int64      `json:"u"`
			Sequence  int64      `json:"seq"`
		} `json:"result"`
		RetMsg string `json:"retMsg"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit orderbook: %w", err)
	}
	if raw.RetCode != 0 {
		return nil, fmt.Errorf("bybit API error: %s", raw.RetMsg)
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
	s.mu.RLock()
	conn, ok := s.exchanges[exchange]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("exchange %s not configured with credentials", exchange)
	}

	// Check if API key is configured
	if conn.APIKey == "" || conn.Secret == "" {
		log.Printf("[CCXT Native] No API credentials for %s, returning empty balance", exchange)
		return &BalanceResponse{
			Exchange: exchange,
			Total:    make(map[string]float64),
			Free:     make(map[string]float64),
			Used:     make(map[string]float64),
		}, nil
	}

	switch exchange {
	case "bitget":
		return s.fetchBitgetBalance(ctx, conn)
	case "binance":
		return s.fetchBinanceBalance(ctx, conn)
	case "bybit":
		return s.fetchBybitBalance(ctx, conn)
	case "okx":
		return s.fetchOKXBalance(ctx, conn)
	default:
		return nil, fmt.Errorf("balance fetch not implemented for exchange: %s", exchange)
	}
}

// fetchBitgetBalance fetches balance from Bitget exchange
func (s *NativeCCXTService) fetchBitgetBalance(ctx context.Context, conn *ExchangeConnection) (*BalanceResponse, error) {
	url := "https://api.bitget.com/api/v2/spot/account/balance"
	timestamp := time.Now().UnixMilli()
	method := "GET"
	requestPath := "/api/v2/spot/account/balance"
	body := ""

	// Generate signature
	signString := fmt.Sprintf("%d%s%s%s", timestamp, method, requestPath, body)
	signature := s.generateHMACSignature(conn.Secret, signString)

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACCESS-KEY", conn.APIKey)
	req.Header.Set("ACCESS-SIGN", signature)
	req.Header.Set("ACCESS-TIMESTAMP", fmt.Sprintf("%d", timestamp))
	req.Header.Set("ACCESS-PASSPHRASE", conn.Passphrase)
	req.Header.Set("locale", "en-US")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse Bitget balance response
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Coin []struct {
				Coin    string `json:"coin"`
				Balance string `json:"balance"`
				Available string `json:"available"`
				Frozen   string `json:"frozen"`
				Lock     string `json:"lock"`
			} `json:"coinList"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget balance: %w", err)
	}

	if raw.Code != "00000" {
		return nil, fmt.Errorf("Bitget API error: %s", raw.Msg)
	}

	result := &BalanceResponse{
		Exchange:  "bitget",
		Timestamp: time.Now(),
		Total:     make(map[string]float64),
		Free:      make(map[string]float64),
		Used:      make(map[string]float64),
	}

	for _, balanceData := range raw.Data {
		for _, coin := range balanceData.Coin {
			if coin.Balance == "" || coin.Balance == "0" {
				continue
			}
			total, _ := strconv.ParseFloat(coin.Balance, 64)
			free, _ := strconv.ParseFloat(coin.Available, 64)
			frozen, _ := strconv.ParseFloat(coin.Frozen, 64)
			locked, _ := strconv.ParseFloat(coin.Lock, 64)
			
			result.Total[coin.Coin] = total
			result.Free[coin.Coin] = free
			result.Used[coin.Coin] = frozen + locked
			
			if total > 0 {
				log.Printf("[CCXT Native] Bitget balance: %s = %.8f (free: %.8f)", coin.Coin, total, free)
			}
		}
	}

	log.Printf("[CCXT Native] Bitget balance fetched: %d assets", len(result.Total))
	return result, nil
}

// fetchBinanceBalance fetches balance from Binance exchange
func (s *NativeCCXTService) fetchBinanceBalance(ctx context.Context, conn *ExchangeConnection) (*BalanceResponse, error) {
	 baseURL := "https://api.binance.com"
	if conn.Testnet {
		baseURL = "https://testnet.binance.vision"
	}
	
	endpoint := "/api/v3/account"
	timestamp := time.Now().UnixMilli()
	queryString := fmt.Sprintf("timestamp=%d", timestamp)
	
	// Generate signature
	signature := s.generateHMACSignature(conn.Secret, queryString)
	url := fmt.Sprintf("%s%s?%s&signature=%s", baseURL, endpoint, queryString, signature)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-MBX-APIKEY", conn.APIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse Binance account response
	var raw struct {
		MakerCommission  int64 `json:"makerCommission"`
		TakerCommission  int64 `json:"takerCommission"`
		CanTrade         bool  `json:"canTrade"`
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Binance balance: %w", err)
	}

	result := &BalanceResponse{
		Exchange:  "binance",
		Timestamp: time.Now(),
		Total:     make(map[string]float64),
		Free:      make(map[string]float64),
		Used:      make(map[string]float64),
	}

	for _, balance := range raw.Balances {
		free, _ := strconv.ParseFloat(balance.Free, 64)
		locked, _ := strconv.ParseFloat(balance.Locked, 64)
		total := free + locked
		
		if total > 0 {
			result.Total[balance.Asset] = total
			result.Free[balance.Asset] = free
			result.Used[balance.Asset] = locked
			log.Printf("[CCXT Native] Binance balance: %s = %.8f", balance.Asset, total)
		}
	}

	log.Printf("[CCXT Native] Binance balance fetched: %d assets", len(result.Total))
	return result, nil
}

// fetchBybitBalance fetches balance from Bybit exchange
func (s *NativeCCXTService) fetchBybitBalance(ctx context.Context, conn *ExchangeConnection) (*BalanceResponse, error) {
	baseURL := "https://api.bybit.com"
	if conn.Testnet {
		baseURL = "https://api-testnet.bybit.com"
	}
	
	endpoint := "/v5/account/wallet-balance"
	accountType := "UNIFIED"
	timestamp := time.Now().UnixMilli()
	
	// Generate signature
	signString := fmt.Sprintf("%d%s%s%s", timestamp, conn.APIKey, "5000", accountType)
	signature := s.generateHMACSignature(conn.Secret, signString)
	url := fmt.Sprintf("%s%s?accountType=%s", baseURL, endpoint, accountType)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-BAPI-API-KEY", conn.APIKey)
	req.Header.Set("X-BAPI-SIGN", signature)
	req.Header.Set("X-BAPI-TIMESTAMP", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-BAPI-RECV-WINDOW", "5000")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse Bybit response
	var raw struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				AccountType string `json:"accountType"`
				Coin []struct {
					Coin       string `json:"coin"`
					WalletBalance string `json:"walletBalance"`
					AvailableToWithdraw string `json:"availableToWithdraw"`
					TotalEquity string `json:"totalEquity"`
				} `json:"coin"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit balance: %w", err)
	}

	if raw.RetCode != 0 {
		return nil, fmt.Errorf("Bybit API error: %s", raw.RetMsg)
	}

	result := &BalanceResponse{
		Exchange:  "bybit",
		Timestamp: time.Now(),
		Total:     make(map[string]float64),
		Free:      make(map[string]float64),
		Used:      make(map[string]float64),
	}

	for _, account := range raw.Result.List {
		for _, coin := range account.Coin {
			balance, _ := strconv.ParseFloat(coin.WalletBalance, 64)
			available, _ := strconv.ParseFloat(coin.AvailableToWithdraw, 64)
			
			if balance > 0 {
				result.Total[coin.Coin] = balance
				result.Free[coin.Coin] = available
				result.Used[coin.Coin] = balance - available
				log.Printf("[CCXT Native] Bybit balance: %s = %.8f", coin.Coin, balance)
			}
		}
	}

	log.Printf("[CCXT Native] Bybit balance fetched: %d assets", len(result.Total))
	return result, nil
}

// fetchOKXBalance fetches balance from OKX exchange
func (s *NativeCCXTService) fetchOKXBalance(ctx context.Context, conn *ExchangeConnection) (*BalanceResponse, error) {
	baseURL := "https://www.okx.com"
	if conn.Testnet {
		baseURL = "https://www.okx.com" // OKX uses same URL with different headers for testnet
	}
	
	endpoint := "/api/v5/account/balance"
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.999Z")
	method := "GET"
	body := ""
	
	// Generate signature
	signString := timestamp + method + endpoint + body
	signature := s.generateBase64HMACSignature(conn.Secret, signString)
	url := baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OK-ACCESS-KEY", conn.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", conn.Passphrase)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse OKX response
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			TotalEq string `json:"totalEq"`
			Details []struct {
				Ccy       string `json:"ccy"`
				Bal       string `json:"bal"`
				AvailBal  string `json:"availBal"`
				FutIsoBal string `json:"futIsoBal"`
			} `json:"details"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse OKX balance: %w", err)
	}

	if raw.Code != "0" {
		return nil, fmt.Errorf("OKX API error: %s", raw.Msg)
	}

	result := &BalanceResponse{
		Exchange:  "okx",
		Timestamp: time.Now(),
		Total:     make(map[string]float64),
		Free:      make(map[string]float64),
		Used:      make(map[string]float64),
	}

	for _, balanceData := range raw.Data {
		for _, detail := range balanceData.Details {
			balance, _ := strconv.ParseFloat(detail.Bal, 64)
			available, _ := strconv.ParseFloat(detail.AvailBal, 64)
			
			if balance > 0 {
				result.Total[detail.Ccy] = balance
				result.Free[detail.Ccy] = available
				result.Used[detail.Ccy] = balance - available
				log.Printf("[CCXT Native] OKX balance: %s = %.8f", detail.Ccy, balance)
			}
		}
	}

	log.Printf("[CCXT Native] OKX balance fetched: %d assets", len(result.Total))
	return result, nil
}

// generateHMACSignature generates an HMAC-SHA256 signature encoded as hex
func (s *NativeCCXTService) generateHMACSignature(secret, message string) string {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// generateBase64HMACSignature generates an HMAC-SHA256 signature encoded as base64
func (s *NativeCCXTService) generateBase64HMACSignature(secret, message string) string {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (s *NativeCCXTService) FetchFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error) {
	rates, err := s.FetchFundingRates(ctx, exchange, []string{symbol})
	if err != nil {
		return nil, err
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("no funding rate data for %s on %s", symbol, exchange)
	}
	return &rates[0], nil
}

func (s *NativeCCXTService) FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
	// Build URL for funding rates
	url := s.buildFundingRateURL(exchange, symbols)
	if url == "" {
		return []FundingRate{}, nil // Not supported, return empty
	}

	s.rateLimiter.Wait()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NeuraTrade/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch funding rates: %w", err)
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

	return s.parseFundingRateResponse(exchange, body)
}

func (s *NativeCCXTService) FetchAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error) {
	// Fetch all funding rates without symbol filter
	url := s.buildAllFundingRateURL(exchange)
	if url == "" {
		return []FundingRate{}, nil // Not supported
	}

	s.rateLimiter.Wait()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NeuraTrade/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch all funding rates: %w", err)
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

	return s.parseFundingRateResponse(exchange, body)
}

// buildFundingRateURL builds the funding rate URL for specific symbols
func (s *NativeCCXTService) buildFundingRateURL(exchange string, symbols []string) string {
	switch exchange {
	case "binance":
		// Binance: /fapi/v1/fundingRate
		if len(symbols) == 0 {
			return "https://fapi.binance.com/fapi/v1/fundingRate"
		}
		symbol := strings.ReplaceAll(symbols[0], "/", "")
		return fmt.Sprintf("https://fapi.binance.com/fapi/v1/fundingRate?symbol=%s", symbol)
	case "bybit":
		// Bybit: /v5/market/funding/history
		if len(symbols) == 0 {
			return "https://api.bybit.com/v5/market/funding/history?category=linear"
		}
		symbol := strings.ReplaceAll(symbols[0], "/", "")
		return fmt.Sprintf("https://api.bybit.com/v5/market/funding/history?category=linear&symbol=%s", symbol)
	case "okx":
		// OKX: /api/v5/public/funding-rate
		if len(symbols) == 0 {
			return "https://www.okx.com/api/v5/public/funding-rate?instType=SWAP"
		}
		instId := strings.ReplaceAll(symbols[0], "/", "-")
		return fmt.Sprintf("https://www.okx.com/api/v5/public/funding-rate?instId=%s", instId)
	case "bitget":
		// Bitget: /api/v2/mix/market/funding-rate
		if len(symbols) == 0 {
			return "https://api.bitget.com/api/v2/mix/market/funding-rate?productType=USDT-FUTURES"
		}
		symbol := strings.ReplaceAll(symbols[0], "/", "")
		return fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/funding-rate?symbol=%s&productType=USDT-FUTURES", symbol)
	default:
		return ""
	}
}

// buildAllFundingRateURL builds URL for fetching all funding rates
func (s *NativeCCXTService) buildAllFundingRateURL(exchange string) string {
	return s.buildFundingRateURL(exchange, nil)
}

// parseFundingRateResponse parses funding rate response
func (s *NativeCCXTService) parseFundingRateResponse(exchange string, body []byte) ([]FundingRate, error) {
	switch exchange {
	case "binance":
		return s.parseBinanceFundingRate(body)
	case "bybit":
		return s.parseBybitFundingRate(body)
	case "okx":
		return s.parseOKXFundingRate(body)
	case "bitget":
		return s.parseBitgetFundingRate(body)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// parseBinanceFundingRate parses Binance funding rate response
func (s *NativeCCXTService) parseBinanceFundingRate(body []byte) ([]FundingRate, error) {
	var raw []struct {
		Symbol        string `json:"symbol"`
		FundingTime   int64  `json:"fundingTime"`
		FundingRate   string `json:"fundingRate"`
		MarkPrice     string `json:"markPrice,omitempty"`
		IndexPrice    string `json:"indexPrice,omitempty"`
		NextFundingTime int64 `json:"nextFundingTime,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Binance funding rate: %w", err)
	}

	rates := make([]FundingRate, 0, len(raw))
	for _, item := range raw {
		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		markPrice, _ := strconv.ParseFloat(item.MarkPrice, 64)
		indexPrice, _ := strconv.ParseFloat(item.IndexPrice, 64)

		rates = append(rates, FundingRate{
			Symbol:           item.Symbol,
			FundingRate:       rate,
			FundingTimestamp:  UnixTimestamp(time.UnixMilli(item.FundingTime)),
			NextFundingTime:   UnixTimestamp(time.UnixMilli(item.NextFundingTime)),
			MarkPrice:         markPrice,
			IndexPrice:        indexPrice,
			Timestamp:         UnixTimestamp(time.Now()),
		})
	}
	return rates, nil
}

// parseBybitFundingRate parses Bybit funding rate response
func (s *NativeCCXTService) parseBybitFundingRate(body []byte) ([]FundingRate, error) {
	var raw struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol               string `json:"symbol"`
				FundingRate          string `json:"fundingRate"`
				FundingRateTimestamp string `json:"fundingRateTimestamp"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit funding rate: %w", err)
	}
	if raw.RetCode != 0 {
		return nil, fmt.Errorf("Bybit API error: %s", raw.RetMsg)
	}

	rates := make([]FundingRate, 0, len(raw.Result.List))
	for _, item := range raw.Result.List {
		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		ts, _ := strconv.ParseInt(item.FundingRateTimestamp, 10, 64)

		rates = append(rates, FundingRate{
			Symbol:          item.Symbol,
			FundingRate:      rate,
			FundingTimestamp: UnixTimestamp(time.UnixMilli(ts)),
			Timestamp:        UnixTimestamp(time.Now()),
		})
	}
	return rates, nil
}

// parseOKXFundingRate parses OKX funding rate response
func (s *NativeCCXTService) parseOKXFundingRate(body []byte) ([]FundingRate, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstId        string `json:"instId"`
			FundingRate   string `json:"fundingRate"`
			FundingTime   string `json:"fundingTime"`
			NextFundingRate string `json:"nextFundingRate"`
			NextFundingTime string `json:"nextFundingTime"`
			MarkPx        string `json:"markPx"`
			IdxPx         string `json:"idxPx"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse OKX funding rate: %w", err)
	}
	if raw.Code != "0" {
		return nil, fmt.Errorf("OKX API error: %s", raw.Msg)
	}

	rates := make([]FundingRate, 0, len(raw.Data))
	for _, item := range raw.Data {
		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		ts, _ := strconv.ParseInt(item.FundingTime, 10, 64)
		nextTs, _ := strconv.ParseInt(item.NextFundingTime, 10, 64)
		markPrice, _ := strconv.ParseFloat(item.MarkPx, 64)
		indexPrice, _ := strconv.ParseFloat(item.IdxPx, 64)

		rates = append(rates, FundingRate{
			Symbol:           item.InstId,
			FundingRate:       rate,
			FundingTimestamp:  UnixTimestamp(time.UnixMilli(ts)),
			NextFundingTime:   UnixTimestamp(time.UnixMilli(nextTs)),
			MarkPrice:         markPrice,
			IndexPrice:        indexPrice,
			Timestamp:         UnixTimestamp(time.Now()),
		})
	}
	return rates, nil
}

// parseBitgetFundingRate parses Bitget funding rate response
func (s *NativeCCXTService) parseBitgetFundingRate(body []byte) ([]FundingRate, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol         string `json:"symbol"`
			FundingRate    string `json:"fundingRate"`
			FundingTime    string `json:"fundingTime"`
			NextFundingTime string `json:"nextFundingTime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget funding rate: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("Bitget API error: %s", raw.Msg)
	}

	rates := make([]FundingRate, 0, len(raw.Data))
	for _, item := range raw.Data {
		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		ts, _ := strconv.ParseInt(item.FundingTime, 10, 64)
		nextTs, _ := strconv.ParseInt(item.NextFundingTime, 10, 64)

		rates = append(rates, FundingRate{
			Symbol:           item.Symbol,
			FundingRate:       rate,
			FundingTimestamp:  UnixTimestamp(time.UnixMilli(ts)),
			NextFundingTime:   UnixTimestamp(time.UnixMilli(nextTs)),
			Timestamp:         UnixTimestamp(time.Now()),
		})
	}
	return rates, nil
}

func (s *NativeCCXTService) CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	return []models.ArbitrageOpportunityResponse{}, nil
}

func (s *NativeCCXTService) CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]FundingArbitrageOpportunity, error) {
	return []FundingArbitrageOpportunity{}, nil
}

// parseBitgetOrderBook parses Bitget orderbook response
func (s *NativeCCXTService) parseBitgetOrderBook(symbol string, body []byte, limit int) (*OrderBookResponse, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Bids      [][]string `json:"bids"`
			Asks      [][]string `json:"asks"`
			Timestamp string     `json:"ts"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget orderbook: %w", err)
	}

	if raw.Code != "00000" {
		return nil, fmt.Errorf("Bitget API error: %s", raw.Msg)
	}

	bids := make([]OrderBookEntry, 0, len(raw.Data.Bids))
	for _, bid := range raw.Data.Bids {
		if len(bid) >= 2 {
			bids = append(bids, OrderBookEntry{
				Price:  parseDecimal(bid[0]),
				Amount: parseDecimal(bid[1]),
			})
		}
	}

	asks := make([]OrderBookEntry, 0, len(raw.Data.Asks))
	for _, ask := range raw.Data.Asks {
		if len(ask) >= 2 {
			asks = append(asks, OrderBookEntry{
				Price:  parseDecimal(ask[0]),
				Amount: parseDecimal(ask[1]),
			})
		}
	}

	ts, _ := strconv.ParseInt(raw.Data.Timestamp, 10, 64)

	return &OrderBookResponse{
		Exchange: "bitget",
		Symbol:   symbol,
		OrderBook: OrderBook{
			Symbol:    symbol,
			Bids:      bids,
			Asks:      asks,
			Timestamp: time.UnixMilli(ts),
		},
	}, nil
}
