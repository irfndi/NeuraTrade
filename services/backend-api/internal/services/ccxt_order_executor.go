package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type CCXTOrderExecutorConfig struct {
	ServiceURL string
	APIKey     string
	Timeout    time.Duration
	MaxRetries int
}

func DefaultCCXTOrderExecutorConfig() CCXTOrderExecutorConfig {
	return CCXTOrderExecutorConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30 * time.Second,
		MaxRetries: 3,
	}
}

type CCXTOrderExecutor struct {
	serviceURL string
	apiKey     string
	httpClient *http.Client
	maxRetries int
}

func NewCCXTOrderExecutor(cfg CCXTOrderExecutorConfig) *CCXTOrderExecutor {
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &CCXTOrderExecutor{
		serviceURL: cfg.ServiceURL,
		apiKey:     cfg.APIKey,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		maxRetries: maxRetries,
	}
}

func (e *CCXTOrderExecutor) doWithRetry(ctx context.Context, makeReq func() (*http.Request, error)) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(200*attempt+rand.Intn(100)) * time.Millisecond
			if attempt > 1 {
				backoff = time.Duration(200*(1<<(attempt-1))+rand.Intn(100)) * time.Millisecond
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, reqErr := makeReq()
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err = e.httpClient.Do(req)
		if err == nil && resp.StatusCode < http.StatusInternalServerError {
			return resp, nil
		}
		if err == nil {
			// 5xx response; drain and close body before retry to allow connection reuse.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("request failed after %d retries: %w", e.maxRetries, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil, fmt.Errorf("request failed with status %d after %d retries", resp.StatusCode, e.maxRetries)
}

func (e *CCXTOrderExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	idempotencyKey := uuid.New().String()
	return e.placeOrderWithKey(ctx, exchange, symbol, side, orderType, amount, price, idempotencyKey)
}

func (e *CCXTOrderExecutor) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	clientOrderID := details.ClientOrderID
	if clientOrderID == "" {
		key, err := generateIdempotencyKey("", details.Symbol, details.Side, details.IntentID)
		if err != nil {
			return "", fmt.Errorf("generate client order id: %w", err)
		}
		clientOrderID = key
	}
	return e.placeOrderWithKey(ctx, details.Exchange, details.Symbol, details.Side,
		details.OrderType, details.AmountUSDT, details.EntryPrice, clientOrderID)
}

func (e *CCXTOrderExecutor) placeOrderWithKey(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal, idempotencyKey string) (string, error) {
	reqBody := map[string]interface{}{
		"exchange":      exchange,
		"symbol":        symbol,
		"side":          side,
		"type":          orderType,
		"amount":        amount.InexactFloat64(),
		"clientOrderId": idempotencyKey,
	}

	if price != nil {
		reqBody["price"] = price.InexactFloat64()
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := e.doWithRetry(ctx, func() (*http.Request, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "POST", e.serviceURL+"/api/order", bytes.NewBuffer(jsonBody))
		if reqErr != nil {
			return nil, reqErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Idempotency-Key", idempotencyKey)
		if e.apiKey != "" {
			httpReq.Header.Set("X-API-Key", e.apiKey)
		}
		return httpReq, nil
	})
	if err != nil {
		return "", fmt.Errorf("place order failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		fmt.Printf("[CCXT-ORDER] Duplicate order detected (idempotent retry, HTTP 409): clientOrderId=%s\n", idempotencyKey)
		return idempotencyKey, nil
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("order placement failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Order struct {
			ID string `json:"id"`
		} `json:"order"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Order.ID, nil
}

func (e *CCXTOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	resp, err := e.doWithRetry(ctx, func() (*http.Request, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("%s/api/order/%s/%s", e.serviceURL, exchange, orderID), nil)
		if reqErr != nil {
			return nil, reqErr
		}
		if e.apiKey != "" {
			httpReq.Header.Set("X-API-Key", e.apiKey)
		}
		return httpReq, nil
	})
	if err != nil {
		return fmt.Errorf("cancel order failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("order cancellation failed with status: %d", resp.StatusCode)
	}
	return nil
}

func (e *CCXTOrderExecutor) GetOrder(ctx context.Context, exchange, orderID string) (map[string]interface{}, error) {
	resp, err := e.doWithRetry(ctx, func() (*http.Request, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/order/%s/%s", e.serviceURL, exchange, orderID), nil)
		if reqErr != nil {
			return nil, reqErr
		}
		if e.apiKey != "" {
			httpReq.Header.Set("X-API-Key", e.apiKey)
		}
		return httpReq, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get order failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get order failed with status: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

func (e *CCXTOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	baseURL := fmt.Sprintf("%s/api/orders/%s", e.serviceURL, exchange)
	resp, err := e.doWithRetry(ctx, func() (*http.Request, error) {
		var requestURL string
		if symbol != "" {
			requestURL = baseURL + "?symbol=" + url.QueryEscape(symbol)
		} else {
			requestURL = baseURL
		}
		httpReq, reqErr := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		if e.apiKey != "" {
			httpReq.Header.Set("X-API-Key", e.apiKey)
		}
		return httpReq, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get open orders failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get open orders failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Orders []map[string]interface{} `json:"orders"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Orders, nil
}

func (e *CCXTOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	baseURL := fmt.Sprintf("%s/api/orders/%s/closed", e.serviceURL, exchange)
	resp, err := e.doWithRetry(ctx, func() (*http.Request, error) {
		params := url.Values{}
		if symbol != "" {
			params.Add("symbol", symbol)
		}
		if limit > 0 {
			params.Add("limit", fmt.Sprintf("%d", limit))
		}
		var requestURL string
		if params.Encode() != "" {
			requestURL = baseURL + "?" + params.Encode()
		} else {
			requestURL = baseURL
		}
		httpReq, reqErr := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		if e.apiKey != "" {
			httpReq.Header.Set("X-API-Key", e.apiKey)
		}
		return httpReq, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get closed orders failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get closed orders failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Orders []map[string]interface{} `json:"orders"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Orders, nil
}

func (e *CCXTOrderExecutor) GetOrderTrades(ctx context.Context, exchange, orderID string) ([]map[string]interface{}, error) {
	resp, err := e.doWithRetry(ctx, func() (*http.Request, error) {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/order/%s/%s/trades", e.serviceURL, exchange, orderID), nil)
		if reqErr != nil {
			return nil, reqErr
		}
		if e.apiKey != "" {
			httpReq.Header.Set("X-API-Key", e.apiKey)
		}
		return httpReq, nil
	})
	if err != nil {
		return nil, fmt.Errorf("get order trades failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get order trades failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Trades []map[string]interface{} `json:"trades"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return result.Trades, nil
}

func (e *CCXTOrderExecutor) IsPaperTrading() bool {
	return false
}

var _ ScalpingOrderExecutor = (*CCXTOrderExecutor)(nil)
