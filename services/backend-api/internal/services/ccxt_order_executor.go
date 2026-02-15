package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/shopspring/decimal"
)

type CCXTOrderExecutorConfig struct {
	ServiceURL string
	APIKey     string
	Timeout    time.Duration
}

func DefaultCCXTOrderExecutorConfig() CCXTOrderExecutorConfig {
	return CCXTOrderExecutorConfig{
		ServiceURL: "http://localhost:3001",
		Timeout:    30 * time.Second,
	}
}

type CCXTOrderExecutor struct {
	serviceURL string
	apiKey     string
	httpClient *http.Client
}

func NewCCXTOrderExecutor(cfg CCXTOrderExecutorConfig) *CCXTOrderExecutor {
	return &CCXTOrderExecutor{
		serviceURL: cfg.ServiceURL,
		apiKey:     cfg.APIKey,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (e *CCXTOrderExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	reqBody := map[string]interface{}{
		"exchange": exchange,
		"symbol":   symbol,
		"side":     side,
		"type":     orderType,
		"amount":   amount.InexactFloat64(),
	}

	if price != nil {
		reqBody["price"] = price.InexactFloat64()
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.serviceURL+"/api/order", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", fmt.Sprintf("%s/api/order/%s/%s", e.serviceURL, exchange, orderID), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("order cancellation failed with status: %d", resp.StatusCode)
	}

	return nil
}

func (e *CCXTOrderExecutor) GetOrder(ctx context.Context, exchange, orderID string) (map[string]interface{}, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/order/%s/%s", e.serviceURL, exchange, orderID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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

	var requestURL string
	if symbol != "" {
		requestURL = baseURL + "?symbol=" + url.QueryEscape(symbol)
	} else {
		requestURL = baseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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

	httpReq, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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
	httpReq, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/order/%s/%s/trades", e.serviceURL, exchange, orderID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if e.apiKey != "" {
		httpReq.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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
