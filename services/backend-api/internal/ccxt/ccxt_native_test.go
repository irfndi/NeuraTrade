package ccxt

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/config"
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
