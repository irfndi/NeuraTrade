package services

import (
	"context"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/polymarket"
	"github.com/irfndi/neuratrade/pkg/interfaces"
	"github.com/shopspring/decimal"
)

type OrderExecutionConfig struct {
	BaseURL    string
	APIKey     string
	APISecret  string
	WalletAddr string
	Timeout    time.Duration
}

func DefaultOrderExecutionConfig() OrderExecutionConfig {
	return OrderExecutionConfig{
		BaseURL: polymarket.CLOBBaseURL,
		Timeout: polymarket.DefaultCLOBTimeout,
	}
}

type OrderExecutionService struct {
	client *polymarket.CLOBClient
}

func NewOrderExecutionService(cfg OrderExecutionConfig) *OrderExecutionService {
	opts := []polymarket.CLOBOption{
		polymarket.WithCLOBTimeout(cfg.Timeout),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, polymarket.WithCLOBBaseURL(cfg.BaseURL))
	}
	if cfg.APIKey != "" && cfg.APISecret != "" {
		opts = append(opts, polymarket.WithCLOBCredentials(cfg.APIKey, cfg.APISecret))
	}
	if cfg.WalletAddr != "" {
		opts = append(opts, polymarket.WithWalletAddress(cfg.WalletAddr))
	}

	return &OrderExecutionService{
		client: polymarket.NewCLOBClient(opts...),
	}
}

func (s *OrderExecutionService) PlaceOrder(ctx context.Context, req interfaces.OrderExecutionRequest) (*interfaces.OrderExecutionResult, error) {
	if req.TokenID == "" {
		return nil, fmt.Errorf("tokenID is required")
	}
	if req.Size.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("size must be greater than zero")
	}

	clobSide := polymarket.Side(req.Side)
	if clobSide != polymarket.SideBuy && clobSide != polymarket.SideSell {
		return nil, fmt.Errorf("invalid side: must be BUY or SELL")
	}

	var resp *polymarket.OrderResponse
	var err error

	switch req.OrderType {
	case interfaces.OrderExecutionMarket:
		clobReq := polymarket.PlaceMarketOrderRequest{
			TokenID:  req.TokenID,
			Side:     clobSide,
			Size:     req.Size,
			MaxPrice: req.MaxPrice,
		}
		resp, err = s.client.PlaceMarketOrder(ctx, clobReq)
	case interfaces.OrderExecutionFOK, interfaces.OrderExecutionGTC, interfaces.OrderExecutionGTD, interfaces.OrderExecutionFAK, interfaces.OrderExecutionLimit:
		clobReq := polymarket.PlaceLimitOrderRequest{
			TokenID:    req.TokenID,
			Side:       clobSide,
			Size:       req.Size,
			Price:      req.Price,
			OrderType:  polymarket.OrderType(req.OrderType),
			PostOnly:   req.PostOnly,
			Expiration: req.Expiration,
		}
		resp, err = s.client.PlaceLimitOrder(ctx, clobReq)
	default:
		return nil, fmt.Errorf("unsupported order type: %s", req.OrderType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to place order: %w", err)
	}

	return s.convertToResult(resp, req.TokenID, req.Side), nil
}

func (s *OrderExecutionService) GetOrder(ctx context.Context, orderID string) (*interfaces.OrderExecutionResult, error) {
	if orderID == "" {
		return nil, fmt.Errorf("orderID is required")
	}

	resp, err := s.client.GetOrder(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	return s.convertToResult(resp, resp.Order.TokenID, ""), nil
}

func (s *OrderExecutionService) CancelOrder(ctx context.Context, orderID string) error {
	if orderID == "" {
		return fmt.Errorf("orderID is required")
	}

	return s.client.CancelOrder(ctx, orderID)
}

func (s *OrderExecutionService) GetOpenOrders(ctx context.Context) ([]*interfaces.OrderExecutionResult, error) {
	orders, err := s.client.GetOpenOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get open orders: %w", err)
	}

	results := make([]*interfaces.OrderExecutionResult, len(orders))
	for i, order := range orders {
		results[i] = s.convertToResult(&order, order.Order.TokenID, "")
	}

	return results, nil
}

func (s *OrderExecutionService) GetOrderBook(ctx context.Context, tokenID string) (*interfaces.OrderBook, error) {
	if tokenID == "" {
		return nil, fmt.Errorf("tokenID is required")
	}

	ob, err := s.client.GetOrderBook(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order book: %w", err)
	}

	bids := make([]interfaces.OrderBookEntry, len(ob.Bids))
	for i, b := range ob.Bids {
		bids[i] = interfaces.OrderBookEntry{
			Price: b.Price,
			Size:  b.Size,
		}
	}

	asks := make([]interfaces.OrderBookEntry, len(ob.Asks))
	for i, a := range ob.Asks {
		asks[i] = interfaces.OrderBookEntry{
			Price: a.Price,
			Size:  a.Size,
		}
	}

	return &interfaces.OrderBook{
		TokenID:   tokenID,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now(),
	}, nil
}

func (s *OrderExecutionService) GetBestPrice(ctx context.Context, tokenID string, side interfaces.OrderSide) (decimal.Decimal, error) {
	if tokenID == "" {
		return decimal.Zero, fmt.Errorf("tokenID is required")
	}

	clobSide := polymarket.Side(side)
	if clobSide != polymarket.SideBuy && clobSide != polymarket.SideSell {
		return decimal.Zero, fmt.Errorf("invalid side: must be BUY or SELL")
	}

	return s.client.GetBestPrice(ctx, tokenID, clobSide)
}

func (s *OrderExecutionService) convertToResult(resp *polymarket.OrderResponse, tokenID string, side interfaces.OrderSide) *interfaces.OrderExecutionResult {
	status := interfaces.OrderStatus(resp.Status)
	if status == "" {
		status = interfaces.OrderStatusOpen
	}

	return &interfaces.OrderExecutionResult{
		OrderID:       resp.OrderID,
		Status:        status,
		Size:          resp.Size,
		FilledSize:    resp.FilledSize,
		RemainingSize: resp.RemainingSize,
		Price:         resp.Price,
		CreatedAt:     resp.CreatedAt,
		TokenID:       tokenID,
		Side:          side,
	}
}
