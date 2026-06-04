package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		name           string
		details        TradeDetails
		wantErr        bool
		errContains    string
		errNotContains string
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
		{
			name: "non-de-risk with empty IntentID rejects",
			details: TradeDetails{
				Symbol:     "BTCUSDT",
				Side:       "buy",
				AmountUSDT: decimal.NewFromFloat(100),
			},
			wantErr:     true,
			errContains: "IntentID is required",
		},
		{
			name: "de-risk (TradeType=risk_reduction) with empty IntentID synthesizes ID",
			details: TradeDetails{
				Symbol:     "BTCUSDT",
				Side:       "sell",
				AmountUSDT: decimal.NewFromFloat(100),
				TradeType:  "risk_reduction",
			},
			wantErr:        true,
			errNotContains: "IntentID is required",
		},
		{
			name: "de-risk (ReduceOnly=true) with empty IntentID synthesizes ID",
			details: TradeDetails{
				Symbol:     "ETHUSDT",
				Side:       "buy",
				AmountUSDT: decimal.NewFromFloat(50),
				ReduceOnly: true,
			},
			wantErr:        true,
			errNotContains: "IntentID is required",
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
				if tt.errNotContains != "" {
					assert.NotContains(t, err.Error(), tt.errNotContains)
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

func TestBitgetOrderExecutor_PlaceOrderWithDetails_BlocksUnprotectedSpotFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/market/contracts"):
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{
				"sizeMultiplier":"1",
				"minTradeNum":"1",
				"volumePlace":"0",
				"pricePlace":"2"
			}]}`))
		case r.URL.Path == "/api/v2/mix/order/place-order":
			_, _ = w.Write([]byte(`{"code":"40001","msg":"symbol not exist","data":{}}`))
		case r.URL.Path == "/api/v2/spot/trade/place-order":
			t.Fatalf("unprotected spot fallback must not place a spot order")
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
		Leverage:          0,
		AmountUSDT:        decimal.NewFromInt(3),
		EntryPrice:        &entryPrice,
		IntentID:          "test-intent-blocks-unprotected-spot",
	})

	require.Error(t, err)
	assert.Empty(t, orderID)
	assert.Contains(t, err.Error(), "protected spot fallback unavailable")
	assert.Contains(t, err.Error(), "exchange-side TP/SL protection")
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_BlocksUnprotectedSpotFallbackForSupportedAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/market/contracts"):
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{
				"sizeMultiplier":"1",
				"minTradeNum":"1",
				"volumePlace":"0",
				"pricePlace":"2"
			}]}`))
		case r.URL.Path == "/api/v2/mix/order/place-order":
			_, _ = w.Write([]byte(`{"code":"40001","msg":"symbol not exist","data":{}}`))
		case r.URL.Path == "/api/v2/spot/trade/place-order":
			t.Fatalf("unprotected spot fallback must not place a spot order")
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
		Side:              "open_long",
		MarketType:        "futures",
		AllowSpotFallback: true,
		Leverage:          0,
		AmountUSDT:        decimal.NewFromInt(3),
		EntryPrice:        &entryPrice,
		IntentID:          "test-intent-blocks-alias",
	})

	require.Error(t, err)
	assert.Empty(t, orderID)
	assert.Contains(t, err.Error(), "protected spot fallback unavailable")
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_BlocksUnprotectedSpotFallbackBeforeShortAlias(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/market/contracts"):
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{
				"sizeMultiplier":"1",
				"minTradeNum":"1",
				"volumePlace":"0",
				"pricePlace":"2"
			}]}`))
		case r.URL.Path == "/api/v2/mix/order/place-order":
			_, _ = w.Write([]byte(`{"code":"40001","msg":"symbol not exist","data":{}}`))
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
		Side:              "open_short",
		MarketType:        "futures",
		AllowSpotFallback: true,
		Leverage:          0,
		AmountUSDT:        decimal.NewFromInt(3),
		EntryPrice:        &entryPrice,
		IntentID:          "test-intent-blocks-short-alias",
	})

	require.Error(t, err)
	assert.Empty(t, orderID)
	assert.Contains(t, err.Error(), "protected spot fallback unavailable")
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_NoOpWhenAlreadySynced(t *testing.T) {
	accountCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"isolated",
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
			currentMarginMode := "crossed"
			currentIsolatedShort := "0"
			if accountCalls > 1 {
				currentMarginMode = "isolated"
				currentIsolatedShort = "5"
			}
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"` + currentMarginMode + `",
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

func TestBitgetOrderExecutor_EnsureFuturesLeverage_UsesExchangeModeOnVerificationMismatch(t *testing.T) {
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

	leverage, status, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 5, "long", bitgetFuturesOrderMarginMode)
	require.NoError(t, err)
	assert.Equal(t, 4, leverage)
	assert.Equal(t, "exchange preserved", status)
	assert.Equal(t, 2, accountCalls)
	assert.Equal(t, 1, setCalls)
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_UsesExchangeModeOnMarginModeMismatch(t *testing.T) {
	accountCalls := 0
	setCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"crossed",
				"posMode":"one_way_mode",
				"crossMarginLeverage":"5"
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

	leverage, status, err := executor.ensureFuturesLeverage(context.Background(), "BTCUSDT", 5, "long", bitgetFuturesOrderMarginMode)
	require.NoError(t, err)
	assert.Equal(t, 5, leverage)
	assert.Equal(t, "exchange preserved", status)
	assert.Equal(t, 2, accountCalls)
	assert.Equal(t, 1, setCalls)
}

func TestBitgetOrderExecutor_EnsureFuturesLeverage_RejectsHigherExchangePreservedLeverage(t *testing.T) {
	accountCalls := 0
	setCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{
				"marginMode":"crossed",
				"posMode":"one_way_mode",
				"crossMarginLeverage":"10"
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
	assert.Contains(t, err.Error(), "exchange preserved higher 10x crossed")
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
		IntentID:   "test-intent-leverage-sync-fail",
	})

	require.Error(t, err)
	assert.Empty(t, orderID)
	assert.Contains(t, err.Error(), "failed to sync futures leverage")
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_AcceptsNumericLeverageFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"marginMode":"isolated","posMode":"one_way_mode","crossMarginLeverage":5,"crossedMarginLeverage":5,"longLeverage":5,"shortLeverage":5,"isolatedLongLever":5,"isolatedShortLever":5}}`))
		case "/api/v2/mix/market/contracts":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{"sizeMultiplier":"1","minTradeNum":"1","volumePlace":"0","pricePlace":"2"}]}`))
		case "/api/v2/mix/order/place-order":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"orderId":"futures-123"}}`))
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
		IntentID:   "test-intent-numeric-leverage",
	})

	require.NoError(t, err)
	assert.Equal(t, "futures-123", orderID)
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_DoesNotSpotFallbackOnLeverageSyncMissingSymbol(t *testing.T) {
	spotCalled := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			_, _ = w.Write([]byte(`{"code":"40001","msg":"symbol not exist","data":{}}`))
		case "/api/v2/spot/trade/place-order":
			spotCalled = true
			t.Fatalf("spot fallback must not run when leverage sync fails")
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	entryPrice := decimal.NewFromInt(1)
	orderID, err := executor.PlaceOrderWithDetails(context.Background(), TradeDetails{
		Symbol:            "BTC/USDT",
		Side:              "buy",
		MarketType:        "futures",
		AllowSpotFallback: true,
		Leverage:          5,
		AmountUSDT:        decimal.NewFromInt(10),
		EntryPrice:        &entryPrice,
		IntentID:          "test-intent-leverage-sync-missing",
	})

	require.Error(t, err)
	assert.Empty(t, orderID)
	assert.False(t, spotCalled)
	assert.Contains(t, err.Error(), "failed to sync futures leverage")
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_FuturesZeroLeverageSkipsSync(t *testing.T) {
	accountCalls := 0
	var orderBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			t.Fatalf("zero-leverage futures orders should not sync leverage")
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
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"orderId":"futures-zero-123"}}`))
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
		Side:       "buy",
		MarketType: "futures",
		Leverage:   0,
		AmountUSDT: decimal.NewFromInt(10),
		EntryPrice: &entryPrice,
		IntentID:   "test-intent-zero-leverage",
	})

	require.NoError(t, err)
	assert.Equal(t, "futures-zero-123", orderID)
	assert.Zero(t, accountCalls)
	require.NotNil(t, orderBody)
	assert.Equal(t, bitgetFuturesOrderMarginMode, orderBody["marginMode"])
}

func TestBitgetOrderExecutor_PlaceOrder_DelegatesToDetailsPath(t *testing.T) {
	accountCalls := 0
	var orderBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/account/account":
			accountCalls++
			t.Fatalf("default PlaceOrder path should not sync leverage when no leverage is configured")
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
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"orderId":"delegated-123"}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	entryPrice := decimal.NewFromInt(2)
	orderID, err := executor.PlaceOrder(
		context.Background(),
		"bitget",
		"BTC/USDT",
		"buy",
		"market",
		decimal.NewFromInt(10),
		&entryPrice,
	)

	require.NoError(t, err)
	assert.Equal(t, "delegated-123", orderID)
	assert.Zero(t, accountCalls)
	require.NotNil(t, orderBody)
	assert.Equal(t, "BTCUSDT", orderBody["symbol"])
	assert.Equal(t, bitgetFuturesOrderMarginMode, orderBody["marginMode"])
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
		IntentID:   "test-intent-risk-reduction",
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

func TestBitgetOrderExecutor_PlaceFuturesOrderWithTPSL_PropagatesBumpedNotional(t *testing.T) {
	var orderBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"orderId":"min-bump-123"}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	entryPrice := decimal.NewFromInt(2)
	details := &TradeDetails{
		Symbol:     "BTC/USDT",
		Side:       "buy",
		MarketType: "futures",
		AmountUSDT: decimal.NewFromInt(1),
		EntryPrice: &entryPrice,
	}

	orderID, err := executor.placeFuturesOrderWithTPSL(context.Background(), "BTCUSDT", details)

	require.NoError(t, err)
	assert.Equal(t, "min-bump-123", orderID)
	assert.True(t, details.AmountUSDT.Equal(bitgetFuturesMinUSDTNotional()))
	require.NotNil(t, orderBody)
	assert.Equal(t, "BTCUSDT", orderBody["symbol"])
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

func newBitgetFuturesTestServer(t *testing.T, orderID string) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var orderBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/market/contracts":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{
				"sizeMultiplier":"0.001",
				"minTradeNum":"0.1",
				"volumePlace":"1",
				"pricePlace":"2"
			}]}`))
		case "/api/v2/mix/order/place-order":
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(body, &orderBody))
			_, _ = w.Write([]byte(fmt.Sprintf(`{"code":"00000","msg":"ok","data":{"orderId":"%s"}}`, orderID)))
		case "/api/v2/mix/order/orders-plan-pending":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{"nextFlag":"false","orderList":[{"orderId":"tpsl-plan-1","holdSide":"long"}]}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	return server, &orderBody
}

func newBitgetFuturesTestExecutor(serverURL string) *BitgetOrderExecutor {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = serverURL
	return executor
}

func TestBitgetOrderExecutor_PlaceFuturesOrderWithTPSL_UsesCorrectPresetFieldNames(t *testing.T) {
	server, orderBodyPtr := newBitgetFuturesTestServer(t, "tpsl-field-123")
	defer server.Close()

	executor := newBitgetFuturesTestExecutor(server.URL)

	entryPrice := decimal.NewFromFloat(48000)
	tpPrice := decimal.NewFromFloat(52000)
	slPrice := decimal.NewFromFloat(46000)

	details := &TradeDetails{
		Symbol:     "BTC/USDT",
		Side:       "buy",
		MarketType: "futures",
		AmountUSDT: decimal.NewFromInt(10),
		EntryPrice: &entryPrice,
		TakeProfit: &tpPrice,
		StopLoss:   &slPrice,
	}

	orderID, err := executor.placeFuturesOrderWithTPSL(context.Background(), "BTCUSDT", details)

	require.NoError(t, err)
	assert.Equal(t, "tpsl-field-123", orderID)
	require.NotNil(t, *orderBodyPtr)
	orderBody := *orderBodyPtr

	assert.Equal(t, "52000.00", orderBody["presetStopSurplusPrice"], "TP must use presetStopSurplusPrice")
	assert.Equal(t, "52000.00", orderBody["presetStopSurplusExecutePrice"], "TP must set execute price")
	assert.Equal(t, "46000.00", orderBody["presetStopLossPrice"], "SL must use presetStopLossPrice")

	_, hasSLExec := orderBody["presetStopLossExecutePrice"]
	assert.False(t, hasSLExec, "SL must NOT set execute price (market execution for guaranteed fill)")

	_, hasInvalidTP := orderBody["presetTakeProfitPrice"]
	assert.False(t, hasInvalidTP, "presetTakeProfitPrice must not be sent (invalid Bitget API field)")
}

func TestBitgetOrderExecutor_PlaceFuturesOrderWithTPSL_SkipsTPSLForRiskReduction(t *testing.T) {
	server, orderBodyPtr := newBitgetFuturesTestServer(t, "risk-reduce-123")
	defer server.Close()

	executor := newBitgetFuturesTestExecutor(server.URL)

	entryPrice := decimal.NewFromFloat(48000)
	tpPrice := decimal.NewFromFloat(52000)
	slPrice := decimal.NewFromFloat(46000)

	details := &TradeDetails{
		Symbol:     "BTC/USDT",
		Side:       "sell",
		MarketType: "futures",
		Amount:     decimal.NewFromInt(1),
		AmountUSDT: decimal.NewFromInt(10),
		EntryPrice: &entryPrice,
		TakeProfit: &tpPrice,
		StopLoss:   &slPrice,
		ReduceOnly: true,
		TradeType:  "risk_reduction",
	}

	_, err := executor.placeFuturesOrderWithTPSL(context.Background(), "BTCUSDT", details)

	require.NoError(t, err)
	require.NotNil(t, *orderBodyPtr)
	orderBody := *orderBodyPtr

	_, hasTP := orderBody["presetStopSurplusPrice"]
	_, hasSL := orderBody["presetStopLossPrice"]
	assert.False(t, hasTP, "risk reduction orders must not include TP")
	assert.False(t, hasSL, "risk reduction orders must not include SL")

	_, hasTPExec := orderBody["presetStopSurplusExecutePrice"]
	_, hasSLExec := orderBody["presetStopLossExecutePrice"]
	assert.False(t, hasTPExec, "risk reduction orders must not include TP execute price")
	assert.False(t, hasSLExec, "risk reduction orders must not include SL execute price")

	assert.Equal(t, "close", orderBody["tradeSide"])
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

// PR-4: listPositionTPSLPlans now omits the planType query param to
// catch all plan types (profit_plan/loss_plan/profit_loss/pos_profit/
// pos_loss) and filters by holdSide client-side. This test guards
// against regression of the W0-F secondary root cause.
func TestBitgetOrderExecutor_ListPositionTPSLPlans_NoPlanTypeFilter(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path + "?" + r.URL.RawQuery
		_, _ = w.Write([]byte(`{
			"code": "00000",
			"msg": "ok",
			"data": {
				"entrustedList": [
					{"orderId": "tp-1", "planType": "pos_profit", "holdSide": "long", "triggerPrice": "51000"},
					{"orderId": "sl-1", "planType": "pos_loss", "holdSide": "long", "triggerPrice": "49000"},
					{"orderId": "other-1", "planType": "pos_profit", "holdSide": "short", "triggerPrice": "1100"}
				]
			}
		}`))
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	plans, err := executor.listPositionTPSLPlans(context.Background(), "BTCUSDT", "long")
	require.NoError(t, err)
	require.Len(t, plans, 2, "should filter to long holdSide only")
	// Verify the request did NOT specify planType (regression guard).
	assert.NotContains(t, requestedPath, "planType=",
		"listPositionTPSLPlans must omit the planType filter to catch all plan types")
	// Verify the returned IDs are the long-side ones.
	ids := make([]string, 0, len(plans))
	for _, p := range plans {
		ids = append(ids, mapStringAny(p, "orderId", "orderID", "id"))
	}
	assert.ElementsMatch(t, []string{"tp-1", "sl-1"}, ids)
}

// PR-4: modifyPositionTPSL updates existing plan orders via the
// /api/v2/mix/order/modify-tpsl-order endpoint. This test verifies
// the per-plan modify request body format and the no-op skip when
// the trigger price already matches.
func TestBitgetOrderExecutor_ModifyPositionTPSL_SendsModifyRequestsForStalePlans(t *testing.T) {
	var modifyRequests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v2/mix/order/orders-plan-pending":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "ok",
				"data": {
					"entrustedList": [
						{"orderId": "tp-1", "planType": "pos_profit", "holdSide": "long", "triggerPrice": "50000"},
						{"orderId": "sl-1", "planType": "pos_loss", "holdSide": "long", "triggerPrice": "49500"}
					]
				}
			}`))
		case r.URL.Path == "/api/v2/mix/order/modify-tpsl-order":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			modifyRequests = append(modifyRequests, payload)
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	// Force the trigger price to differ from the mocked 50000/49500 so
	// both plans are considered stale and trigger a modify call.
	modified, err := executor.modifyPositionTPSL(
		context.Background(),
		"BTCUSDT",
		"long",
		decimal.NewFromInt(49000), // new SL trigger (was 49500)
		decimal.NewFromInt(52000), // new TP trigger (was 50000)
		&ContractInfo{PricePlace: 2},
	)
	require.NoError(t, err)
	assert.True(t, modified, "modified should be true when at least one plan was updated")
	require.Len(t, modifyRequests, 2, "should issue one modify per stale plan")

	// Verify both request bodies carry the expected shape (orderId,
	// triggerPrice, planType, mark_price triggerType).
	orderIDs := make([]string, 0, 2)
	for _, req := range modifyRequests {
		orderIDs = append(orderIDs, req["orderId"].(string))
		assert.Equal(t, "mark_price", req["triggerType"])
		assert.Equal(t, "USDT-FUTURES", req["productType"])
		assert.NotEmpty(t, req["triggerPrice"])
		assert.NotEmpty(t, req["planType"])
	}
	assert.ElementsMatch(t, []string{"tp-1", "sl-1"}, orderIDs)
}

// PR-4: when both plans are already at the target price, the modify
// path is a no-op (no modify-tpsl-order requests) BUT still returns
// true. The true signal means "already-synced" — SyncPositionProtection
// uses it to avoid the wasteful cancel+recreate fallback (Bug 2).
func TestBitgetOrderExecutor_ModifyPositionTPSL_SkipsPlansAlreadyAtTarget(t *testing.T) {
	var modifyCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/order/orders-plan-pending":
			// Trigger prices already match the targets we'll pass below.
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "ok",
				"data": {
					"entrustedList": [
						{"orderId": "tp-1", "planType": "pos_profit", "holdSide": "long", "triggerPrice": "52000.00"},
						{"orderId": "sl-1", "planType": "pos_loss", "holdSide": "long", "triggerPrice": "49000.00"}
					]
				}
			}`))
		case "/api/v2/mix/order/modify-tpsl-order":
			modifyCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	modified, err := executor.modifyPositionTPSL(
		context.Background(),
		"BTCUSDT",
		"long",
		decimal.NewFromInt(49000),
		decimal.NewFromInt(52000),
		&ContractInfo{PricePlace: 2},
	)
	require.NoError(t, err)
	assert.True(t, modified, "modified should be true (already-synced signal) when all plans are at target")
	assert.Equal(t, 0, modifyCalls, "no modify requests should be sent when triggers already match")
}

// PR-4 Bug 1: if the caller wants both TP and SL but only a TP plan
// exists, modifyPositionTPSL must NOT return true (it must fall
// back to cancel+recreate). Returning true here would leave the
// position with only the old TP and no SL — a correctness bug that
// could cause real financial loss.
func TestBitgetOrderExecutor_ModifyPositionTPSL_FallsBackWhenSLMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/order/orders-plan-pending":
			// Only a TP plan exists; no SL.
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "ok",
				"data": {
					"entrustedList": [
						{"orderId": "tp-1", "planType": "pos_profit", "holdSide": "long", "triggerPrice": "50000"}
					]
				}
			}`))
		case "/api/v2/mix/order/modify-tpsl-order":
			t.Fatalf("modify-tpsl-order must NOT be called when SL plan is missing")
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	modified, err := executor.modifyPositionTPSL(
		context.Background(),
		"BTCUSDT",
		"long",
		decimal.NewFromInt(49000), // SL requested
		decimal.NewFromInt(52000), // TP requested
		&ContractInfo{PricePlace: 2},
	)
	require.NoError(t, err)
	assert.False(t, modified, "missing-leg must fall back to cancel+recreate (Bug 1)")
}

// PR-4 Bug 1 (symmetric): if the caller wants only TP but both TP
// and SL plans exist, modifyPositionTPSL must fall back because the
// SL plan can't be removed via the modify-tpsl-order endpoint.
func TestBitgetOrderExecutor_ModifyPositionTPSL_FallsBackWhenSLExtra(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/order/orders-plan-pending":
			// Both plans exist; caller wants only TP (SL=0).
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "ok",
				"data": {
					"entrustedList": [
						{"orderId": "tp-1", "planType": "pos_profit", "holdSide": "long", "triggerPrice": "50000"},
						{"orderId": "sl-1", "planType": "pos_loss", "holdSide": "long", "triggerPrice": "49000"}
					]
				}
			}`))
		case "/api/v2/mix/order/modify-tpsl-order":
			t.Fatalf("modify-tpsl-order must NOT be called when caller wants to remove the SL")
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	modified, err := executor.modifyPositionTPSL(
		context.Background(),
		"BTCUSDT",
		"long",
		decimal.NewFromInt(0),     // SL = 0 (want to remove)
		decimal.NewFromInt(52000), // TP requested
		&ContractInfo{PricePlace: 2},
	)
	require.NoError(t, err)
	assert.False(t, modified, "extra-leg must fall back to cancel+recreate (Bug 1)")
}

// PR-4 (gemini HIGH): combined TP+SL plans (planType contains
// "profit_loss") cannot be modified in-place with the single-trigger
// body the endpoint accepts. Must fall back to cancel+recreate.
func TestBitgetOrderExecutor_ModifyPositionTPSL_FallsBackOnCombinedPlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/order/orders-plan-pending":
			_, _ = w.Write([]byte(`{
				"code": "00000",
				"msg": "ok",
				"data": {
					"entrustedList": [
						{"orderId": "tpsl-1", "planType": "profit_loss", "holdSide": "long", "triggerPrice": "50000"}
					]
				}
			}`))
		case "/api/v2/mix/order/modify-tpsl-order":
			t.Fatalf("modify-tpsl-order must NOT be called for combined TP+SL plans")
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	modified, err := executor.modifyPositionTPSL(
		context.Background(),
		"BTCUSDT",
		"long",
		decimal.NewFromInt(49000),
		decimal.NewFromInt(52000),
		&ContractInfo{PricePlace: 2},
	)
	require.NoError(t, err)
	assert.False(t, modified, "combined plans must fall back to cancel+recreate")
}

// PR-4: modifyPositionTPSL on a position with no existing plans must
// return (false, nil) so the caller can fall through to
// placePositionTPSL. This is the first-ever-sync case.
func TestBitgetOrderExecutor_ModifyPositionTPSL_NoExistingPlans(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/order/orders-plan-pending":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		case "/api/v2/mix/order/modify-tpsl-order":
			t.Fatalf("modify-tpsl-order must not be called when no plans exist")
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	modified, err := executor.modifyPositionTPSL(
		context.Background(),
		"BTCUSDT",
		"long",
		decimal.NewFromInt(49000),
		decimal.NewFromInt(52000),
		&ContractInfo{PricePlace: 2},
	)
	require.NoError(t, err)
	assert.False(t, modified)
}

// PR-4: SyncPositionProtection now tries modify-in-place first, and
// only falls back to cancel+place when the modify path either errors
// or returns modified=false (no plans existed). This test exercises
// the happy modify path: no cancel and no place-pos-tpsl calls
// should be made.
func TestBitgetOrderExecutor_SyncPositionProtection_ModifyInPlaceHappyPath(t *testing.T) {
	var modifyCalls, cancelCalls, placeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/market/contracts":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{
				"sizeMultiplier":"1","minTradeNum":"1","volumePlace":"0","pricePlace":"2"
			}]}`))
		case "/api/v2/mix/order/orders-plan-pending":
			// Existing plans with stale triggers → modify path applies.
			_, _ = w.Write([]byte(`{
				"code":"00000","msg":"ok","data":{"entrustedList":[
					{"orderId":"tp-1","planType":"pos_profit","holdSide":"long","triggerPrice":"50000.00"},
					{"orderId":"sl-1","planType":"pos_loss","holdSide":"long","triggerPrice":"49500.00"}
				]}}`))
		case "/api/v2/mix/order/modify-tpsl-order":
			modifyCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		case "/api/v2/mix/order/cancel-plan-order":
			cancelCalls++
		case "/api/v2/mix/order/place-pos-tpsl":
			placeCalls++
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	err := executor.SyncPositionProtection(
		context.Background(),
		"bitget",
		ManagedOpenPosition{Symbol: "BTC/USDT", Side: "long", MarketType: "futures"},
		decimal.NewFromInt(49000),
		decimal.NewFromInt(52000),
	)
	require.NoError(t, err)
	assert.Equal(t, 2, modifyCalls, "should modify both stale plans")
	assert.Equal(t, 0, cancelCalls, "should NOT cancel when modify succeeds")
	assert.Equal(t, 0, placeCalls, "should NOT place new plans when modify succeeds")
}

// PR-4: when no existing plans exist, SyncPositionProtection falls
// through to place new plans. The cancel-plan-order call is a no-op
// (no plans to cancel) so cancelCalls=0 is correct. The place path
// runs to create the initial protection.
func TestBitgetOrderExecutor_SyncPositionProtection_FallsBackToPlaceWhenNoPlans(t *testing.T) {
	var cancelCalls, placeCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/mix/market/contracts":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":[{
				"sizeMultiplier":"1","minTradeNum":"1","volumePlace":"0","pricePlace":"2"
			}]}`))
		case "/api/v2/mix/order/orders-plan-pending":
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		case "/api/v2/mix/order/cancel-plan-order":
			cancelCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		case "/api/v2/mix/order/place-pos-tpsl":
			placeCalls++
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		}
	}))
	defer server.Close()

	executor := NewBitgetOrderExecutor("k", "s", "p")
	executor.baseURL = server.URL

	err := executor.SyncPositionProtection(
		context.Background(),
		"bitget",
		ManagedOpenPosition{Symbol: "BTC/USDT", Side: "long", MarketType: "futures"},
		decimal.NewFromInt(49000),
		decimal.NewFromInt(52000),
	)
	require.NoError(t, err)
	assert.Equal(t, 0, cancelCalls, "cancel is a no-op when no plans exist")
	assert.Equal(t, 1, placeCalls, "should place new plans when no existing ones")
}

// PR-4 (tertiary root cause): verifyFuturesTPSLActive now requires
// BOTH a TP plan AND an SL plan to report "verified". A position
// with only an SL plan must produce the "Partial TP/SL coverage" log
// instead of the success log.
func TestBitgetOrderExecutor_VerifyFuturesTPSLActive_RequiresBothTPAndSL(t *testing.T) {
	tests := []struct {
		name        string
		plans       string
		shouldCover bool
	}{
		{
			name: "TP only — partial coverage",
			plans: `{"code":"00000","msg":"ok","data":{"entrustedList":[
				{"orderId":"tp-1","planType":"pos_profit","holdSide":"long","triggerPrice":"50000"}
			]}}`,
			shouldCover: false,
		},
		{
			name: "SL only — partial coverage",
			plans: `{"code":"00000","msg":"ok","data":{"entrustedList":[
				{"orderId":"sl-1","planType":"pos_loss","holdSide":"long","triggerPrice":"49000"}
			]}}`,
			shouldCover: false,
		},
		{
			name: "Both TP and SL — full coverage",
			plans: `{"code":"00000","msg":"ok","data":{"entrustedList":[
				{"orderId":"tp-1","planType":"pos_profit","holdSide":"long","triggerPrice":"50000"},
				{"orderId":"sl-1","planType":"pos_loss","holdSide":"long","triggerPrice":"49000"}
			]}}`,
			shouldCover: true,
		},
		{
			name: "profit_loss plan covers both",
			plans: `{"code":"00000","msg":"ok","data":{"entrustedList":[
				{"orderId":"tpsl-1","planType":"profit_loss","holdSide":"long","triggerPrice":"50000"}
			]}}`,
			shouldCover: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// No planType filter — the helper now omits it.
				assert.NotContains(t, r.URL.RawQuery, "planType=")
				_, _ = w.Write([]byte(tt.plans))
			}))
			defer server.Close()

			executor := NewBitgetOrderExecutor("k", "s", "p")
			executor.baseURL = server.URL

			// We don't assert on stdout (captured by other tests);
			// the function returns void. The contract is: the
			// helper now requires BOTH TP and SL plans before
			// logging the "✅ verified" success line. This
			// test guards the underlying listPositionTPSLPlans
			// call and the hasTP/hasSL decomposition, not the
			// log line itself.
			plans, err := executor.listPositionTPSLPlans(context.Background(), "BTCUSDT", "long")
			require.NoError(t, err)
			hasTP, hasSL := false, false
			for _, p := range plans {
				planType := strings.ToLower(strings.TrimSpace(mapStringAny(p, "planType", "plan_type")))
				// Same decomposition as production: a "profit_loss" plan
				// covers BOTH legs (combined TP+SL placed via
				// place-pos-tpsl), so it sets hasTP AND hasSL.
				isTP := strings.Contains(planType, "profit") || strings.Contains(planType, "surplus")
				isSL := strings.Contains(planType, "loss")
				if isTP {
					hasTP = true
				}
				if isSL {
					hasSL = true
				}
			}
			covered := hasTP && hasSL
			assert.Equal(t, tt.shouldCover, covered,
				"hasTP=%t hasSL=%t shouldCover=%t", hasTP, hasSL, tt.shouldCover)
		})
	}
}
