package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"
)

const (
	CLOBBaseURL        = "https://clob.polymarket.com"
	DefaultCLOBTimeout = 30 * time.Second
)

type OrderType string

const (
	OrderTypeFOK OrderType = "FOK"
	OrderTypeGTC OrderType = "GTC"
	OrderTypeGTD OrderType = "GTD"
	OrderTypeFAK OrderType = "FAK"
)

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type Order struct {
	Salt        string `json:"salt"`
	Maker       string `json:"maker"`
	Signer      string `json:"signer"`
	Taker       string `json:"taker"`
	TokenID     string `json:"tokenId"`
	MakerAmount string `json:"makerAmount"`
	TakerAmount string `json:"takerAmount"`
	Expiration  string `json:"expiration"`
}

type PostOrderRequest struct {
	Order     Order     `json:"order"`
	OrderType OrderType `json:"orderType"`
	Owner     string    `json:"owner"`
	PostOnly  bool      `json:"postOnly,omitempty"`
}

type OrderResponse struct {
	OrderID       string    `json:"orderID"`
	Order         Order     `json:"order"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	Price         float64   `json:"price"`
	Size          float64   `json:"size"`
	RemainingSize float64   `json:"remainingSize"`
	FilledSize    float64   `json:"filledSize"`
}

type OrderBookEntry struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type OrderBook struct {
	Bids []OrderBookEntry `json:"bids"`
	Asks []OrderBookEntry `json:"asks"`
}

type CLOBClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
}

type CLOBOption func(*CLOBClient)

func WithCLOBBaseURL(baseURL string) CLOBOption {
	return func(c *CLOBClient) {
		c.baseURL = baseURL
	}
}

func WithCLOBTimeout(timeout time.Duration) CLOBOption {
	return func(c *CLOBClient) {
		c.httpClient.Timeout = timeout
	}
}

func WithCLOBCredentials(apiKey, apiSecret string) CLOBOption {
	return func(c *CLOBClient) {
		c.apiKey = apiKey
		c.apiSecret = apiSecret
	}
}

func NewCLOBClient(opts ...CLOBOption) *CLOBClient {
	c := &CLOBClient{
		httpClient: &http.Client{Timeout: DefaultCLOBTimeout},
		baseURL:    CLOBBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *CLOBClient) CreateOrder(ctx context.Context, req PostOrderRequest) (*OrderResponse, error) {
	url := c.baseURL + "/order"

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal order request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var orderResp OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&orderResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &orderResp, nil
}

func (c *CLOBClient) CreateOrderBatch(ctx context.Context, orders []PostOrderRequest) ([]OrderResponse, error) {
	url := c.baseURL + "/orders"

	type BatchRequest struct {
		Orders []PostOrderRequest `json:"orders"`
	}

	batchReq := BatchRequest{Orders: orders}
	payload, err := json.Marshal(batchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal batch request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var orderResps []OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&orderResps); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return orderResps, nil
}

func (c *CLOBClient) GetOrderBook(ctx context.Context, tokenID string) (*OrderBook, error) {
	url := c.baseURL + "/orderbook/" + tokenID

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var orderBook OrderBook
	if err := json.NewDecoder(resp.Body).Decode(&orderBook); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &orderBook, nil
}

func (c *CLOBClient) GetBestPrice(ctx context.Context, tokenID string, side Side) (float64, error) {
	orderBook, err := c.GetOrderBook(ctx, tokenID)
	if err != nil {
		return 0, err
	}

	switch side {
	case SideBuy:
		if len(orderBook.Asks) == 0 {
			return 0, fmt.Errorf("no asks available")
		}
		return orderBook.Asks[0].Price, nil
	case SideSell:
		if len(orderBook.Bids) == 0 {
			return 0, fmt.Errorf("no bids available")
		}
		return orderBook.Bids[0].Price, nil
	default:
		return 0, fmt.Errorf("invalid side: %s", side)
	}
}

func (c *CLOBClient) CancelOrder(ctx context.Context, orderID string) error {
	url := c.baseURL + "/order"

	type CancelRequest struct {
		OrderID string `json:"orderID"`
	}

	cancelReq := CancelRequest{OrderID: orderID}
	payload, err := json.Marshal(cancelReq)
	if err != nil {
		return fmt.Errorf("failed to marshal cancel request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	return nil
}

func (c *CLOBClient) GetOpenOrders(ctx context.Context) ([]OrderResponse, error) {
	url := c.baseURL + "/orders"

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var orders []OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&orders); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return orders, nil
}

func (c *CLOBClient) GetOrder(ctx context.Context, orderID string) (*OrderResponse, error) {
	url := c.baseURL + "/order/" + orderID

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var order OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &order, nil
}

func CalculateMakerAmount(size, price float64) string {
	makerAmount := size * price
	return fmt.Sprintf("%d", int(makerAmount*1000))
}

func CalculateTakerAmount(size float64) string {
	return fmt.Sprintf("%d", int(size*1000))
}

func PriceToFloat(price *big.Int) float64 {
	f, _ := new(big.Rat).SetString(price.String())
	f.Mul(f, big.NewRat(1, 1000))
	floatVal, _ := f.Float64()
	return floatVal
}

func FloatToPrice(price float64) *big.Int {
	return new(big.Int).Mul(big.NewInt(int64(price*1000)), big.NewInt(1e15))
}

func BuildOrder(
	salt int64,
	maker, signer, taker, tokenID string,
	makerAmount, takerAmount string,
	expiration int64,
) Order {
	return Order{
		Salt:        fmt.Sprintf("%d", salt),
		Maker:       maker,
		Signer:      signer,
		Taker:       taker,
		TokenID:     tokenID,
		MakerAmount: makerAmount,
		TakerAmount: takerAmount,
		Expiration:  fmt.Sprintf("%d", expiration),
	}
}

func CreatePostOrderRequest(
	order Order,
	orderType OrderType,
	owner string,
	postOnly bool,
) PostOrderRequest {
	return PostOrderRequest{
		Order:     order,
		OrderType: orderType,
		Owner:     owner,
		PostOnly:  postOnly,
	}
}

type PlaceMarketOrderRequest struct {
	TokenID  string  `json:"tokenId"`
	Side     Side    `json:"side"`
	Size     float64 `json:"size"`
	MaxPrice float64 `json:"maxPrice,omitempty"`
}

type PlaceLimitOrderRequest struct {
	TokenID    string    `json:"tokenId"`
	Side       Side      `json:"side"`
	Size       float64   `json:"size"`
	Price      float64   `json:"price"`
	OrderType  OrderType `json:"orderType"`
	PostOnly   bool      `json:"postOnly,omitempty"`
	Expiration int64     `json:"expiration,omitempty"`
}

func (c *CLOBClient) PlaceMarketOrder(ctx context.Context, req PlaceMarketOrderRequest) (*OrderResponse, error) {
	bestPrice, err := c.GetBestPrice(ctx, req.TokenID, req.Side)
	if err != nil {
		return nil, fmt.Errorf("failed to get best price: %w", err)
	}

	if req.MaxPrice > 0 && bestPrice > req.MaxPrice {
		return nil, fmt.Errorf("best price %.4f exceeds max price %.4f", bestPrice, req.MaxPrice)
	}

	return c.PlaceLimitOrder(ctx, PlaceLimitOrderRequest{
		TokenID:   req.TokenID,
		Side:      req.Side,
		Size:      req.Size,
		Price:     bestPrice,
		OrderType: OrderTypeFOK,
	})
}

func (c *CLOBClient) PlaceLimitOrder(ctx context.Context, req PlaceLimitOrderRequest) (*OrderResponse, error) {
	salt := time.Now().UnixNano()
	expiration := req.Expiration
	if expiration == 0 {
		expiration = time.Now().Add(24 * time.Hour).Unix()
	}

	makerAmount := CalculateMakerAmount(req.Size, req.Price)
	takerAmount := CalculateTakerAmount(req.Size)

	order := BuildOrder(
		salt,
		c.apiKey,
		c.apiKey,
		"0x0000000000000000000000000000000000000000",
		req.TokenID,
		makerAmount,
		takerAmount,
		expiration,
	)

	postOrder := CreatePostOrderRequest(order, req.OrderType, c.apiKey, req.PostOnly)

	return c.CreateOrder(ctx, postOrder)
}
