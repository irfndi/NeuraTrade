//go:build e2e_sqlite
// +build e2e_sqlite

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

const TestBaseURL = "http://localhost:8080"

func main() {
	fmt.Println("🧪 SQLite Full Feature Test Suite")
	fmt.Println("====================================")
	fmt.Println()

	tests := []struct {
		name   string
		method string
		path   string
		body   map[string]interface{}
	}{
		// User endpoints
		{"POST /api/v1/users/register", "POST", "/api/v1/users/register", map[string]interface{}{
			"email":            "test@example.com",
			"password":         "securepassword123",
			"telegram_chat_id": "123456789",
		}},
		{"POST /api/v1/users/login", "POST", "/api/v1/users/login", map[string]interface{}{}},
		{"GET /api/v1/users/profile", "GET", "/api/v1/users/profile", nil},

		// Wallet endpoints
		{"GET /api/v1/wallets", "GET", "/api/v1/wallets?chat_id=123456789", nil},
		{"POST /api/v1/wallets", "POST", "/api/v1/wallets", map[string]interface{}{
			"name":     "main-wallet",
			"exchange": "binance",
		}},
		{"POST /api/v1/wallets/connect_exchange", "POST", "/api/v1/wallets/connect_exchange", map[string]interface{}{
			"exchange":   "binance",
			"api_key":    "test_key",
			"api_secret": "test_secret",
		}},
		{"GET /api/v1/wallets/balance", "GET", "/api/v1/wallets/balance", nil},

		// Portfolio endpoints
		{"GET /api/v1/portfolio", "GET", "/api/v1/portfolio", nil},
		{"GET /api/v1/performance", "GET", "/api/v1/performance", nil},
		{"GET /api/v1/summary", "GET", "/api/v1/summary", nil},
		{"GET /api/v1/doctor", "GET", "/api/v1/doctor", nil},

		// Market endpoints
		{"GET /api/v1/market/prices", "GET", "/api/v1/market/prices", nil},
		{"GET /api/v1/market/ticker/binance/BTCUSDT", "GET", "/api/v1/market/ticker/binance/BTCUSDT", nil},
		{"GET /api/v1/market/tickers/binance", "GET", "/api/v1/market/tickers/binance", nil},
		{"GET /api/v1/market/orderbook/binance/BTCUSDT", "GET", "/api/v1/market/orderbook/binance/BTCUSDT", nil},
		{"GET /api/v1/market/workers/status", "GET", "/api/v1/market/workers/status", nil},

		// Telegram internal endpoints
		{"GET /api/v1/telegram/internal/users/123456789", "GET", "/api/v1/telegram/internal/users/123456789", nil},
		{"GET /api/v1/telegram/internal/wallets", "GET", "/api/v1/telegram/internal/wallets", nil},
		{"GET /api/v1/telegram/internal/doctor", "GET", "/api/v1/telegram/internal/doctor", nil},
		{"GET /api/v1/telegram/internal/portfolio", "GET", "/api/v1/telegram/internal/portfolio", nil},
	}

	passed := 0
	failed := 0

	for _, test := range tests {
		fmt.Printf("Testing: %s\n", test.name)

		var req *http.Request
		var err error

		if test.body != nil {
			jsonBody, _ := json.Marshal(test.body)
			req, err = http.NewRequest(test.method, TestBaseURL+test.path, bytes.NewBuffer(jsonBody))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req, err = http.NewRequest(test.method, TestBaseURL+test.path, nil)
		}

		if err != nil {
			fmt.Printf("  ❌ Failed to create request: %v\n", err)
			failed++
			continue
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("  ❌ Request failed: %v\n", err)
			failed++
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			fmt.Printf("  ✅ Status: %d\n", resp.StatusCode)
			passed++
		} else {
			fmt.Printf("  ❌ Status: %d (unexpected)\n", resp.StatusCode)
			failed++
		}
	}

	fmt.Println()
	fmt.Println("====================================")
	fmt.Printf("Total: %d | Passed: %d | Failed: %d\n", len(tests), passed, failed)

	if failed > 0 {
		fmt.Println("\n❌ Some tests failed")
		os.Exit(1)
	}
	fmt.Println("\n✅ All tests passed!")
}
