package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/shopspring/decimal"
)

type SmartOrderExecutorConfig struct {
	BaseExecutor          ScalpingOrderExecutor
	MaxRetries            int
	InitialDelay          time.Duration
	MaxDelay              time.Duration
	BackoffFactor         float64
	MaxSlippagePercent    float64
	DefaultTimeout        time.Duration
	MinPartialFillPercent float64
	EnableFOK             bool
	EnableIOC             bool
}

func DefaultSmartOrderExecutorConfig() SmartOrderExecutorConfig {
	return SmartOrderExecutorConfig{
		MaxRetries:            4,
		InitialDelay:          1 * time.Second,
		MaxDelay:              8 * time.Second,
		BackoffFactor:         2.0,
		MaxSlippagePercent:    0.5,
		DefaultTimeout:        30 * time.Second,
		MinPartialFillPercent: 50.0,
		EnableFOK:             true,
		EnableIOC:             true,
	}
}

type SmartOrderExecutor struct {
	config SmartOrderExecutorConfig
}

func NewSmartOrderExecutor(cfg SmartOrderExecutorConfig) *SmartOrderExecutor {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 4
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = 1 * time.Second
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 8 * time.Second
	}
	if cfg.BackoffFactor <= 0 {
		cfg.BackoffFactor = 2.0
	}
	if cfg.MaxSlippagePercent <= 0 {
		cfg.MaxSlippagePercent = 0.5
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.MinPartialFillPercent <= 0 {
		cfg.MinPartialFillPercent = 50.0
	}

	return &SmartOrderExecutor{config: cfg}
}

type OrderType string

const (
	OrderTypeMarket OrderType = "market"
	OrderTypeLimit  OrderType = "limit"
	OrderTypeFOK    OrderType = "fok"
	OrderTypeIOC    OrderType = "ioc"
	OrderTypeGTC    OrderType = "gtc"
)

type SmartOrderRequest struct {
	Exchange    string
	Symbol      string
	Side        string
	OrderType   OrderType
	Amount      decimal.Decimal
	Price       *decimal.Decimal
	MaxSlippage float64
	Timeout     time.Duration
	MaxRetries  int
}

type SmartOrderResult struct {
	OrderID         string          `json:"order_id"`
	Status          string          `json:"status"`
	FilledAmount    decimal.Decimal `json:"filled_amount"`
	RemainingAmount decimal.Decimal `json:"remaining_amount"`
	FillPrice       decimal.Decimal `json:"fill_price"`
	SlippagePercent float64         `json:"slippage_percent"`
	Attempts        int             `json:"attempts"`
	ExecutionTime   time.Duration   `json:"execution_time"`
	Error           string          `json:"error,omitempty"`
}

func (e *SmartOrderExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	req := SmartOrderRequest{
		Exchange:  exchange,
		Symbol:    symbol,
		Side:      side,
		OrderType: OrderType(orderType),
		Amount:    amount,
		Price:     price,
	}

	result, err := e.PlaceOrderSmart(ctx, req)
	if err != nil {
		return "", err
	}

	if result.Error != "" && result.Status != "filled" && result.Status != "placed" {
		return "", errors.New(result.Error)
	}

	return result.OrderID, nil
}

func (e *SmartOrderExecutor) PlaceOrderSmart(ctx context.Context, req SmartOrderRequest) (*SmartOrderResult, error) {
	if req.Exchange == "" {
		return nil, errors.New("exchange is required")
	}
	if req.Symbol == "" {
		return nil, errors.New("symbol is required")
	}
	if req.Side == "" {
		return nil, errors.New("side is required")
	}
	if req.Amount.LessThanOrEqual(decimal.Zero) {
		return nil, errors.New("amount must be greater than zero")
	}

	if req.MaxSlippage <= 0 {
		req.MaxSlippage = e.config.MaxSlippagePercent
	}
	if req.Timeout <= 0 {
		req.Timeout = e.config.DefaultTimeout
	}
	if req.MaxRetries <= 0 {
		req.MaxRetries = e.config.MaxRetries
	}

	startTime := time.Now()

	if req.OrderType == OrderTypeFOK {
		return e.executeFOK(ctx, req, startTime)
	}

	if req.OrderType == OrderTypeIOC {
		return e.executeIOC(ctx, req, startTime)
	}

	return e.executeWithRetry(ctx, req, req.MaxRetries, startTime)
}

func (e *SmartOrderExecutor) executeFOK(ctx context.Context, req SmartOrderRequest, startTime time.Time) (*SmartOrderResult, error) {
	log.Printf("[SMART-EXEC] Executing FOK order: %s %s %s", req.Side, req.Amount.String(), req.Symbol)

	orderID, err := e.config.BaseExecutor.PlaceOrder(ctx, req.Exchange, req.Symbol, req.Side, "limit", req.Amount, req.Price)
	if err != nil {
		return &SmartOrderResult{
			Status:        "cancelled",
			ExecutionTime: time.Since(startTime),
			Error:         err.Error(),
		}, err
	}

	filled, fillPrice, err := e.waitForFill(ctx, req, orderID, req.Timeout)
	if err != nil {
		_ = e.config.BaseExecutor.CancelOrder(ctx, req.Exchange, orderID)
		return &SmartOrderResult{
			OrderID:       orderID,
			Status:        "cancelled",
			ExecutionTime: time.Since(startTime),
			Error:         err.Error(),
		}, err
	}

	if filled.GreaterThanOrEqual(req.Amount) {
		return &SmartOrderResult{
			OrderID:         orderID,
			Status:          "filled",
			FilledAmount:    filled,
			RemainingAmount: decimal.Zero,
			FillPrice:       fillPrice,
			SlippagePercent: e.calcSlippage(req, fillPrice),
			Attempts:        1,
			ExecutionTime:   time.Since(startTime),
		}, nil
	}

	_ = e.config.BaseExecutor.CancelOrder(ctx, req.Exchange, orderID)
	return &SmartOrderResult{
		OrderID:         orderID,
		Status:          "cancelled",
		FilledAmount:    filled,
		RemainingAmount: req.Amount.Sub(filled),
		FillPrice:       fillPrice,
		SlippagePercent: e.calcSlippage(req, fillPrice),
		Attempts:        1,
		ExecutionTime:   time.Since(startTime),
		Error:           "FOK order not fully filled",
	}, errors.New("FOK order not fully filled")
}

func (e *SmartOrderExecutor) executeIOC(ctx context.Context, req SmartOrderRequest, startTime time.Time) (*SmartOrderResult, error) {
	log.Printf("[SMART-EXEC] Executing IOC order: %s %s %s", req.Side, req.Amount.String(), req.Symbol)

	orderID, err := e.config.BaseExecutor.PlaceOrder(ctx, req.Exchange, req.Symbol, req.Side, "limit", req.Amount, req.Price)
	if err != nil {
		return &SmartOrderResult{
			Status:        "cancelled",
			ExecutionTime: time.Since(startTime),
			Error:         err.Error(),
		}, err
	}

	iocTimeout := 5 * time.Second
	if req.Timeout < iocTimeout {
		iocTimeout = req.Timeout
	}

	filled, fillPrice, err := e.waitForFill(ctx, req, orderID, iocTimeout)
	if err != nil {
		_ = e.config.BaseExecutor.CancelOrder(ctx, req.Exchange, orderID)
		return &SmartOrderResult{
			OrderID:       orderID,
			Status:        "cancelled",
			ExecutionTime: time.Since(startTime),
			Error:         err.Error(),
		}, err
	}

	_ = e.config.BaseExecutor.CancelOrder(ctx, req.Exchange, orderID)

	fillPercent := filled.Div(req.Amount).Mul(decimal.NewFromInt(100)).InexactFloat64()
	if fillPercent < e.config.MinPartialFillPercent {
		return &SmartOrderResult{
			OrderID:         orderID,
			Status:          "cancelled",
			FilledAmount:    filled,
			RemainingAmount: req.Amount.Sub(filled),
			FillPrice:       fillPrice,
			SlippagePercent: e.calcSlippage(req, fillPrice),
			Attempts:        1,
			ExecutionTime:   time.Since(startTime),
			Error:           fmt.Sprintf("partial fill %.1f%% below minimum %.1f%%", fillPercent, e.config.MinPartialFillPercent),
		}, errors.New("IOC partial fill below minimum threshold")
	}

	return &SmartOrderResult{
		OrderID:         orderID,
		Status:          "partial",
		FilledAmount:    filled,
		RemainingAmount: req.Amount.Sub(filled),
		FillPrice:       fillPrice,
		SlippagePercent: e.calcSlippage(req, fillPrice),
		Attempts:        1,
		ExecutionTime:   time.Since(startTime),
	}, nil
}

func (e *SmartOrderExecutor) executeWithRetry(ctx context.Context, req SmartOrderRequest, maxRetries int, startTime time.Time) (*SmartOrderResult, error) {
	var lastOrderID string
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("[SMART-EXEC] Attempt %d/%d: %s %s %s", attempt, maxRetries, req.Side, req.Amount.String(), req.Symbol)

		orderType := string(req.OrderType)
		if orderType == "" {
			orderType = "market"
		}

		orderID, err := e.config.BaseExecutor.PlaceOrder(ctx, req.Exchange, req.Symbol, req.Side, orderType, req.Amount, req.Price)
		if err != nil {
			lastErr = err
			lastOrderID = orderID
			log.Printf("[SMART-EXEC] Attempt %d failed: %v", attempt, err)

			if attempt < maxRetries && e.isRetryableError(err) {
				delay := e.calcBackoff(attempt)
				log.Printf("[SMART-EXEC] Retrying in %v (attempt %d/%d)", delay, attempt+1, maxRetries)

				select {
				case <-ctx.Done():
					return &SmartOrderResult{
						OrderID:       lastOrderID,
						Status:        "expired",
						ExecutionTime: time.Since(startTime),
						Attempts:      attempt,
						Error:         ctx.Err().Error(),
					}, ctx.Err()
				case <-time.After(delay):
					continue
				}
			}
			break
		}

		lastOrderID = orderID

		if req.OrderType == OrderTypeMarket {
			filled, fillPrice, fillErr := e.waitForFill(ctx, req, orderID, req.Timeout)
			if fillErr != nil {
				if filled.GreaterThan(decimal.Zero) {
					slippage := e.calcSlippage(req, fillPrice)
					if slippage > req.MaxSlippage {
						_ = e.config.BaseExecutor.CancelOrder(ctx, req.Exchange, orderID)
						lastErr = fmt.Errorf("slippage %.2f%% exceeds maximum %.2f%%", slippage, req.MaxSlippage)
						log.Printf("[SMART-EXEC] Slippage protection triggered: %.2f%% > %.2f%%", slippage, req.MaxSlippage)

						if attempt < maxRetries {
							continue
						}
						break
					}
				}

				lastErr = fillErr
				if attempt < maxRetries && e.isRetryableError(fillErr) {
					delay := e.calcBackoff(attempt)
					select {
					case <-ctx.Done():
						return &SmartOrderResult{
							OrderID:       orderID,
							Status:        "expired",
							ExecutionTime: time.Since(startTime),
							Attempts:      attempt,
							Error:         ctx.Err().Error(),
						}, ctx.Err()
					case <-time.After(delay):
						continue
					}
				}
				break
			}

			fillPercent := filled.Div(req.Amount).Mul(decimal.NewFromInt(100)).InexactFloat64()
			if fillPercent < 100 && fillPercent < e.config.MinPartialFillPercent {
				_ = e.config.BaseExecutor.CancelOrder(ctx, req.Exchange, orderID)
				lastErr = fmt.Errorf("partial fill %.1f%% below minimum %.1f%%", fillPercent, e.config.MinPartialFillPercent)
				log.Printf("[SMART-EXEC] Partial fill below minimum: %.1f%% < %.1f%%", fillPercent, e.config.MinPartialFillPercent)

				if attempt < maxRetries {
					req.Amount = req.Amount.Sub(filled)
					delay := e.calcBackoff(attempt)
					select {
					case <-ctx.Done():
						return &SmartOrderResult{
							OrderID:         orderID,
							Status:          "partial",
							FilledAmount:    filled,
							RemainingAmount: req.Amount,
							FillPrice:       fillPrice,
							SlippagePercent: e.calcSlippage(req, fillPrice),
							Attempts:        attempt,
							ExecutionTime:   time.Since(startTime),
							Error:           lastErr.Error(),
						}, lastErr
					case <-time.After(delay):
						continue
					}
				}
				break
			}

			slippage := e.calcSlippage(req, fillPrice)
			return &SmartOrderResult{
				OrderID:         orderID,
				Status:          "filled",
				FilledAmount:    filled,
				RemainingAmount: req.Amount.Sub(filled),
				FillPrice:       fillPrice,
				SlippagePercent: slippage,
				Attempts:        attempt,
				ExecutionTime:   time.Since(startTime),
			}, nil
		}

		return &SmartOrderResult{
			OrderID:       orderID,
			Status:        "placed",
			ExecutionTime: time.Since(startTime),
			Attempts:      attempt,
		}, nil
	}

	return &SmartOrderResult{
		OrderID:       lastOrderID,
		Status:        "failed",
		ExecutionTime: time.Since(startTime),
		Attempts:      maxRetries,
		Error:         lastErr.Error(),
	}, lastErr
}

func (e *SmartOrderExecutor) waitForFill(ctx context.Context, req SmartOrderRequest, orderID string, timeout time.Duration) (decimal.Decimal, decimal.Decimal, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return decimal.Zero, decimal.Zero, fmt.Errorf("order fill timeout after %v", timeout)
		case <-ticker.C:
			orders, err := e.config.BaseExecutor.GetOpenOrders(ctx, req.Exchange, req.Symbol)
			if err != nil {
				continue
			}

			var filled decimal.Decimal
			for _, order := range orders {
				if order["id"] == orderID {
					if filledAmt, ok := order["filled"].(float64); ok {
						filled = decimal.NewFromFloat(filledAmt)
					}
					break
				}
			}

			if !filled.IsZero() && filled.LessThan(req.Amount) {
				continue
			}

			if filled.IsZero() {
				closed, err := e.config.BaseExecutor.GetClosedOrders(ctx, req.Exchange, req.Symbol, 10)
				if err == nil && len(closed) > 0 {
					for _, order := range closed {
						if order["id"] == orderID {
							if filledAmt, ok := order["filled"].(float64); ok {
								filled = decimal.NewFromFloat(filledAmt)
							}
							if avgPrice, ok := order["average"].(float64); ok && avgPrice > 0 {
								return filled, decimal.NewFromFloat(avgPrice), nil
							}
							if price, ok := order["price"].(float64); ok && price > 0 {
								return filled, decimal.NewFromFloat(price), nil
							}
							return filled, decimal.Zero, nil
						}
					}
				}
				return decimal.Zero, decimal.Zero, errors.New("order not filled or cancelled")
			}
		}
	}
}

func (e *SmartOrderExecutor) calcBackoff(attempt int) time.Duration {
	delay := float64(e.config.InitialDelay) * math.Pow(e.config.BackoffFactor, float64(attempt-1))
	if delay > float64(e.config.MaxDelay) {
		delay = float64(e.config.MaxDelay)
	}
	return time.Duration(delay)
}

func (e *SmartOrderExecutor) calcSlippage(req SmartOrderRequest, fillPrice decimal.Decimal) float64 {
	if req.Price == nil || req.Price.IsZero() {
		return 0
	}

	price := fillPrice.Sub(*req.Price).Abs()
	return price.Div(*req.Price).Mul(decimal.NewFromInt(100)).InexactFloat64()
}

func (e *SmartOrderExecutor) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()

	retryableKeywords := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"service unavailable",
		"too many requests",
		"rate limit",
		"429",
		"503",
		"500",
	}

	for _, kw := range retryableKeywords {
		if len(errStr) > 0 && len(kw) > 0 {
			for i := 0; i <= len(errStr)-len(kw); i++ {
				if errStr[i:i+len(kw)] == kw {
					return true
				}
			}
		}
	}

	return false
}

func (e *SmartOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	return e.config.BaseExecutor.CancelOrder(ctx, exchange, orderID)
}

func (e *SmartOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	return e.config.BaseExecutor.GetOpenOrders(ctx, exchange, symbol)
}

func (e *SmartOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	return e.config.BaseExecutor.GetClosedOrders(ctx, exchange, symbol, limit)
}

var _ ScalpingOrderExecutor = (*SmartOrderExecutor)(nil)
