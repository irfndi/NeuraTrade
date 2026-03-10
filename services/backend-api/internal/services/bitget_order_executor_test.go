package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBitgetOrderExecutor_PlaceOrderWithDetails_Validation(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	tests := []struct {
		name        string
		details     TradeDetails
		wantErr     bool
		errContains string
	}{
		{
			name: "zero amount",
			details: TradeDetails{
				Symbol:     "BTCUSDT",
				Side:       "buy",
				AmountUSDT: decimal.Zero,
			},
			wantErr:     true,
			errContains: "invalid order amount",
		},
		{
			name: "negative amount",
			details: TradeDetails{
				Symbol:     "BTCUSDT",
				Side:       "buy",
				AmountUSDT: decimal.NewFromFloat(-10),
			},
			wantErr:     true,
			errContains: "invalid order amount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.PlaceOrderWithDetails(context.Background(), tt.details)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBitgetOrderExecutor_Sign(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	// Test that sign produces a base64 encoded HMAC-SHA256 signature
	signature := executor.sign("test message")
	assert.NotEmpty(t, signature)
	// Signature should be base64 encoded
	assert.Regexp(t, "^[A-Za-z0-9+/]+=*$", signature)
}

func TestBitgetOrderExecutor_SetNotificationService(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	// Should not panic when setting nil service
	executor.SetNotificationService(nil)

	// Should not panic when setting a service
	ns := &NotificationService{}
	executor.SetNotificationService(ns)
}

func TestBitgetOrderExecutor_SetChatID(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	executor.SetChatID("123456789")
	assert.Equal(t, "123456789", executor.chatID)
}

func TestBitgetOrderExecutor_SetWalletBalance(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	executor.SetWalletBalance(1000.0)
	assert.Equal(t, 1000.0, executor.walletBalance)
}

func TestBitgetOrderExecutor_MinNotionalUsesDynamicEnv(t *testing.T) {
	t.Setenv("NEURATRADE_BITGET_FUTURES_MIN_NOTIONAL_USDT", "7.25")

	assert.True(t, bitgetFuturesMinUSDTNotional().Equal(decimal.NewFromFloat(7.25)))
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_SpotFallbackKeepsOriginalAmount(t *testing.T) {
	var spotOrderBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/account/account"):
			_, _ = w.Write([]byte(`{"code":"40001","msg":"symbol not exist","data":{}}`))
		case r.URL.Path == "/api/v2/spot/trade/place-order":
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &spotOrderBody))
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"orderId":"spot-123"}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	entryPrice := decimal.NewFromInt(1)
	orderID, err := executor.PlaceOrderWithDetails(context.Background(), TradeDetails{
		Symbol:            "SONIC/USDT",
		Side:              "buy",
		MarketType:        "futures",
		AllowSpotFallback: true,
		Leverage:          5,
		AmountUSDT:        decimal.NewFromInt(3),
		EntryPrice:        &entryPrice,
	})

	require.NoError(t, err)
	require.Equal(t, "spot-123", orderID)
	require.NotNil(t, spotOrderBody)
	assert.Equal(t, "3", spotOrderBody["size"])
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_NoOpWhenAlreadySynced(t *testing.T) {
	accountCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"crossed",
				"posMode":"one_way_mode",
				"crossMarginLeverage":"3",
				"isolatedLongLever":"5"
			}}`))
		case "/api/v2/mix/account/set-leverage":
			t.Fatalf("unexpected leverage mutation request")
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	leverage, status, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 5, "long", bitgetFuturesOrderMarginMode)
	require.NoError(t, err)
	assert.Equal(t, 5, leverage)
	assert.Equal(t, "exchange confirmed", status)
	assert.Equal(t, 1, accountCalls)
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_SetsAndVerifiesIntendedIsolatedMode(t *testing.T) {
	accountCalls := 0
	setCalls := 0
	var setBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			currentIsolatedShort := "0"
			if accountCalls > 1 {
				currentIsolatedShort = "5"
			}
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"crossed",
				"posMode":"one_way_mode",
				"crossMarginLeverage":"5",
				"isolatedShortLever":"` + currentIsolatedShort + `"
			}}`))
		case "/api/v2/mix/account/set-leverage":
			setCalls++
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &setBody))
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok"}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	leverage, status, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 5, "short", bitgetFuturesOrderMarginMode)
	require.NoError(t, err)
	assert.Equal(t, 5, leverage)
	assert.Equal(t, "exchange synced", status)
	assert.Equal(t, 2, accountCalls)
	assert.Equal(t, 1, setCalls)
	require.NotNil(t, setBody)
	assert.Equal(t, "BTCUSDT", setBody["symbol"])
	assert.Equal(t, "USDT-FUTURES", setBody["productType"])
	assert.Equal(t, "USDT", setBody["marginCoin"])
	assert.Equal(t, "5", setBody["leverage"])
	assert.Equal(t, "short", setBody["holdSide"])
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_RejectsNonPositiveDesiredLeverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request path %s", r.URL.Path)
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	_, _, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 0, "long", bitgetFuturesOrderMarginMode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "desired leverage must be positive")

	_, _, err = executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", -5, "long", bitgetFuturesOrderMarginMode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "desired leverage must be positive")
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_WrapsAccountFetchFailures(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "non_00000_code",
			response: `{"code":"30001","msg":"error","data":{}}`,
		},
		{
			name:     "malformed_json",
			response: `{"code":"00000","msg":"ok","data":{`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/mix/account/account":
					accountCalls++
					_, _ = w.Write([]byte(tt.response))
				default:
					t.Fatalf("unexpected request path %s", r.URL.Path)
				}
			}))
			defer server.Close()

			executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
			executor.baseURL = server.URL

			_, _, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 5, "long", bitgetFuturesOrderMarginMode)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to get futures account")
			assert.Equal(t, 1, accountCalls)
		})
	}
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_WrapsSetLeverageFailures(t *testing.T) {
	accountCalls := 0
	setCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"crossed",
				"posMode":"one_way_mode",
				"crossMarginLeverage":"3"
			}}`))
		case "/api/v2/mix/account/set-leverage":
			setCalls++
			_, _ = w.Write([]byte(`{"code":"30001","msg":"failed"}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	_, _, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 5, "long", bitgetFuturesOrderMarginMode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set futures leverage")
	assert.Equal(t, 1, accountCalls)
	assert.Equal(t, 1, setCalls)
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_ErrOnVerificationMismatch(t *testing.T) {
	accountCalls := 0
	setCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			current := "3"
			if accountCalls > 1 {
				current = "4"
			}
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"crossed",
				"posMode":"one_way_mode",
				"crossMarginLeverage":"` + current + `"
			}}`))
		case "/api/v2/mix/account/set-leverage":
			setCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok"}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	_, _, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 5, "long", bitgetFuturesOrderMarginMode)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verification mismatch")
	assert.Equal(t, 2, accountCalls)
	assert.Equal(t, 1, setCalls)
}

func TestBitgetOrderExecutor_SyncFuturesLeverageForDetails_NormalizesSideAliases(t *testing.T) {
	accountCalls := 0
	setCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"isolated",
				"posMode":"one_way_mode",
				"isolatedShortLever":"5"
			}}`))
		case "/api/v2/mix/account/set-leverage":
			setCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok"}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	leverage, status, err := executor.syncFuturesLeverageForDetails(context.Background(), "BTCUSDT", TradeDetails{
		Side:     "open_short",
		Leverage: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, leverage)
	assert.Equal(t, "exchange confirmed", status)
	assert.Equal(t, 1, accountCalls)
	assert.Zero(t, setCalls)
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_BlocksOnLeverageSyncFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			_, _ = w.Write([]byte(`{"code":"40001","msg":"permission denied","data":{}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	entryPrice := decimal.NewFromInt(1)
	orderID, err := executor.PlaceOrderWithDetails(context.Background(), TradeDetails{
		Symbol:     "BTC/USDT",
		Side:       "buy",
		MarketType: "futures",
		Leverage:   5,
		AmountUSDT: decimal.NewFromInt(10),
		EntryPrice: &entryPrice,
	})

	require.Error(t, err)
	assert.Empty(t, orderID)
	assert.Contains(t, err.Error(), "failed to sync futures leverage")
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_RiskReductionSkipsLeverageSync(t *testing.T) {
	accountCalls := 0
	var orderBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			t.Fatalf("risk reduction orders should not sync leverage")
		case "/api/v2/mix/market/contracts":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{
				"sizeMultiplier":"1",
				"minTradeNum":"1",
				"volumePlace":"0",
				"pricePlace":"2"
			}]}`))
		case "/api/v2/mix/order/place-order":
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &orderBody))
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"orderId":"close-123"}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	entryPrice := decimal.NewFromInt(2)
	orderID, err := executor.PlaceOrderWithDetails(context.Background(), TradeDetails{
		Symbol:     "BTC/USDT",
		Side:       "sell",
		MarketType: "futures",
		TradeType:  "risk_reduction",
		Amount:     decimal.NewFromInt(3),
		AmountUSDT: decimal.NewFromInt(10),
		EntryPrice: &entryPrice,
	})

	require.NoError(t, err)
	assert.Equal(t, "close-123", orderID)
	assert.Zero(t, accountCalls)
	require.NotNil(t, orderBody)
	assert.Equal(t, "close", orderBody["tradeSide"])
	assert.Equal(t, "YES", orderBody["reduceOnly"])
	assert.Equal(t, "long", orderBody["holdSide"])
	assert.Equal(t, bitgetFuturesOrderMarginMode, orderBody["marginMode"])
}

func TestBitgetFuturesAccount_EffectiveLeverageForCrossMarginPrefersCrossValues(t *testing.T) {
	account := bitgetFuturesAccount{
		CrossMarginLeverage:   9,
		CrossedMarginLeverage: 11,
		LongLeverage:          5,
		ShortLeverage:         6,
		IsolatedLongLeverage:  25,
		IsolatedShortLeverage: 30,
	}

	assert.Equal(t, 11, account.effectiveLeverageForMarginMode("long", "crossed"))
	assert.Equal(t, 11, account.effectiveLeverageForMarginMode("short", "crossed"))
}

func TestShouldFallbackToSpot_NarrowsMissingMarketDetection(t *testing.T) {
	assert.True(t, shouldFallbackToSpot(errors.New("bitget futures account API error: symbol not exist (code: 40001)")))
	assert.True(t, shouldFallbackToSpot(errors.New("failed to get contract info: contract does not exist")))
	assert.False(t, shouldFallbackToSpot(errors.New("bitget API error: holdSide not exist")))
	assert.False(t, shouldFallbackToSpot(errors.New("bitget API error: order not exist")))
}

func TestBitgetOrderExecutor_FormatTradeNotification_UsesEffectiveLeverage(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	msg := executor.formatTradeNotification(TradeDetails{
		Symbol:             "BTC/USDT",
		Side:               "buy",
		MarketType:         "futures",
		TradeType:          "scalping",
		Leverage:           5,
		EffectiveLeverage:  7,
		LeverageSyncStatus: "exchange synced",
		AmountUSDT:         decimal.NewFromInt(100),
	}, "ord-123")

	assert.Contains(t, msg, "Futures (7x)")
	assert.Contains(t, msg, "⚙️ Leverage: 7x (exchange synced)")
	assert.NotContains(t, msg, "Futures (5x)")
}

func TestBitgetOrderExecutor_FormatTradeNotification_FallsBackToConfiguredLeverage(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	msg := executor.formatTradeNotification(TradeDetails{
		Symbol:             "BTC/USDT",
		Side:               "buy",
		MarketType:         "futures",
		TradeType:          "scalping",
		Leverage:           5,
		EffectiveLeverage:  0,
		LeverageSyncStatus: "not synced",
		AmountUSDT:         decimal.NewFromInt(100),
	}, "ord-456")

	assert.Contains(t, msg, "Futures (5x)")
	assert.Contains(t, msg, "⚙️ Leverage: 5x (not synced)")
	assert.NotContains(t, msg, "Futures (0x)")
}

func TestBitgetOrderExecutor_FormatTradeNotification_SuppressesZeroLeverageLabel(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	msg := executor.formatTradeNotification(TradeDetails{
		Symbol:             "BTC/USDT",
		Side:               "buy",
		MarketType:         "futures",
		TradeType:          "scalping",
		Leverage:           0,
		EffectiveLeverage:  0,
		LeverageSyncStatus: "",
		AmountUSDT:         decimal.NewFromInt(100),
	}, "ord-789")

	assert.Contains(t, msg, "📍 Market: Futures")
	assert.NotContains(t, msg, "Futures (0x)")
	assert.NotContains(t, msg, "⚙️ Leverage:")
}

func TestContractInfo(t *testing.T) {
	info := &ContractInfo{
		SizeMultiplier: decimal.NewFromFloat(0.1),
		MinTradeNum:    decimal.NewFromInt(1),
		VolumePlace:    0,
	}

	assert.Equal(t, "0.1", info.SizeMultiplier.String())
	assert.Equal(t, "1", info.MinTradeNum.String())
	assert.Equal(t, 0, info.VolumePlace)
}

func TestTradeDetails(t *testing.T) {
	tp := decimal.NewFromFloat(50000)
	sl := decimal.NewFromFloat(45000)
	entry := decimal.NewFromFloat(48000)

	details := TradeDetails{
		Exchange:      "bitget",
		Symbol:        "BTC/USDT",
		Side:          "buy",
		OrderType:     "market",
		MarketType:    "futures",
		Leverage:      5,
		AmountUSDT:    decimal.NewFromFloat(100),
		WalletPercent: 5.0,
		EntryPrice:    &entry,
		TakeProfit:    &tp,
		StopLoss:      &sl,
		TradeType:     "scalping",
		Confidence:    0.85,
		Reasoning:     "Strong bullish momentum",
		IsPaperTrade:  false,
	}

	assert.Equal(t, "bitget", details.Exchange)
	assert.Equal(t, "BTC/USDT", details.Symbol)
	assert.Equal(t, "buy", details.Side)
	assert.Equal(t, "market", details.OrderType)
	assert.Equal(t, "futures", details.MarketType)
	assert.Equal(t, 5, details.Leverage)
	assert.Equal(t, "100", details.AmountUSDT.String())
	assert.Equal(t, 5.0, details.WalletPercent)
	assert.Equal(t, "48000", details.EntryPrice.String())
	assert.Equal(t, "50000", details.TakeProfit.String())
	assert.Equal(t, "45000", details.StopLoss.String())
	assert.Equal(t, "scalping", details.TradeType)
	assert.Equal(t, 0.85, details.Confidence)
	assert.Equal(t, "Strong bullish momentum", details.Reasoning)
	assert.False(t, details.IsPaperTrade)
}

func TestFormatPrice(t *testing.T) {
	tests := []struct {
		price    decimal.Decimal
		expected string
	}{
		{decimal.NewFromFloat(65000), "$65000.00"},
		{decimal.NewFromFloat(1.5234), "$1.5234"},
		{decimal.NewFromFloat(0.00012345), "$0.000123"},
		{decimal.NewFromFloat(0.0001), "$0.000100"},
		{decimal.NewFromFloat(1000.5), "$1000.50"},
	}

	for _, tt := range tests {
		result := formatPrice(tt.price)
		assert.Equal(t, tt.expected, result, "Price: %s", tt.price.String())
	}
}
