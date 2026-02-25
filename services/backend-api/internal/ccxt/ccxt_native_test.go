package ccxt

import (
	"context"
	"io"
	"net/http"
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
