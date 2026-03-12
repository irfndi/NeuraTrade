package ccxt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNativeCCXTService_InitializePreservesConfiguredCredentials(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTServiceWithConfig(5*time.Second, 1, map[string]config.ExchangeCredentials{
		"bitget": {
			APIKey:     "bitget-key",
			Secret:     "bitget-secret",
			Passphrase: "bitget-passphrase",
		},
	})

	if err := service.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() returned error: %v", err)
	}

	conn, ok := service.exchanges["bitget"]
	if !ok || conn == nil {
		t.Fatalf("bitget exchange connection not initialized")
	}
	if conn.APIKey != "bitget-key" {
		t.Fatalf("bitget API key was overwritten during initialize: got %q", conn.APIKey)
	}
	if conn.Secret != "bitget-secret" {
		t.Fatalf("bitget secret was overwritten during initialize: got %q", conn.Secret)
	}
	if conn.Passphrase != "bitget-passphrase" {
		t.Fatalf("bitget passphrase was overwritten during initialize: got %q", conn.Passphrase)
	}
}

func TestNativeCCXTService_FetchBalance_BitgetAllAccountBalance(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	service.exchanges["bitget"] = &ExchangeConnection{
		Name:       "bitget",
		BaseURL:    "https://api.bitget.com",
		APIKey:     "bitget-key",
		Secret:     "bitget-secret",
		Passphrase: "bitget-passphrase",
	}

	var gotPath string
	service.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path

			if req.Header.Get("ACCESS-KEY") != "bitget-key" {
				t.Fatalf("missing ACCESS-KEY header")
			}
			sign := req.Header.Get("ACCESS-SIGN")
			if sign == "" {
				t.Fatalf("missing ACCESS-SIGN header")
			}
			if ok, _ := regexp.MatchString(`^[A-Za-z0-9+/]+=*$`, sign); !ok {
				t.Fatalf("ACCESS-SIGN is not base64 encoded: %q", sign)
			}
			if req.Header.Get("ACCESS-PASSPHRASE") != "bitget-passphrase" {
				t.Fatalf("missing ACCESS-PASSPHRASE header")
			}

			body := `{
				"code":"00000",
				"msg":"success",
				"data":[
					{"accountType":"spot","usdtBalance":"10.50"},
					{"accountType":"usdt_futures","usdtBalance":"2.25"}
				]
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	balance, err := service.FetchBalance(context.Background(), "bitget")
	if err != nil {
		t.Fatalf("FetchBalance() returned error: %v", err)
	}
	if gotPath != "/api/v2/account/all-account-balance" {
		t.Fatalf("unexpected Bitget endpoint path: got %q", gotPath)
	}

	if balance.Total["SPOT_USDT"] != 10.5 {
		t.Fatalf("unexpected SPOT_USDT balance: got %.8f", balance.Total["SPOT_USDT"])
	}
	if balance.Total["USDT_FUTURES_USDT"] != 2.25 {
		t.Fatalf("unexpected USDT_FUTURES_USDT balance: got %.8f", balance.Total["USDT_FUTURES_USDT"])
	}
	if balance.Total["USDT"] != 12.75 {
		t.Fatalf("unexpected aggregated USDT balance: got %.8f", balance.Total["USDT"])
	}
}

func TestNativeCCXTService_FetchBalance_BitgetMergesCoinListAndSummaryUSDT(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	service.exchanges["bitget"] = &ExchangeConnection{
		Name:       "bitget",
		BaseURL:    "https://api.bitget.com",
		APIKey:     "bitget-key",
		Secret:     "bitget-secret",
		Passphrase: "bitget-passphrase",
	}

	service.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{
				"code":"00000",
				"msg":"success",
				"data":[
					{
						"accountType":"spot",
						"usdtBalance":"10.50",
						"coinList":[{"coin":"USDT","balance":"10.00","available":"9.50","frozen":"0.25","lock":"0.25"}]
					},
					{
						"accountType":"usdt_futures",
						"usdtBalance":"2.25"
					}
				]
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	balance, err := service.FetchBalance(context.Background(), "bitget")
	if err != nil {
		t.Fatalf("FetchBalance() returned error: %v", err)
	}
	if balance.Total["SPOT_USDT"] != 10 {
		t.Fatalf("unexpected SPOT_USDT balance: got %.8f", balance.Total["SPOT_USDT"])
	}
	if balance.Total["USDT"] != 12.25 {
		t.Fatalf("unexpected aggregated USDT balance: got %.8f", balance.Total["USDT"])
	}
	if balance.Free["USDT"] != 11.75 {
		t.Fatalf("unexpected aggregated free USDT balance: got %.8f", balance.Free["USDT"])
	}
}

func TestNativeCCXTService_ParseBitgetTicker_PopulatesPercentage(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	ticker, err := service.parseBitgetTicker("DOGE/USDT", []byte(`{
		"code":"00000",
		"msg":"success",
		"data":[{
			"symbol":"DOGEUSDT",
			"lastPr":"0.1000",
			"bidPr":"0.0999",
			"askPr":"0.1001",
			"high24h":"0.1050",
			"low24h":"0.0950",
			"baseVolume":"1000000",
			"change24h":"-1.25"
		}]
	}`))
	require.NoError(t, err)
	percentage, _ := ticker.Percentage.Float64()
	assert.Equal(t, -1.25, percentage)
	adapter := &TickerMarketPriceAdapter{data: &TickerData{Exchange: "bitget", Ticker: *ticker}}
	assert.Equal(t, -1.25, adapter.GetPriceChange24h())
}

func TestNativeCCXTService_ParseBybitTicker_PopulatesPercentage(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	ticker, err := service.parseBybitTicker("BTCUSDT", []byte(`{
		"retCode":0,
		"retMsg":"OK",
		"result":{
			"list":[{
				"symbol":"BTCUSDT",
				"lastPrice":"120635.50",
				"bid1Price":"120635.40",
				"ask1Price":"120635.60",
				"highPrice24h":"131309.30",
				"lowPrice24h":"102007.60",
				"volume24h":"13713832.0000",
				"price24hPcnt":"0.142425"
			}]
		}
	}`))
	require.NoError(t, err)
	percentage, _ := ticker.Percentage.Float64()
	assert.InDelta(t, 14.2425, percentage, 0.0001)
	adapter := &TickerMarketPriceAdapter{data: &TickerData{Exchange: "bybit", Ticker: *ticker}}
	assert.InDelta(t, 14.2425, adapter.GetPriceChange24h(), 0.0001)
}

func TestNativeCCXTService_ParseOKXTicker_PopulatesPercentage(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	ticker, err := service.parseOKXTicker("BTC-USDT", []byte(`{
		"code":"0",
		"msg":"",
		"data":[{
			"instId":"BTC-USDT",
			"last":"51240",
			"bidPx":"51239.9",
			"askPx":"51240",
			"open24h":"51695.6",
			"high24h":"52080",
			"low24h":"50936",
			"vol24h":"10474.12353007"
		}]
	}`))
	require.NoError(t, err)
	percentage, _ := ticker.Percentage.Float64()
	assert.InDelta(t, -0.8813129, percentage, 0.000001)
	adapter := &TickerMarketPriceAdapter{data: &TickerData{Exchange: "okx", Ticker: *ticker}}
	assert.InDelta(t, -0.8813129, adapter.GetPriceChange24h(), 0.000001)
}

func TestNativeCCXTService_FetchOpenOrders_Bitget(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	service.credentials["bitget"] = config.ExchangeCredentials{
		APIKey:     "bitget-key",
		Secret:     "bitget-secret",
		Passphrase: "bitget-passphrase",
	}

	var gotPath string
	service.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path + "?" + req.URL.RawQuery

			if req.Header.Get("ACCESS-KEY") != "bitget-key" {
				t.Fatalf("missing ACCESS-KEY header")
			}
			sign := req.Header.Get("ACCESS-SIGN")
			if sign == "" {
				t.Fatalf("missing ACCESS-SIGN header")
			}
			if ok, _ := regexp.MatchString(`^[A-Za-z0-9+/]+=*$`, sign); !ok {
				t.Fatalf("ACCESS-SIGN is not base64 encoded: %q", sign)
			}
			if req.Header.Get("ACCESS-PASSPHRASE") != "bitget-passphrase" {
				t.Fatalf("missing ACCESS-PASSPHRASE header")
			}

			body := `{
				"code":"00000",
				"msg":"success",
				"data":{
					"entrustedList":[
						{
							"orderId":"ord-1",
							"clientOid":"client-1",
							"symbol":"ADAUSDT",
							"side":"open_long",
							"orderType":"market",
							"state":"live",
							"price":"0.3000",
							"size":"10",
							"baseVolume":"2",
							"cTime":"1700000000000",
							"uTime":"1700000001000"
						}
					]
				}
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resp, err := service.FetchOpenOrders(context.Background(), "bitget")
	if err != nil {
		t.Fatalf("FetchOpenOrders() returned error: %v", err)
	}
	if gotPath != "/api/v2/mix/order/orders-pending?productType=USDT-FUTURES" {
		t.Fatalf("unexpected Bitget endpoint path: got %q", gotPath)
	}
	if resp.Count != 1 {
		t.Fatalf("expected one open order, got %d", resp.Count)
	}
	order := resp.Orders[0]
	if order.Symbol != "ADA/USDT" {
		t.Fatalf("unexpected symbol: %s", order.Symbol)
	}
	if order.Side != "buy" {
		t.Fatalf("unexpected side: %s", order.Side)
	}
	if !order.Amount.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("unexpected amount: %s", order.Amount.String())
	}
	if !order.Filled.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("unexpected filled: %s", order.Filled.String())
	}
	if !order.Remaining.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("unexpected remaining: %s", order.Remaining.String())
	}
}

func TestNativeCCXTService_FetchPositions_Bitget(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	service.credentials["bitget"] = config.ExchangeCredentials{
		APIKey:     "bitget-key",
		Secret:     "bitget-secret",
		Passphrase: "bitget-passphrase",
	}

	var gotPath string
	service.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path + "?" + req.URL.RawQuery

			body := `{
				"code":"00000",
				"msg":"success",
				"data":[
					{
						"positionId":"pos-1",
						"symbol":"ADAUSDT",
						"holdSide":"long",
						"total":"12.5",
						"averageOpenPrice":"0.3000",
						"markPrice":"0.3100",
						"unrealizedPL":"0.50",
						"leverage":"5",
						"liquidationPrice":"0.2000",
						"marginMode":"isolated",
						"uTime":"1700000000000"
					},
					{
						"positionId":"pos-2",
						"symbol":"DOGEUSDT",
						"holdSide":"short",
						"total":"0",
						"averageOpenPrice":"0.1000"
					}
				]
			}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resp, err := service.FetchPositions(context.Background(), "bitget")
	if err != nil {
		t.Fatalf("FetchPositions() returned error: %v", err)
	}
	if gotPath != "/api/v2/mix/position/all-position?productType=USDT-FUTURES&marginCoin=USDT" {
		t.Fatalf("unexpected Bitget endpoint path: got %q", gotPath)
	}
	if resp.Count != 1 {
		t.Fatalf("expected one open position, got %d", resp.Count)
	}
	position := resp.Positions[0]
	if position.Symbol != "ADA/USDT" {
		t.Fatalf("unexpected symbol: %s", position.Symbol)
	}
	if position.Side != "long" {
		t.Fatalf("unexpected side: %s", position.Side)
	}
	if !position.Size.Equal(decimal.RequireFromString("12.5")) {
		t.Fatalf("unexpected size: %s", position.Size.String())
	}
	if !position.UnrealizedPnl.Equal(decimal.RequireFromString("0.5")) {
		t.Fatalf("unexpected unrealized pnl: %s", position.UnrealizedPnl.String())
	}
}

func TestBuildTickerURL(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)

	tests := []struct {
		name     string
		exchange string
		symbol   string
		want     string
	}{
		{"bitget", "bitget", "BTC/USDT", "https://api.bitget.com/api/v2/spot/market/tickers?symbol=BTC/USDT"},
		{"binance", "binance", "ETH/USDT", "https://api.binance.com/api/v3/ticker/24hr?symbol=ETH/USDT"},
		{"bybit", "bybit", "BTC/USDT", "https://api.bybit.com/v5/market/tickers?category=linear&symbol=BTC/USDT"},
		{"okx", "okx", "BTC/USDT", "https://www.okx.com/api/v5/market/ticker?instId=BTC/USDT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.buildTickerURL(tt.exchange, tt.symbol)
			if got != tt.want {
				t.Fatalf("buildTickerURL(%s, %s) = %s, want %s", tt.exchange, tt.symbol, got, tt.want)
			}
		})
	}
}

func TestBuildFundingRateURL_Bitget(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)

	tests := []struct {
		name    string
		symbols []string
		want    string
	}{
		{
			name:    "all funding rates",
			symbols: nil,
			want:    "https://api.bitget.com/api/v2/mix/market/tickers?productType=USDT-FUTURES",
		},
		{
			name:    "single symbol",
			symbols: []string{"BTC/USDT:USDT"},
			want:    "https://api.bitget.com/api/v2/mix/market/current-fund-rate?symbol=BTCUSDT&productType=USDT-FUTURES",
		},
		{
			name:    "multiple symbols",
			symbols: []string{"BTC/USDT:USDT", "ETH/USDT:USDT"},
			want:    "https://api.bitget.com/api/v2/mix/market/tickers?productType=USDT-FUTURES",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := service.buildFundingRateURL("bitget", tt.symbols)
			if got != tt.want {
				t.Fatalf("buildFundingRateURL(bitget, %v) = %s, want %s", tt.symbols, got, tt.want)
			}
		})
	}
}

func TestParseBitgetFundingRate_V2Formats(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)

	currentFundRatePayload := []byte(`{
		"code":"00000",
		"msg":"success",
		"data":[
			{
				"symbol":"BTCUSDT",
				"fundingRate":"-0.000033",
				"fundingRateInterval":"8",
				"nextUpdate":"1772121600000"
			}
		]
	}`)

	rates, err := service.parseBitgetFundingRate(currentFundRatePayload)
	if err != nil {
		t.Fatalf("parseBitgetFundingRate(current-fund-rate) returned error: %v", err)
	}
	if len(rates) != 1 {
		t.Fatalf("expected 1 funding rate, got %d", len(rates))
	}
	if rates[0].Symbol != "BTCUSDT" {
		t.Fatalf("unexpected symbol: %s", rates[0].Symbol)
	}
	parsedRate := decimal.NewFromFloat(rates[0].FundingRate).Round(6)
	if !parsedRate.Equal(decimal.RequireFromString("-0.000033")) {
		t.Fatalf("unexpected funding rate: %s", parsedRate.String())
	}
	if time.Time(rates[0].NextFundingTime).UnixMilli() != 1772121600000 {
		t.Fatalf("unexpected next funding time: %d", time.Time(rates[0].NextFundingTime).UnixMilli())
	}

	tickersPayload := []byte(`{
		"code":"00000",
		"msg":"success",
		"data":[
			{
				"symbol":"ETHUSDT",
				"fundingRate":"-0.000072",
				"ts":"1772103947939",
				"markPrice":"2063.73",
				"indexPrice":"2064.9400585406"
			}
		]
	}`)

	rates, err = service.parseBitgetFundingRate(tickersPayload)
	if err != nil {
		t.Fatalf("parseBitgetFundingRate(tickers) returned error: %v", err)
	}
	if len(rates) != 1 {
		t.Fatalf("expected 1 funding rate from tickers payload, got %d", len(rates))
	}
	if rates[0].Symbol != "ETHUSDT" {
		t.Fatalf("unexpected symbol from tickers payload: %s", rates[0].Symbol)
	}
	if time.Time(rates[0].FundingTimestamp).UnixMilli() != 1772103947939 {
		t.Fatalf("unexpected funding timestamp from tickers payload: %d", time.Time(rates[0].FundingTimestamp).UnixMilli())
	}
	if rates[0].MarkPrice <= 0 {
		t.Fatalf("expected positive mark price, got %f", rates[0].MarkPrice)
	}
	if rates[0].IndexPrice <= 0 {
		t.Fatalf("expected positive index price, got %f", rates[0].IndexPrice)
	}
}

func TestParseBitgetFundingRate_SkipsMalformedRows(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)

	payload := []byte(`{
		"code":"00000",
		"msg":"success",
		"data":[
			{
				"symbol":"BADUSDT",
				"fundingRate":"bad"
			},
			{
				"symbol":"ETHUSDT",
				"fundingRate":"-0.000072",
				"ts":"1772103947939",
				"markPrice":"2063.73",
				"indexPrice":"2064.9400585406"
			}
		]
	}`)

	rates, err := service.parseBitgetFundingRate(payload)
	if err != nil {
		t.Fatalf("parseBitgetFundingRate returned error: %v", err)
	}
	if len(rates) != 1 {
		t.Fatalf("expected 1 valid funding rate, got %d", len(rates))
	}
	if rates[0].Symbol != "ETHUSDT" {
		t.Fatalf("unexpected symbol from valid row: %s", rates[0].Symbol)
	}
}

func TestNativeCCXTService_FetchMarketData_FallbackConfig(t *testing.T) {
	tests := []struct {
		name                  string
		maxSymbolsEnv         string
		cycleBudgetEnvMS      string
		perSymbolTimeoutEnvMS string
		symbols               []string
		perRequestDelay       time.Duration
		cancelOnSymbolPrefix  string
		ctxTimeout            time.Duration
		expectedMinData       int
		expectedMaxData       int
		expectedRequests      int
		expectedElapsedMax    time.Duration
		expectContextErr      bool
		expectPartialErr      bool
	}{
		{
			name:                  "symbol_budget_limits_fetches",
			maxSymbolsEnv:         "2",
			cycleBudgetEnvMS:      "10000",
			perSymbolTimeoutEnvMS: "500",
			symbols:               []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
			expectedMinData:       2,
			expectedMaxData:       2,
			expectedRequests:      2,
			expectPartialErr:      true,
		},
		{
			name:                  "empty_symbols_returns_fast",
			maxSymbolsEnv:         "10",
			cycleBudgetEnvMS:      "10000",
			perSymbolTimeoutEnvMS: "500",
			symbols:               []string{},
			expectedMinData:       0,
			expectedMaxData:       0,
			expectedRequests:      0,
		},
		{
			name:                  "zero_budget_uses_default_budget",
			maxSymbolsEnv:         "10",
			cycleBudgetEnvMS:      "0",
			perSymbolTimeoutEnvMS: "500",
			symbols:               []string{"BTC/USDT"},
			expectedMinData:       1,
			expectedMaxData:       1,
			expectedRequests:      1,
		},
		{
			name:                  "one_ms_cycle_budget_returns_partial",
			maxSymbolsEnv:         "10",
			cycleBudgetEnvMS:      "2",
			perSymbolTimeoutEnvMS: "500",
			symbols:               []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
			perRequestDelay:       5 * time.Millisecond,
			expectedMinData:       0,
			expectedMaxData:       1,
			expectedRequests:      -1,
			expectPartialErr:      true,
		},
		{
			name:                  "one_ms_per_symbol_timeout_does_not_error",
			maxSymbolsEnv:         "10",
			cycleBudgetEnvMS:      "10000",
			perSymbolTimeoutEnvMS: "1",
			symbols:               []string{"BTC/USDT", "ETH/USDT"},
			perRequestDelay:       20 * time.Millisecond,
			expectedMinData:       0,
			expectedMaxData:       0,
			expectedRequests:      -1,
		},
		{
			name:                  "negative_values_fall_back_to_defaults",
			maxSymbolsEnv:         "-4",
			cycleBudgetEnvMS:      "-1",
			perSymbolTimeoutEnvMS: "-5",
			symbols:               []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
			expectedMinData:       3,
			expectedMaxData:       3,
			expectedRequests:      3,
		},
		{
			name:                  "non_numeric_values_fall_back_to_defaults",
			maxSymbolsEnv:         "abc",
			cycleBudgetEnvMS:      "oops",
			perSymbolTimeoutEnvMS: "bad",
			symbols:               []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
			expectedMinData:       3,
			expectedMaxData:       3,
			expectedRequests:      3,
		},
		{
			name:                  "context_cancellation_returns_partial_data",
			maxSymbolsEnv:         "10",
			cycleBudgetEnvMS:      "10000",
			perSymbolTimeoutEnvMS: "500",
			symbols:               []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
			perRequestDelay:       40 * time.Millisecond,
			cancelOnSymbolPrefix:  "ETH",
			ctxTimeout:            400 * time.Millisecond,
			expectedMinData:       1,
			expectedMaxData:       2,
			expectedRequests:      -1,
			expectedElapsedMax:    3 * time.Second,
			expectContextErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NEURATRADE_SCALPING_FALLBACK_MAX_SYMBOLS_PER_CYCLE", tt.maxSymbolsEnv)
			t.Setenv("NEURATRADE_SCALPING_FALLBACK_CYCLE_BUDGET_MS", tt.cycleBudgetEnvMS)
			t.Setenv("NEURATRADE_SCALPING_FALLBACK_PER_SYMBOL_TIMEOUT_MS", tt.perSymbolTimeoutEnvMS)

			service := NewNativeCCXTService(5*time.Second, 1)
			service.exchanges["binance"] = &ExchangeConnection{
				Name:    "binance",
				BaseURL: "https://api.binance.com",
			}

			var requests atomic.Int32
			service.httpClient = &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests.Add(1)
					symbol := req.URL.Query().Get("symbol")
					if symbol == "" {
						symbol = "BTCUSDT"
					}
					if tt.cancelOnSymbolPrefix != "" && strings.HasPrefix(symbol, tt.cancelOnSymbolPrefix) {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					if tt.perRequestDelay > 0 {
						select {
						case <-time.After(tt.perRequestDelay):
						case <-req.Context().Done():
							return nil, req.Context().Err()
						}
					}
					body := fmt.Sprintf(`{"symbol":"%s","lastPrice":"100","bidPrice":"99","askPrice":"101","highPrice":"105","lowPrice":"95","volume":"123","openPrice":"98","prevClosePrice":"97"}`, symbol)
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(body)),
						Header:     make(http.Header),
					}, nil
				}),
			}

			ctx := context.Background()
			cancel := func() {}
			if tt.ctxTimeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
			}
			defer cancel()

			start := time.Now()
			data, err := service.FetchMarketData(ctx, []string{"binance"}, tt.symbols)
			elapsed := time.Since(start)

			if tt.expectContextErr {
				if err == nil {
					t.Fatalf("expected context cancellation error, got nil")
				}
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected context cancellation error, got: %v", err)
				}
			} else if tt.expectPartialErr {
				var partialErr *PartialMarketDataError
				if err == nil {
					t.Fatalf("expected partial market data error, got nil")
				}
				if !errors.As(err, &partialErr) {
					t.Fatalf("expected PartialMarketDataError, got: %v", err)
				}
				if len(partialErr.Data) != len(data) {
					t.Fatalf("expected partial payload length %d, got %d", len(data), len(partialErr.Data))
				}
			} else if err != nil {
				t.Fatalf("FetchMarketData returned error: %v", err)
			}
			if len(data) < tt.expectedMinData || len(data) > tt.expectedMaxData {
				t.Fatalf("expected ticker count in [%d,%d], got %d", tt.expectedMinData, tt.expectedMaxData, len(data))
			}
			if tt.expectedRequests >= 0 {
				if got := int(requests.Load()); got != tt.expectedRequests {
					t.Fatalf("expected %d HTTP requests, got %d", tt.expectedRequests, got)
				}
			}
			if tt.expectedElapsedMax > 0 && elapsed > tt.expectedElapsedMax {
				t.Fatalf("expected elapsed <= %s, got %s", tt.expectedElapsedMax, elapsed)
			}
		})
	}
}

func TestGetExchangeBaseURL(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)

	tests := []struct {
		exchange string
		want     string
		wantOK   bool
	}{
		{"bitget", "https://api.bitget.com", true},
		{"binance", "https://api.binance.com", true},
		{"bybit", "https://api.bybit.com", true},
		{"okx", "https://www.okx.com", true},
		{"unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.exchange, func(t *testing.T) {
			got, ok := service.getExchangeBaseURL(tt.exchange)
			if ok != tt.wantOK {
				t.Fatalf("getExchangeBaseURL(%s) ok = %v, want %v", tt.exchange, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("getExchangeBaseURL(%s) = %s, want %s", tt.exchange, got, tt.want)
			}
		})
	}
}

func TestGetSupportedExchanges(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	service.exchanges["bitget"] = &ExchangeConnection{Name: "bitget"}
	service.exchanges["binance"] = &ExchangeConnection{Name: "binance"}

	exchanges := service.GetSupportedExchanges()
	if len(exchanges) != 2 {
		t.Fatalf("GetSupportedExchanges() returned %d exchanges, want 2", len(exchanges))
	}
}

func TestGetExchangeInfo(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	service.exchanges["bitget"] = &ExchangeConnection{
		Name:    "bitget",
		BaseURL: "https://api.bitget.com",
	}

	info, ok := service.GetExchangeInfo("bitget")
	if !ok {
		t.Fatal("GetExchangeInfo(bitget) returned false")
	}
	if info.Name != "bitget" {
		t.Fatalf("GetExchangeInfo.Name = %s, want bitget", info.Name)
	}

	_, ok = service.GetExchangeInfo("unknown")
	if ok {
		t.Fatal("GetExchangeInfo(unknown) should return false")
	}
}

func TestClose(t *testing.T) {
	t.Parallel()

	service := NewNativeCCXTService(5*time.Second, 1)
	service.exchanges["bitget"] = &ExchangeConnection{Name: "bitget"}

	if err := service.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}
