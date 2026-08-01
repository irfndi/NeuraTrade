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
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

// parseDecimal safely parses a string to decimal, returning zero on error
func parseDecimal(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

func closeBody(body io.Closer) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil {
		zaplogrus.Warnf("[CCXT Native] failed to close response body: %v", err)
	}
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

func (r *rateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	elapsed := time.Since(r.lastCall)
	if elapsed < r.minDelay {
		delay := r.minDelay - elapsed
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.lastCall = time.Now()
	return nil
}

// NativeCCXTService implements CCXTService using direct exchange API calls
type NativeCCXTService struct {
	mu            sync.RWMutex
	httpClient    *http.Client
	exchanges     map[string]*ExchangeConnection
	credentials   map[string]config.ExchangeCredentials
	timeout       time.Duration
	retryAttempts int
	rateLimiter   *rateLimiter
	// Scalping fallback controls are loaded once at construction for deterministic behavior.
	fallbackMaxSymbolsPerCycle int
	fallbackCycleBudget        time.Duration
	fallbackPerSymbolTimeout   time.Duration
}

const (
	defaultFallbackMaxSymbolsPerCycle = 32
	defaultFallbackCycleBudget        = 4 * time.Second
	defaultFallbackPerSymbolTimeout   = 900 * time.Millisecond
)

// PartialMarketDataError indicates that market data fetch returned only a subset of requested data.
type PartialMarketDataError struct {
	Data   []MarketPriceInterface
	Reason string
}

func (e *PartialMarketDataError) Error() string {
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "partial market data"
	}
	return reason
}

type fallbackCycleLimits struct {
	maxSymbols       int
	cycleBudget      time.Duration
	perSymbolTimeout time.Duration
	cycleStarted     time.Time
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
	fallbackCfg := resolveScalpingFallbackConfigFromEnv()

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
		exchanges:                  make(map[string]*ExchangeConnection),
		credentials:                make(map[string]config.ExchangeCredentials),
		timeout:                    timeout,
		retryAttempts:              retryAttempts,
		fallbackMaxSymbolsPerCycle: fallbackCfg.maxSymbolsPerCycle,
		fallbackCycleBudget:        fallbackCfg.cycleBudget,
		fallbackPerSymbolTimeout:   fallbackCfg.perSymbolTimeout,
	}
}

type scalpingFallbackConfig struct {
	maxSymbolsPerCycle int
	cycleBudget        time.Duration
	perSymbolTimeout   time.Duration
}

func resolveScalpingFallbackConfigFromEnv() scalpingFallbackConfig {
	maxSymbols := readPositiveIntEnv(
		"NEURATRADE_SCALPING_FALLBACK_MAX_SYMBOLS_PER_CYCLE",
		defaultFallbackMaxSymbolsPerCycle,
	)
	cycleBudgetMS := readPositiveIntEnv(
		"NEURATRADE_SCALPING_FALLBACK_CYCLE_BUDGET_MS",
		int(defaultFallbackCycleBudget.Milliseconds()),
	)
	perSymbolTimeoutMS := readPositiveIntEnv(
		"NEURATRADE_SCALPING_FALLBACK_PER_SYMBOL_TIMEOUT_MS",
		int(defaultFallbackPerSymbolTimeout.Milliseconds()),
	)

	return scalpingFallbackConfig{
		maxSymbolsPerCycle: maxSymbols,
		cycleBudget:        time.Duration(cycleBudgetMS) * time.Millisecond,
		perSymbolTimeout:   time.Duration(perSymbolTimeoutMS) * time.Millisecond,
	}
}

func readPositiveIntEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// NewNativeCCXTServiceWithConfig creates a native CCXT service with exchange credentials from config
func NewNativeCCXTServiceWithConfig(timeout time.Duration, retryAttempts int, exchangeCreds map[string]config.ExchangeCredentials) *NativeCCXTService {
	s := NewNativeCCXTService(timeout, retryAttempts)

	// Populate exchange credentials from config
	for name, creds := range exchangeCreds {
		baseURL, ok := s.getExchangeBaseURL(name)
		if !ok {
			zaplogrus.Infof("[CCXT Native] Unknown exchange in config: %s", name)
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
		s.credentials[name] = creds
		zaplogrus.Infof("[CCXT Native] Configured exchange: %s with API key", name)
	}
	return s
}

// Initialize prepares the service for use
func (s *NativeCCXTService) Initialize(ctx context.Context) error {
	zaplogrus.Info("[CCXT Native] Initializing native exchange connections")

	// Initialize default exchanges
	defaultExchanges := []string{"binance", "bybit", "okx", "bitget"}

	for _, exchange := range defaultExchanges {
		if err := s.initializeExchange(exchange); err != nil {
			zaplogrus.Warnf("[CCXT Native] Failed to initialize %s: %v", exchange, err)
			continue
		}
	}

	zaplogrus.Infof("[CCXT Native] Initialized %d exchanges", len(s.exchanges))
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

	existing, hadExisting := s.exchanges[exchangeID]
	connection := &ExchangeConnection{
		Name:       exchangeID,
		BaseURL:    baseURL,
		LastUpdate: time.Now(),
	}
	if hadExisting && existing != nil {
		connection.APIKey = existing.APIKey
		connection.Secret = existing.Secret
		connection.Passphrase = existing.Passphrase
		connection.Testnet = existing.Testnet
	}

	s.exchanges[exchangeID] = connection

	if connection.APIKey != "" && connection.Secret != "" {
		zaplogrus.Infof("[CCXT Native] Initialized exchange: %s (%s) with configured credentials", exchangeID, baseURL)
	} else {
		zaplogrus.Infof("[CCXT Native] Initialized exchange: %s (%s) without API credentials", exchangeID, baseURL)
	}
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
	zaplogrus.Info("[CCXT Native] Service closed")
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

	// Normalize symbol for API calls.
	apiSymbol := strings.ReplaceAll(symbol, "/", "")
	if exchange == "okx" {
		apiSymbol = okxSwapInstrumentID(symbol)
	}

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
		return fmt.Sprintf("https://www.okx.com/api/v5/market/ticker?instId=%s", okxSwapInstrumentID(symbol))
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
	default:
		return ""
	}
}

func okxSwapInstrumentID(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}
	if idx := strings.Index(normalized, ":"); idx >= 0 {
		normalized = normalized[:idx]
	}
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, "/", "-")
	if strings.HasSuffix(normalized, "-SWAP") {
		return normalized
	}
	if strings.HasSuffix(normalized, "USDT") && !strings.Contains(normalized, "-") {
		normalized = strings.TrimSuffix(normalized, "USDT") + "-USDT"
	}
	if strings.HasSuffix(normalized, "-USDT") {
		return normalized + "-SWAP"
	}
	return normalized
}

// buildOrderBookURL builds the orderbook URL for an exchange
func (s *NativeCCXTService) buildOrderBookURL(exchange, symbol, apiSymbol, okxSymbol string, limit int) string {
	switch exchange {
	case "binance":
		return fmt.Sprintf("https://api.binance.com/api/v3/depth?symbol=%s&limit=%d", apiSymbol, limit)
	case "bybit":
		return fmt.Sprintf("https://api.bybit.com/v5/market/orderbook?category=linear&symbol=%s&limit=%d", apiSymbol, limit)
	case "okx":
		return fmt.Sprintf("https://www.okx.com/api/v5/market/books?instId=%s&sz=%d", okxSwapInstrumentID(okxSymbol), limit)
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
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
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
		Symbol:     symbol,
		Last:       parseDecimal(raw.LastPrice),
		Bid:        parseDecimal(raw.BidPrice),
		Ask:        parseDecimal(raw.AskPrice),
		High:       parseDecimal(raw.High24h),
		Low:        parseDecimal(raw.Low24h),
		Volume:     parseDecimal(raw.Volume24h),
		Open:       parseDecimal(raw.OpenPrice),
		Close:      parseDecimal(raw.PrevClosePrice),
		Percentage: calculateTickerPercentage(parseDecimal(raw.LastPrice), parseDecimal(raw.OpenPrice)),
		Timestamp:  UnixTimestamp(time.Now()),
	}, nil
}

// parseBybitTicker parses Bybit ticker response
func (s *NativeCCXTService) parseBybitTicker(symbol string, body []byte) (*Ticker, error) {
	var raw struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol       string `json:"symbol"`
				LastPrice    string `json:"lastPrice"`
				Bid1Price    string `json:"bid1Price"`
				Ask1Price    string `json:"ask1Price"`
				High24h      string `json:"highPrice24h"`
				Low24h       string `json:"lowPrice24h"`
				Volume24h    string `json:"volume24h"`
				Price24hPcnt string `json:"price24hPcnt"`
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
		Symbol:     t.Symbol,
		Last:       parseDecimal(t.LastPrice),
		Bid:        parseDecimal(t.Bid1Price),
		Ask:        parseDecimal(t.Ask1Price),
		High:       parseDecimal(t.High24h),
		Low:        parseDecimal(t.Low24h),
		Volume:     parseDecimal(t.Volume24h),
		Percentage: parseDecimal(t.Price24hPcnt).Mul(decimal.NewFromInt(100)),
		Timestamp:  UnixTimestamp(time.Now()),
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
			Open24h string `json:"open24h"`
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
		Symbol:     symbol,
		Last:       parseDecimal(t.Last),
		Bid:        parseDecimal(t.BidPx),
		Ask:        parseDecimal(t.AskPx),
		Open:       parseDecimal(t.Open24h),
		High:       parseDecimal(t.High24h),
		Low:        parseDecimal(t.Low24h),
		Volume:     parseDecimal(t.Vol24h),
		Percentage: calculateTickerPercentage(parseDecimal(t.Last), parseDecimal(t.Open24h)),
		Timestamp:  UnixTimestamp(time.Now()),
	}, nil
}

func (s *NativeCCXTService) parseBitgetTicker(symbol string, body []byte) (*Ticker, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol     string `json:"symbol"`
			LastPr     string `json:"lastPr"`
			BidPr      string `json:"bidPr"`
			AskPr      string `json:"askPr"`
			High24h    string `json:"high24h"`
			Low24h     string `json:"low24h"`
			BaseVolume string `json:"baseVolume"`
			Change24h  string `json:"change24h"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget response: %w", err)
	}

	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", raw.Msg)
	}

	if len(raw.Data) == 0 {
		return nil, fmt.Errorf("no ticker data for %s", symbol)
	}

	t := raw.Data[0]
	return &Ticker{
		Symbol:     symbol,
		Last:       parseDecimal(t.LastPr),
		Bid:        parseDecimal(t.BidPr),
		Ask:        parseDecimal(t.AskPr),
		High:       parseDecimal(t.High24h),
		Low:        parseDecimal(t.Low24h),
		Volume:     parseDecimal(t.BaseVolume),
		Percentage: parseDecimal(t.Change24h),
		Timestamp:  UnixTimestamp(time.Now()),
	}, nil
}

type TickerMarketPriceAdapter struct {
	data *TickerData
}

func (a *TickerMarketPriceAdapter) GetPrice() decimal.Decimal {
	return a.data.Ticker.Last
}
func (a *TickerMarketPriceAdapter) GetVolume() decimal.Decimal {
	return a.data.Ticker.Volume
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
func (a *TickerMarketPriceAdapter) GetBid() decimal.Decimal {
	return a.data.Ticker.Bid
}
func (a *TickerMarketPriceAdapter) GetAsk() decimal.Decimal {
	return a.data.Ticker.Ask
}
func (a *TickerMarketPriceAdapter) GetHigh() decimal.Decimal {
	return a.data.Ticker.High
}
func (a *TickerMarketPriceAdapter) GetLow() decimal.Decimal {
	return a.data.Ticker.Low
}
func (a *TickerMarketPriceAdapter) GetPriceChange24h() float64 {
	v, _ := a.data.Ticker.Percentage.Float64()
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
	limits := s.newFallbackCycleLimits(len(symbols))
	fallbackFetches := 0

	for _, exchange := range exchanges {
		if err := ctx.Err(); err != nil {
			if len(allTickers) > 0 {
				return allTickers, err
			}
			return nil, err
		}

		if err := s.processExchange(ctx, exchange, symbols, &allTickers, &fallbackFetches, limits); err != nil {
			if len(allTickers) > 0 {
				return allTickers, err
			}
			return nil, err
		}
	}

	return allTickers, nil
}

func (s *NativeCCXTService) newFallbackCycleLimits(symbolCount int) fallbackCycleLimits {
	return fallbackCycleLimits{
		maxSymbols:       s.fallbackMaxSymbolsForCycle(symbolCount),
		cycleBudget:      s.getFallbackCycleBudget(),
		perSymbolTimeout: s.getFallbackPerSymbolTimeout(),
		cycleStarted:     time.Now(),
	}
}

func (s *NativeCCXTService) processExchange(
	ctx context.Context,
	exchange string,
	symbols []string,
	allTickers *[]MarketPriceInterface,
	fallbackFetches *int,
	limits fallbackCycleLimits,
) error {
	// Bitget supports bulk spot ticker fetch; use it to avoid per-symbol request storms.
	if exchange == "bitget" && len(symbols) > 1 {
		bulkTickers, err := s.fetchBitgetBulkTickers(ctx, symbols)
		if err == nil {
			*allTickers = append(*allTickers, bulkTickers...)
			return nil
		}
		zaplogrus.Warnf("[CCXT Native] Failed bulk ticker fetch for bitget: %v", err)
	}

	for _, symbol := range symbols {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.checkFallbackLimits(*allTickers, *fallbackFetches, limits); err != nil {
			return err
		}

		ticker, err := s.fetchSymbolWithTimeout(ctx, exchange, symbol, limits.perSymbolTimeout)
		*fallbackFetches = *fallbackFetches + 1
		if err != nil {
			zaplogrus.Warnf("[CCXT Native] Failed to fetch %s:%s: %v", exchange, symbol, err)
			continue
		}
		*allTickers = append(*allTickers, ticker)
	}

	return nil
}

func (s *NativeCCXTService) fetchSymbolWithTimeout(
	ctx context.Context,
	exchange string,
	symbol string,
	perSymbolTimeout time.Duration,
) (MarketPriceInterface, error) {
	tickerCtx := ctx
	cancel := func() {}
	if perSymbolTimeout > 0 {
		tickerCtx, cancel = context.WithTimeout(ctx, perSymbolTimeout)
	}
	defer cancel()
	return s.FetchSingleTicker(tickerCtx, exchange, symbol)
}

func (s *NativeCCXTService) checkFallbackLimits(
	allTickers []MarketPriceInterface,
	fallbackFetches int,
	limits fallbackCycleLimits,
) error {
	if limits.maxSymbols > 0 && fallbackFetches >= limits.maxSymbols {
		reason := fmt.Sprintf("fallback ticker fetch budget reached (%d symbols)", limits.maxSymbols)
		zaplogrus.Infof("[CCXT Native] %s, returning partial market data", reason)
		return &PartialMarketDataError{
			Data:   append([]MarketPriceInterface(nil), allTickers...),
			Reason: reason,
		}
	}
	if limits.cycleBudget > 0 && time.Since(limits.cycleStarted) >= limits.cycleBudget {
		reason := fmt.Sprintf("fallback cycle budget exceeded (%s)", limits.cycleBudget)
		zaplogrus.Infof("[CCXT Native] %s, returning partial market data", reason)
		return &PartialMarketDataError{
			Data:   append([]MarketPriceInterface(nil), allTickers...),
			Reason: reason,
		}
	}
	return nil
}

func (s *NativeCCXTService) fallbackMaxSymbolsForCycle(symbolCount int) int {
	maxSymbols := s.fallbackMaxSymbolsPerCycle
	if maxSymbols <= 0 {
		maxSymbols = defaultFallbackMaxSymbolsPerCycle
	}
	if symbolCount > 0 && maxSymbols > symbolCount {
		return symbolCount
	}
	return maxSymbols
}

func (s *NativeCCXTService) getFallbackCycleBudget() time.Duration {
	if s.fallbackCycleBudget <= 0 {
		return defaultFallbackCycleBudget
	}
	return s.fallbackCycleBudget
}

func (s *NativeCCXTService) getFallbackPerSymbolTimeout() time.Duration {
	if s.fallbackPerSymbolTimeout <= 0 {
		return defaultFallbackPerSymbolTimeout
	}
	return s.fallbackPerSymbolTimeout
}

func (s *NativeCCXTService) fetchBitgetBulkTickers(ctx context.Context, symbols []string) ([]MarketPriceInterface, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.bitget.com/api/v2/spot/market/tickers", nil)
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

	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol     string `json:"symbol"`
			LastPr     string `json:"lastPr"`
			BidPr      string `json:"bidPr"`
			AskPr      string `json:"askPr"`
			High24h    string `json:"high24h"`
			Low24h     string `json:"low24h"`
			BaseVolume string `json:"baseVolume"`
			Change24h  string `json:"change24h"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget bulk ticker response: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", raw.Msg)
	}

	wanted := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		wanted[bitgetSymbolKey(symbol)] = struct{}{}
	}

	result := make([]MarketPriceInterface, 0, len(symbols))
	for _, ticker := range raw.Data {
		if _, ok := wanted[bitgetSymbolKey(ticker.Symbol)]; !ok {
			continue
		}
		formattedSymbol := normalizeBitgetSpotSymbol(ticker.Symbol)
		result = append(result, &TickerMarketPriceAdapter{
			data: &TickerData{
				Exchange: "bitget",
				Ticker: Ticker{
					Symbol:     formattedSymbol,
					Last:       parseDecimal(ticker.LastPr),
					Bid:        parseDecimal(ticker.BidPr),
					Ask:        parseDecimal(ticker.AskPr),
					High:       parseDecimal(ticker.High24h),
					Low:        parseDecimal(ticker.Low24h),
					Volume:     parseDecimal(ticker.BaseVolume),
					Percentage: parseDecimal(ticker.Change24h),
					Timestamp:  UnixTimestamp(time.Now()),
				},
			},
		})
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no matching bitget tickers found for requested symbols")
	}
	return result, nil
}

func bitgetSymbolKey(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if idx := strings.Index(normalized, ":"); idx >= 0 {
		normalized = normalized[:idx]
	}
	if idx := strings.Index(normalized, "_"); idx >= 0 {
		normalized = normalized[:idx]
	}
	normalized = strings.ReplaceAll(normalized, "/", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	return normalized
}

func calculateTickerPercentage(last decimal.Decimal, reference decimal.Decimal) decimal.Decimal {
	if !last.GreaterThan(decimal.Zero) || !reference.GreaterThan(decimal.Zero) {
		return decimal.Zero
	}
	return last.Sub(reference).Div(reference).Mul(decimal.NewFromInt(100))
}

func normalizeBitgetSpotSymbol(symbol string) string {
	normalized := bitgetSymbolKey(symbol)
	if strings.HasSuffix(normalized, "USDT") && len(normalized) > len("USDT") {
		base := normalized[:len(normalized)-len("USDT")]
		return base + "/USDT"
	}
	return normalized
}

func (s *NativeCCXTService) FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error) {
	// Check if exchange is initialized
	s.mu.RLock()
	_, ok := s.exchanges[exchange]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("exchange %s not initialized", exchange)
	}

	apiSymbol := strings.ReplaceAll(symbol, "/", "")
	okxSymbol := strings.ReplaceAll(symbol, "/", "-")
	url := s.buildOrderBookURL(exchange, symbol, apiSymbol, okxSymbol, limit)
	if url == "" {
		return nil, fmt.Errorf("orderbook endpoint not supported for %s", exchange)
	}

	// Rate limit API calls
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
	}

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
	ob, err := s.FetchOrderBook(ctx, exchange, symbol, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch order book for metrics: %w", err)
	}
	if ob == nil || len(ob.OrderBook.Bids) == 0 || len(ob.OrderBook.Asks) == 0 {
		return nil, fmt.Errorf("empty order book for %s:%s", exchange, symbol)
	}
	client := &Client{}
	return client.CalculateOrderBookMetrics(ob), nil
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
	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
	}

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
	defer closeBody(resp.Body)

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

// FetchOHLCVPaginated fetches OHLCV data across a time range by paginating
// through the exchange API. Binance returns at most 1000 candles per request;
// this method loops from startTime to endTime, advancing the cursor after
// each batch until the full range is covered.
func (s *NativeCCXTService) FetchOHLCVPaginated(
	ctx context.Context,
	exchange, symbol, timeframe string,
	startTime, endTime time.Time,
) (*OHLCVResponse, error) {
	s.mu.RLock()
	_, ok := s.exchanges[exchange]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("exchange %s not initialized", exchange)
	}

	const pageSize = 1000

	apiSymbol := strings.ReplaceAll(symbol, "/", "")
	allOHLCV := make([]OHLCV, 0, 1024)
	cursor := startTime

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("paginated OHLCV fetch cancelled: %w", err)
		}

		url := s.buildOHLCVURLWithTime(exchange, apiSymbol, timeframe, pageSize, cursor, endTime)
		if url == "" {
			return nil, fmt.Errorf("OHLCV endpoint not supported for %s", exchange)
		}

		if err := s.rateLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", "NeuraTrade/1.0")
		req.Header.Set("Accept", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch OHLCV page: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		closeBody(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read OHLCV body: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
		}

		batch, err := s.parseOHLCVResponse(exchange, symbol, timeframe, body)
		if err != nil {
			return nil, fmt.Errorf("parse OHLCV page: %w", err)
		}

		if len(batch.OHLCV) == 0 {
			break
		}

		allOHLCV = append(allOHLCV, batch.OHLCV...)

		// Advance cursor: use the last candle timestamp + 1ms
		lastTS := batch.OHLCV[len(batch.OHLCV)-1].Timestamp
		nextStart := lastTS.Add(time.Millisecond)

		// If we got fewer than a full page or we've passed endTime, we're done
		if len(batch.OHLCV) < pageSize || !nextStart.Before(endTime) {
			break
		}
		cursor = nextStart
	}

	return &OHLCVResponse{
		Exchange:  exchange,
		Symbol:    symbol,
		Timeframe: timeframe,
		OHLCV:     allOHLCV,
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// buildOHLCVURLWithTime builds the OHLCV URL with optional startTime/endTime
// for paginated historical data fetching. Times are converted to millisecond
// timestamps as required by exchange APIs.
func (s *NativeCCXTService) buildOHLCVURLWithTime(exchange, symbol, timeframe string, limit int, startTime, endTime time.Time) string {
	startMs := startTime.UnixMilli()
	endMs := endTime.UnixMilli()

	switch exchange {
	case "binance":
		return fmt.Sprintf(
			"https://api.binance.com/api/v3/klines?symbol=%s&interval=%s&limit=%d&startTime=%d&endTime=%d",
			symbol, timeframe, limit, startMs, endMs,
		)
	case "bybit":
		interval := s.convertTimeframeToBybit(timeframe)
		return fmt.Sprintf(
			"https://api.bybit.com/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d&start=%d&end=%d",
			symbol, interval, limit, startMs, endMs,
		)
	case "okx":
		return fmt.Sprintf(
			"https://www.okx.com/api/v5/market/candles?instId=%s&bar=%s&limit=%d&after=%d&before=%d",
			okxSwapInstrumentID(symbol), timeframe, limit, endMs, startMs,
		)
	case "bitget":
		return fmt.Sprintf(
			"https://www.bitget.com/api/v2/spot/market/candles?symbol=%s&granularity=%s&limit=%d&startTime=%d&endTime=%d",
			symbol, timeframe, limit, startMs, endMs,
		)
	default:
		return ""
	}
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
		return fmt.Sprintf("https://www.okx.com/api/v5/market/candles?instId=%s&bar=%s&limit=%d", okxSwapInstrumentID(symbol), timeframe, limit)
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
		return nil, fmt.Errorf("bybit API error: %s", raw.RetMsg)
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
		Code string     `json:"code"`
		Msg  string     `json:"msg"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget OHLCV: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", raw.Msg)
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
			InstId    string `json:"instId"`
			BaseCcy   string `json:"baseCcy"`
			QuoteCcy  string `json:"quoteCcy"`
			CtValCcy  string `json:"ctValCcy"`
			SettleCcy string `json:"settleCcy"`
			State     string `json:"state"`
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
		instID := strings.ToUpper(strings.TrimSpace(inst.InstId))
		if inst.State != "live" || !isOKXUSDTInstrument(instID, inst.QuoteCcy, inst.CtValCcy, inst.SettleCcy) {
			continue
		}
		formatted := okxMarketSymbol(instID, inst.BaseCcy)
		if formatted == "" {
			continue
		}
		symbols = append(symbols, formatted)
	}
	return symbols, nil
}

func isOKXUSDTInstrument(instID, quoteCcy, ctValCcy, settleCcy string) bool {
	if strings.HasSuffix(instID, "-USDT-SWAP") {
		return true
	}
	return strings.EqualFold(quoteCcy, "USDT") ||
		strings.EqualFold(ctValCcy, "USDT") ||
		strings.EqualFold(settleCcy, "USDT")
}

func okxMarketSymbol(instID, baseCcy string) string {
	base := strings.ToUpper(strings.TrimSpace(baseCcy))
	if base == "" {
		base = strings.TrimSuffix(instID, "-USDT-SWAP")
	}
	if base == "" || base == instID {
		return ""
	}
	return base + "/USDT"
}

// parseBitgetMarkets parses Bitget markets response
func (s *NativeCCXTService) parseBitgetMarkets(body []byte) ([]string, error) {
	var raw struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol    string `json:"symbol"`
			BaseCoin  string `json:"baseCoin"`
			QuoteCoin string `json:"quoteCoin"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget markets: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", raw.Msg)
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
		zaplogrus.Infof("[CCXT Native] No API credentials for %s, returning empty balance", exchange)
		return &BalanceResponse{
			Exchange: exchange,
			Total:    make(map[string]decimal.Decimal),
			Free:     make(map[string]decimal.Decimal),
			Used:     make(map[string]decimal.Decimal),
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
	url := "https://api.bitget.com/api/v2/account/all-account-balance"
	timestamp := time.Now().UnixMilli()
	method := "GET"
	requestPath := "/api/v2/account/all-account-balance"
	body := ""

	// Generate signature
	signString := fmt.Sprintf("%d%s%s%s", timestamp, method, requestPath, body)
	signature := s.generateBase64HMACSignature(conn.Secret, signString)

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
	defer closeBody(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse Bitget account-overview response.
	var raw struct {
		Code    string `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
		Data    []struct {
			AccountType string `json:"accountType"`
			USDTBalance string `json:"usdtBalance"`
			Coin        []struct {
				Coin      string `json:"coin"`
				Balance   string `json:"balance"`
				Available string `json:"available"`
				Frozen    string `json:"frozen"`
				Lock      string `json:"lock"`
			} `json:"coinList"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget balance: %w", err)
	}

	if raw.Code != "00000" {
		errMsg := strings.TrimSpace(raw.Msg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(raw.Message)
		}
		return nil, fmt.Errorf("bitget API error: %s", errMsg)
	}

	result := &BalanceResponse{
		Exchange:  "bitget",
		Timestamp: time.Now(),
		Total:     make(map[string]decimal.Decimal),
		Free:      make(map[string]decimal.Decimal),
		Used:      make(map[string]decimal.Decimal),
		Raw:       make(map[string]interface{}),
	}

	totalUSDTFromSummary := decimal.Zero
	freeUSDTFromSummary := decimal.Zero
	usedUSDTFromSummary := decimal.Zero
	totalUSDTFromCoinList := decimal.Zero
	freeUSDTFromCoinList := decimal.Zero
	usedUSDTFromCoinList := decimal.Zero
	for _, balanceData := range raw.Data {
		accountPrefix := strings.ToUpper(strings.TrimSpace(balanceData.AccountType))
		accountHasCoinListUSDT := false
		accountSummaryUSDT := decimal.Zero
		if strings.TrimSpace(balanceData.USDTBalance) != "" {
			accountSummaryUSDT = decimalFromString(strings.TrimSpace(balanceData.USDTBalance))
		}

		for _, coin := range balanceData.Coin {
			if coin.Balance == "" || coin.Balance == "0" {
				continue
			}
			total := decimalFromString(coin.Balance)
			free := decimalFromString(coin.Available)
			frozen := decimalFromString(coin.Frozen)
			locked := decimalFromString(coin.Lock)
			coinKey := strings.ToUpper(strings.TrimSpace(coin.Coin))
			scopedKey := accountPrefix + "_" + coinKey

			if coinKey != "USDT" {
				result.Total[coinKey] = result.Total[coinKey].Add(total)
				result.Free[coinKey] = result.Free[coinKey].Add(free)
				result.Used[coinKey] = result.Used[coinKey].Add(frozen.Add(locked))
			}

			if strings.TrimSpace(scopedKey) != "_" {
				result.Total[scopedKey] = result.Total[scopedKey].Add(total)
				result.Free[scopedKey] = result.Free[scopedKey].Add(free)
				result.Used[scopedKey] = result.Used[scopedKey].Add(frozen.Add(locked))
			}

			if coinKey == "USDT" {
				accountHasCoinListUSDT = true
				totalUSDTFromCoinList = totalUSDTFromCoinList.Add(total)
				freeUSDTFromCoinList = freeUSDTFromCoinList.Add(free)
				usedUSDTFromCoinList = usedUSDTFromCoinList.Add(frozen.Add(locked))
			}
		}

		if accountSummaryUSDT.GreaterThan(decimal.Zero) && !accountHasCoinListUSDT {
			totalUSDTFromSummary = totalUSDTFromSummary.Add(accountSummaryUSDT)
			key := accountPrefix + "_USDT"
			result.Total[key] = result.Total[key].Add(accountSummaryUSDT)
			if accountPrefix == "USDT_FUTURES" {
				markSummaryOnlyBalanceKey(result, key)
			} else {
				freeUSDTFromSummary = freeUSDTFromSummary.Add(accountSummaryUSDT)
				result.Free[key] = result.Free[key].Add(accountSummaryUSDT)
			}
			zaplogrus.Infof("[CCXT Native] Bitget balance account=%s usdt=%s", balanceData.AccountType, accountSummaryUSDT.StringFixed(8))
		}
	}
	totalUSDT := totalUSDTFromCoinList.Add(totalUSDTFromSummary)
	freeUSDT := freeUSDTFromCoinList.Add(freeUSDTFromSummary)
	usedUSDT := usedUSDTFromCoinList.Add(usedUSDTFromSummary)
	if totalUSDT.GreaterThan(decimal.Zero) {
		result.Total["USDT"] = totalUSDT
		result.Free["USDT"] = freeUSDT
		result.Used["USDT"] = usedUSDT
	}
	if freeUSDTFromCoinList.GreaterThan(decimal.Zero) {
		clearSummaryOnlyBalanceKey(result, "USDT")
	}

	zaplogrus.Infof("[CCXT Native] Bitget balance fetched: %d assets", len(result.Total))
	return result, nil
}

func markSummaryOnlyBalanceKey(balance *BalanceResponse, key string) {
	if balance == nil || strings.TrimSpace(key) == "" {
		return
	}
	if balance.Raw == nil {
		balance.Raw = make(map[string]interface{})
	}
	rawMap, ok := balance.Raw["summary_only_balance_keys"].(map[string]interface{})
	if !ok || rawMap == nil {
		rawMap = make(map[string]interface{})
	}
	rawMap[key] = true
	balance.Raw["summary_only_balance_keys"] = rawMap
}

func clearSummaryOnlyBalanceKey(balance *BalanceResponse, key string) {
	if balance == nil || balance.Raw == nil || strings.TrimSpace(key) == "" {
		return
	}
	rawMap, ok := balance.Raw["summary_only_balance_keys"].(map[string]interface{})
	if !ok || rawMap == nil {
		return
	}
	delete(rawMap, key)
	if len(rawMap) == 0 {
		delete(balance.Raw, "summary_only_balance_keys")
		return
	}
	balance.Raw["summary_only_balance_keys"] = rawMap
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
	defer closeBody(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse Binance account response
	var raw struct {
		MakerCommission int64 `json:"makerCommission"`
		TakerCommission int64 `json:"takerCommission"`
		CanTrade        bool  `json:"canTrade"`
		Balances        []struct {
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
		Total:     make(map[string]decimal.Decimal),
		Free:      make(map[string]decimal.Decimal),
		Used:      make(map[string]decimal.Decimal),
	}

	for _, balance := range raw.Balances {
		free := decimalFromString(balance.Free)
		locked := decimalFromString(balance.Locked)
		total := free.Add(locked)

		if total.GreaterThan(decimal.Zero) {
			result.Total[balance.Asset] = total
			result.Free[balance.Asset] = free
			result.Used[balance.Asset] = locked
			zaplogrus.Infof("[CCXT Native] Binance balance: %s = %s", balance.Asset, total.StringFixed(8))
		}
	}

	zaplogrus.Infof("[CCXT Native] Binance balance fetched: %d assets", len(result.Total))
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
	defer closeBody(resp.Body)

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
				Coin        []struct {
					Coin                string `json:"coin"`
					WalletBalance       string `json:"walletBalance"`
					AvailableToWithdraw string `json:"availableToWithdraw"`
					TotalEquity         string `json:"totalEquity"`
				} `json:"coin"`
			} `json:"list"`
		} `json:"result"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bybit balance: %w", err)
	}

	if raw.RetCode != 0 {
		return nil, fmt.Errorf("bybit API error: %s", raw.RetMsg)
	}

	result := &BalanceResponse{
		Exchange:  "bybit",
		Timestamp: time.Now(),
		Total:     make(map[string]decimal.Decimal),
		Free:      make(map[string]decimal.Decimal),
		Used:      make(map[string]decimal.Decimal),
	}

	for _, account := range raw.Result.List {
		for _, coin := range account.Coin {
			balance := decimalFromString(coin.WalletBalance)
			available := decimalFromString(coin.AvailableToWithdraw)

			if balance.GreaterThan(decimal.Zero) {
				result.Total[coin.Coin] = balance
				result.Free[coin.Coin] = available
				result.Used[coin.Coin] = balance.Sub(available)
				zaplogrus.Infof("[CCXT Native] Bybit balance: %s = %s", coin.Coin, balance.StringFixed(8))
			}
		}
	}

	zaplogrus.Infof("[CCXT Native] Bybit balance fetched: %d assets", len(result.Total))
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
	defer closeBody(resp.Body)

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
		Total:     make(map[string]decimal.Decimal),
		Free:      make(map[string]decimal.Decimal),
		Used:      make(map[string]decimal.Decimal),
	}

	for _, balanceData := range raw.Data {
		for _, detail := range balanceData.Details {
			balance := decimalFromString(detail.Bal)
			available := decimalFromString(detail.AvailBal)

			if balance.GreaterThan(decimal.Zero) {
				result.Total[detail.Ccy] = balance
				result.Free[detail.Ccy] = available
				result.Used[detail.Ccy] = balance.Sub(available)
				zaplogrus.Infof("[CCXT Native] OKX balance: %s = %s", detail.Ccy, balance.StringFixed(8))
			}
		}
	}

	zaplogrus.Infof("[CCXT Native] OKX balance fetched: %d assets", len(result.Total))
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

	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
	}

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
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	rates, err := s.parseFundingRateResponse(exchange, body)
	if err != nil {
		return nil, err
	}
	// For Bitget with specific symbols requested, filter results
	if exchange == "bitget" && len(symbols) > 1 {
		wanted := make(map[string]struct{}, len(symbols))
		for _, symbol := range symbols {
			wanted[bitgetSymbolKey(symbol)] = struct{}{}
		}
		filtered := make([]FundingRate, 0, len(rates))
		for _, rate := range rates {
			if _, ok := wanted[bitgetSymbolKey(rate.Symbol)]; ok {
				filtered = append(filtered, rate)
			}
		}
		return filtered, nil
	}
	return rates, nil
}

func (s *NativeCCXTService) FetchAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error) {
	// Fetch all funding rates without symbol filter
	url := s.buildAllFundingRateURL(exchange)
	if url == "" {
		return []FundingRate{}, nil // Not supported
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
	}

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
	defer closeBody(resp.Body)

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
		return fmt.Sprintf("https://www.okx.com/api/v5/public/funding-rate?instId=%s", okxSwapInstrumentID(symbols[0]))
	case "bitget":
		// Bitget v2: current-fund-rate for single symbol, tickers for all/filtered symbol sets.
		if len(symbols) == 1 {
			symbol := bitgetSymbolKey(symbols[0])
			return fmt.Sprintf("https://api.bitget.com/api/v2/mix/market/current-fund-rate?symbol=%s&productType=USDT-FUTURES", symbol)
		}
		return "https://api.bitget.com/api/v2/mix/market/tickers?productType=USDT-FUTURES"
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
		Symbol          string `json:"symbol"`
		FundingTime     int64  `json:"fundingTime"`
		FundingRate     string `json:"fundingRate"`
		MarkPrice       string `json:"markPrice,omitempty"`
		IndexPrice      string `json:"indexPrice,omitempty"`
		NextFundingTime int64  `json:"nextFundingTime,omitempty"`
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
			FundingRate:      rate,
			FundingTimestamp: UnixTimestamp(time.UnixMilli(item.FundingTime)),
			NextFundingTime:  UnixTimestamp(time.UnixMilli(item.NextFundingTime)),
			MarkPrice:        markPrice,
			IndexPrice:       indexPrice,
			Timestamp:        UnixTimestamp(time.Now()),
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
		return nil, fmt.Errorf("bybit API error: %s", raw.RetMsg)
	}

	rates := make([]FundingRate, 0, len(raw.Result.List))
	for _, item := range raw.Result.List {
		rate, _ := strconv.ParseFloat(item.FundingRate, 64)
		ts, _ := strconv.ParseInt(item.FundingRateTimestamp, 10, 64)

		rates = append(rates, FundingRate{
			Symbol:           item.Symbol,
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
			InstId          string `json:"instId"`
			FundingRate     string `json:"fundingRate"`
			FundingTime     string `json:"fundingTime"`
			NextFundingRate string `json:"nextFundingRate"`
			NextFundingTime string `json:"nextFundingTime"`
			MarkPx          string `json:"markPx"`
			IdxPx           string `json:"idxPx"`
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
			FundingRate:      rate,
			FundingTimestamp: UnixTimestamp(time.UnixMilli(ts)),
			NextFundingTime:  UnixTimestamp(time.UnixMilli(nextTs)),
			MarkPrice:        markPrice,
			IndexPrice:       indexPrice,
			Timestamp:        UnixTimestamp(time.Now()),
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
			Symbol          string `json:"symbol"`
			FundingRate     string `json:"fundingRate"`
			FundingTime     string `json:"fundingTime"`
			NextFundingTime string `json:"nextFundingTime"`
			NextUpdate      string `json:"nextUpdate"`
			Ts              string `json:"ts"`
			MarkPrice       string `json:"markPrice"`
			IndexPrice      string `json:"indexPrice"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget funding rate: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", raw.Msg)
	}

	rates := make([]FundingRate, 0, len(raw.Data))
	skipped := 0
	for _, item := range raw.Data {
		rate, err := strconv.ParseFloat(strings.TrimSpace(item.FundingRate), 64)
		if err != nil {
			skipped++
			zaplogrus.Warnf("[CCXT Native] skipping malformed bitget fundingRate row symbol=%s: %v", item.Symbol, err)
			continue
		}

		ts := parseBitgetTimestampMillis(item.FundingTime)
		if ts == 0 {
			ts = parseBitgetTimestampMillis(item.Ts)
		}
		if ts == 0 {
			// Fallback to current time if no timestamp is available
			ts = time.Now().UnixMilli()
		}

		nextTs := parseBitgetTimestampMillis(item.NextFundingTime)
		if nextTs == 0 {
			nextTs = parseBitgetTimestampMillis(item.NextUpdate)
		}
		if nextTs == 0 {
			nextTs = ts
		}

		markPrice := 0.0
		if v := strings.TrimSpace(item.MarkPrice); v != "" {
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				skipped++
				zaplogrus.Warnf("[CCXT Native] skipping malformed bitget markPrice row symbol=%s: %v", item.Symbol, err)
				continue
			}
			markPrice = parsed
		}

		indexPrice := 0.0
		if v := strings.TrimSpace(item.IndexPrice); v != "" {
			parsed, err := strconv.ParseFloat(v, 64)
			if err != nil {
				skipped++
				zaplogrus.Warnf("[CCXT Native] skipping malformed bitget indexPrice row symbol=%s: %v", item.Symbol, err)
				continue
			}
			indexPrice = parsed
		}

		rates = append(rates, FundingRate{
			Symbol:           item.Symbol,
			FundingRate:      rate,
			FundingTimestamp: UnixTimestamp(time.UnixMilli(ts)),
			NextFundingTime:  UnixTimestamp(time.UnixMilli(nextTs)),
			MarkPrice:        markPrice,
			IndexPrice:       indexPrice,
			Timestamp:        UnixTimestamp(time.Now()),
		})
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("no valid bitget funding rate rows in response")
	}
	if skipped > 0 {
		zaplogrus.Warnf("[CCXT Native] parsed %d bitget funding rate rows, skipped %d malformed row(s)", len(rates), skipped)
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
		return nil, fmt.Errorf("bitget API error: %s", raw.Msg)
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

// FetchOpenOrders retrieves all open orders for an exchange.
func (s *NativeCCXTService) FetchOpenOrders(ctx context.Context, exchange string) (*OpenOrdersResponse, error) {
	creds, ok := s.credentials[exchange]
	if !ok {
		return nil, fmt.Errorf("no credentials for exchange: %s", exchange)
	}

	switch exchange {
	case "bitget":
		return s.fetchBitgetOpenOrders(ctx, creds)
	case "binance":
		return s.fetchBinanceOpenOrders(ctx, creds)
	case "bybit":
		return s.fetchBybitOpenOrders(ctx, creds)
	case "okx":
		return s.fetchOKXOpenOrders(ctx, creds)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// FetchOpenOrdersForSymbol retrieves open orders for a specific symbol.
func (s *NativeCCXTService) FetchOpenOrdersForSymbol(ctx context.Context, exchange, symbol string) (*OpenOrdersResponse, error) {
	// For now, fetch all and filter - can be optimized later
	resp, err := s.FetchOpenOrders(ctx, exchange)
	if err != nil {
		return nil, err
	}

	filtered := make([]Order, 0)
	for _, order := range resp.Orders {
		if order.Symbol == symbol {
			filtered = append(filtered, order)
		}
	}

	return &OpenOrdersResponse{
		Exchange:  exchange,
		Orders:    filtered,
		Count:     len(filtered),
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// CancelOrder cancels an order by ID.
func (s *NativeCCXTService) CancelOrder(ctx context.Context, exchange, orderID, symbol string) error {
	creds, ok := s.credentials[exchange]
	if !ok {
		return fmt.Errorf("no credentials for exchange: %s", exchange)
	}

	switch exchange {
	case "bitget":
		return s.cancelBitgetOrder(ctx, creds, orderID, symbol)
	case "binance":
		return s.cancelBinanceOrder(ctx, creds, orderID, symbol)
	case "bybit":
		return s.cancelBybitOrder(ctx, creds, orderID, symbol)
	case "okx":
		return s.cancelOKXOrder(ctx, creds, orderID, symbol)
	default:
		return fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// FetchOrder retrieves a specific order by ID.
func (s *NativeCCXTService) FetchOrder(ctx context.Context, exchange, orderID, symbol string) (*OrderResponse, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(exchange)), "bitget") {
		creds, ok := s.credentials["bitget"]
		if !ok {
			return nil, fmt.Errorf("no credentials for exchange: bitget")
		}
		return s.fetchBitgetOrder(ctx, creds, orderID, symbol)
	}
	// For now, fetch all and find - can be optimized later
	resp, err := s.FetchOpenOrders(ctx, exchange)
	if err != nil {
		return nil, err
	}

	for _, order := range resp.Orders {
		if order.ID == orderID {
			return &OrderResponse{
				Exchange:  exchange,
				Order:     order,
				Timestamp: time.Now().Format(time.RFC3339),
			}, nil
		}
	}

	return nil, fmt.Errorf("order not found: %s", orderID)
}

func (s *NativeCCXTService) fetchBitgetOrder(ctx context.Context, creds config.ExchangeCredentials, orderID, symbol string) (*OrderResponse, error) {
	query := url.Values{}
	query.Set("orderId", orderID)
	query.Set("productType", "USDT-FUTURES")
	if strings.TrimSpace(symbol) != "" {
		query.Set("symbol", strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(symbol)), "/", ""))
	}
	body, err := s.bitgetPrivateGet(ctx, creds, "/api/v2/mix/order/detail?"+query.Encode())
	if err != nil {
		return nil, err
	}
	var raw struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget order: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", bitgetErrorMessage(raw.Msg, ""))
	}
	var rec struct {
		OrderID     string `json:"orderId"`
		ClientOid   string `json:"clientOid"`
		Symbol      string `json:"symbol"`
		Side        string `json:"side"`
		OrderType   string `json:"orderType"`
		State       string `json:"state"`
		Price       string `json:"price"`
		Size        string `json:"size"`
		BaseVolume  string `json:"baseVolume"`
		QuoteVolume string `json:"quoteVolume"`
		Fee         string `json:"fee"`
		CTime       string `json:"cTime"`
		UTime       string `json:"uTime"`
	}
	if err := json.Unmarshal(raw.Data, &rec); err != nil {
		return nil, fmt.Errorf("failed to decode Bitget order data: %w", err)
	}
	amount := parseDecimal(rec.Size)
	filled := parseDecimal(rec.BaseVolume)
	cost := parseDecimal(rec.QuoteVolume)
	if cost.IsZero() {
		cost = parseDecimal(rec.Price).Mul(filled)
	}
	createdMS := parseBitgetTimestampMillis(rec.CTime)
	createdAt := time.Now().UTC()
	if createdMS > 0 {
		createdAt = time.UnixMilli(createdMS).UTC()
	}
	return &OrderResponse{
		Exchange: "bitget",
		Order: Order{
			ID:            rec.OrderID,
			ClientOrderID: rec.ClientOid,
			Symbol:        normalizeBitgetSpotSymbol(rec.Symbol),
			Type:          strings.ToLower(strings.TrimSpace(rec.OrderType)),
			Side:          normalizeBitgetOrderSide(rec.Side),
			Status:        normalizeBitgetOrderStatus(rec.State),
			Price:         parseDecimal(rec.Price),
			Amount:        amount,
			Filled:        filled,
			Remaining:     amount.Sub(filled),
			Cost:          cost,
			Fee:           parseDecimal(rec.Fee).Abs(),
			CreatedAt:     createdAt,
			Timestamp:     UnixTimestamp(time.Now().UTC()),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

// FetchPositions retrieves all positions for an exchange.
func (s *NativeCCXTService) FetchPositions(ctx context.Context, exchange string) (*PositionsResponse, error) {
	creds, ok := s.credentials[exchange]
	if !ok {
		return nil, fmt.Errorf("no credentials for exchange: %s", exchange)
	}

	switch exchange {
	case "bitget":
		return s.fetchBitgetPositions(ctx, creds)
	case "binance":
		return s.fetchBinancePositions(ctx, creds)
	case "bybit":
		return s.fetchBybitPositions(ctx, creds)
	case "okx":
		return s.fetchOKXPositions(ctx, creds)
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", exchange)
	}
}

// Stub implementations for each exchange - to be implemented
func (s *NativeCCXTService) fetchBitgetOpenOrders(ctx context.Context, creds config.ExchangeCredentials) (*OpenOrdersResponse, error) {
	body, err := s.bitgetPrivateGet(ctx, creds, "/api/v2/mix/order/orders-pending?productType=USDT-FUTURES")
	if err != nil {
		return nil, err
	}

	var raw struct {
		Code    string          `json:"code"`
		Msg     string          `json:"msg"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget open orders: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", bitgetErrorMessage(raw.Msg, raw.Message))
	}

	type bitgetPendingOrder struct {
		OrderID   string `json:"orderId"`
		ClientOid string `json:"clientOid"`
		Symbol    string `json:"symbol"`
		Side      string `json:"side"`
		OrderType string `json:"orderType"`
		State     string `json:"state"`
		Price     string `json:"price"`
		Size      string `json:"size"`
		BaseVol   string `json:"baseVolume"`
		CTime     string `json:"cTime"`
		UTime     string `json:"uTime"`
	}

	records := make([]bitgetPendingOrder, 0)
	if len(raw.Data) > 0 && string(raw.Data) != "null" {
		var wrapped struct {
			EntrustedList []bitgetPendingOrder `json:"entrustedList"`
			List          []bitgetPendingOrder `json:"list"`
		}
		if err := json.Unmarshal(raw.Data, &wrapped); err == nil {
			switch {
			case len(wrapped.EntrustedList) > 0:
				records = wrapped.EntrustedList
			case len(wrapped.List) > 0:
				records = wrapped.List
			}
		}
		if len(records) == 0 {
			var direct []bitgetPendingOrder
			if err := json.Unmarshal(raw.Data, &direct); err == nil {
				records = direct
			}
		}
	}

	orders := make([]Order, 0, len(records))
	for _, rec := range records {
		amount := parseDecimal(rec.Size)
		filled := parseDecimal(rec.BaseVol)
		if filled.GreaterThan(amount) {
			filled = amount
		}
		remaining := amount.Sub(filled)
		if remaining.IsNegative() {
			remaining = decimal.Zero
		}

		createdMS := parseBitgetTimestampMillis(rec.CTime)
		updatedMS := parseBitgetTimestampMillis(rec.UTime)
		if updatedMS == 0 {
			updatedMS = createdMS
		}

		createdAt := time.Now().UTC()
		if createdMS > 0 {
			createdAt = time.UnixMilli(createdMS).UTC()
		}
		updatedAt := UnixTimestamp(time.Now().UTC())
		if updatedMS > 0 {
			updatedAt = UnixTimestamp(time.UnixMilli(updatedMS).UTC())
		}

		side := normalizeBitgetOrderSide(rec.Side)
		status := normalizeBitgetOrderStatus(rec.State)
		if status == "" {
			status = "open"
		}

		price := parseDecimal(rec.Price)
		cost := price.Mul(filled)

		orders = append(orders, Order{
			ID:            rec.OrderID,
			ClientOrderID: rec.ClientOid,
			Symbol:        normalizeBitgetSpotSymbol(rec.Symbol),
			Type:          strings.ToLower(strings.TrimSpace(rec.OrderType)),
			Side:          side,
			Status:        status,
			Price:         price,
			Amount:        amount,
			Filled:        filled,
			Remaining:     remaining,
			Cost:          cost,
			CreatedAt:     createdAt,
			Timestamp:     updatedAt,
		})
	}

	return &OpenOrdersResponse{
		Exchange:  "bitget",
		Orders:    orders,
		Count:     len(orders),
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}
func (s *NativeCCXTService) fetchBinanceOpenOrders(ctx context.Context, creds config.ExchangeCredentials) (*OpenOrdersResponse, error) {
	return &OpenOrdersResponse{Exchange: "binance", Orders: []Order{}, Count: 0, Timestamp: time.Now().Format(time.RFC3339)}, nil
}
func (s *NativeCCXTService) fetchBybitOpenOrders(ctx context.Context, creds config.ExchangeCredentials) (*OpenOrdersResponse, error) {
	return &OpenOrdersResponse{Exchange: "bybit", Orders: []Order{}, Count: 0, Timestamp: time.Now().Format(time.RFC3339)}, nil
}
func (s *NativeCCXTService) fetchOKXOpenOrders(ctx context.Context, creds config.ExchangeCredentials) (*OpenOrdersResponse, error) {
	return &OpenOrdersResponse{Exchange: "okx", Orders: []Order{}, Count: 0, Timestamp: time.Now().Format(time.RFC3339)}, nil
}

func (s *NativeCCXTService) cancelBitgetOrder(ctx context.Context, creds config.ExchangeCredentials, orderID, symbol string) error {
	return fmt.Errorf("cancel order not implemented for bitget")
}
func (s *NativeCCXTService) cancelBinanceOrder(ctx context.Context, creds config.ExchangeCredentials, orderID, symbol string) error {
	return fmt.Errorf("cancel order not implemented for binance")
}
func (s *NativeCCXTService) cancelBybitOrder(ctx context.Context, creds config.ExchangeCredentials, orderID, symbol string) error {
	return fmt.Errorf("cancel order not implemented for bybit")
}
func (s *NativeCCXTService) cancelOKXOrder(ctx context.Context, creds config.ExchangeCredentials, orderID, symbol string) error {
	return fmt.Errorf("cancel order not implemented for okx")
}

func (s *NativeCCXTService) fetchBitgetPositions(ctx context.Context, creds config.ExchangeCredentials) (*PositionsResponse, error) {
	body, err := s.bitgetPrivateGet(ctx, creds, "/api/v2/mix/position/all-position?productType=USDT-FUTURES&marginCoin=USDT")
	if err != nil {
		return nil, err
	}

	var raw struct {
		Code    string          `json:"code"`
		Msg     string          `json:"msg"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Bitget positions: %w", err)
	}
	if raw.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s", bitgetErrorMessage(raw.Msg, raw.Message))
	}

	type bitgetPositionRecord struct {
		Symbol           string `json:"symbol"`
		PositionID       string `json:"positionId"`
		HoldSide         string `json:"holdSide"`
		PosSide          string `json:"posSide"`
		Total            string `json:"total"`
		AverageOpenPrice string `json:"averageOpenPrice"`
		OpenPriceAvg     string `json:"openPriceAvg"`
		MarkPrice        string `json:"markPrice"`
		UnrealizedPL     string `json:"unrealizedPL"`
		Leverage         string `json:"leverage"`
		LiquidationPrice string `json:"liquidationPrice"`
		MarginMode       string `json:"marginMode"`
		MarginType       string `json:"marginType"`
		UTime            string `json:"uTime"`
		CTime            string `json:"cTime"`
	}

	records := make([]bitgetPositionRecord, 0)
	if len(raw.Data) > 0 && string(raw.Data) != "null" {
		var wrapped struct {
			List []bitgetPositionRecord `json:"list"`
		}
		if err := json.Unmarshal(raw.Data, &wrapped); err == nil && len(wrapped.List) > 0 {
			records = wrapped.List
		}
		if len(records) == 0 {
			var direct []bitgetPositionRecord
			if err := json.Unmarshal(raw.Data, &direct); err == nil {
				records = direct
			}
		}
	}

	positions := make([]Position, 0, len(records))
	for _, rec := range records {
		size := parseDecimal(rec.Total).Abs()
		if size.IsZero() {
			continue
		}

		entry := parseDecimal(firstNonEmpty(rec.AverageOpenPrice, rec.OpenPriceAvg))
		mark := parseDecimal(rec.MarkPrice)
		if mark.IsZero() {
			mark = entry
		}

		leverage := 0
		if parsed, err := strconv.Atoi(strings.TrimSpace(rec.Leverage)); err == nil {
			leverage = parsed
		}

		tsMillis := parseBitgetTimestampMillis(firstNonEmpty(rec.UTime, rec.CTime))
		ts := UnixTimestamp(time.Now().UTC())
		if tsMillis > 0 {
			ts = UnixTimestamp(time.UnixMilli(tsMillis).UTC())
		}

		side := normalizeBitgetPositionSide(firstNonEmpty(rec.HoldSide, rec.PosSide), size)
		marginMode := strings.ToLower(strings.TrimSpace(firstNonEmpty(rec.MarginMode, rec.MarginType)))
		if marginMode == "" {
			marginMode = "crossed"
		}

		positions = append(positions, Position{
			ID:               firstNonEmpty(rec.PositionID, rec.Symbol+":"+side),
			Symbol:           normalizeBitgetSpotSymbol(rec.Symbol),
			Side:             side,
			Size:             size,
			EntryPrice:       entry,
			MarkPrice:        mark,
			UnrealizedPnl:    parseDecimal(rec.UnrealizedPL),
			Leverage:         leverage,
			LiquidationPrice: parseDecimal(rec.LiquidationPrice),
			MarginMode:       marginMode,
			Timestamp:        ts,
		})
	}

	return &PositionsResponse{
		Exchange:  "bitget",
		Positions: positions,
		Count:     len(positions),
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}
func (s *NativeCCXTService) fetchBinancePositions(ctx context.Context, creds config.ExchangeCredentials) (*PositionsResponse, error) {
	return &PositionsResponse{Exchange: "binance", Positions: []Position{}, Count: 0, Timestamp: time.Now().Format(time.RFC3339)}, nil
}
func (s *NativeCCXTService) fetchBybitPositions(ctx context.Context, creds config.ExchangeCredentials) (*PositionsResponse, error) {
	return &PositionsResponse{Exchange: "bybit", Positions: []Position{}, Count: 0, Timestamp: time.Now().Format(time.RFC3339)}, nil
}
func (s *NativeCCXTService) fetchOKXPositions(ctx context.Context, creds config.ExchangeCredentials) (*PositionsResponse, error) {
	return &PositionsResponse{Exchange: "okx", Positions: []Position{}, Count: 0, Timestamp: time.Now().Format(time.RFC3339)}, nil
}

func (s *NativeCCXTService) bitgetPrivateGet(ctx context.Context, creds config.ExchangeCredentials, endpoint string) ([]byte, error) {
	if strings.TrimSpace(creds.APIKey) == "" || strings.TrimSpace(creds.Secret) == "" {
		return nil, fmt.Errorf("bitget credentials are incomplete")
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait interrupted: %w", err)
	}

	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signPayload := timestamp + "GET" + endpoint
	signature := s.generateBase64HMACSignature(creds.Secret, signPayload)

	baseURL := "https://api.bitget.com"
	if connection, ok := s.exchanges["bitget"]; ok && connection != nil && strings.TrimSpace(connection.BaseURL) != "" {
		baseURL = strings.TrimRight(connection.BaseURL, "/")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bitget request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACCESS-KEY", creds.APIKey)
	req.Header.Set("ACCESS-SIGN", signature)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("ACCESS-PASSPHRASE", creds.Passphrase)
	req.Header.Set("locale", "en-US")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Bitget API: %w", err)
	}
	defer closeBody(resp.Body)

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Bitget response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bitget API status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

func bitgetErrorMessage(msg, message string) string {
	candidates := []string{strings.TrimSpace(msg), strings.TrimSpace(message)}
	for _, candidate := range candidates {
		if candidate != "" {
			return candidate
		}
	}
	return "unknown error"
}

func parseBitgetTimestampMillis(raw string) int64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		if parsed > 0 && parsed < 1_000_000_000_000 {
			return parsed * 1000
		}
		return parsed
	}
	if parsedFloat, err := strconv.ParseFloat(trimmed, 64); err == nil {
		parsed := int64(parsedFloat)
		if parsed > 0 && parsed < 1_000_000_000_000 {
			return parsed * 1000
		}
		return parsed
	}
	return 0
}

func normalizeBitgetOrderSide(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy", "open_long", "close_short":
		return "buy"
	case "sell", "open_short", "close_long":
		return "sell"
	default:
		return strings.ToLower(strings.TrimSpace(side))
	}
}

func normalizeBitgetOrderStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "live", "new", "partially_filled", "partial_fill":
		return "open"
	case "filled", "full_fill", "closed":
		return "closed"
	case "cancelled", "canceled":
		return "canceled"
	default:
		return strings.ToLower(strings.TrimSpace(state))
	}
}

func normalizeBitgetPositionSide(side string, size decimal.Decimal) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "long", "buy", "open_long":
		return "long"
	case "short", "sell", "open_short":
		return "short"
	default:
		if size.IsNegative() {
			return "short"
		}
		return "long"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
