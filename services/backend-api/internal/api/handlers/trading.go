package handlers

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type TradingHandler struct {
	mu        sync.RWMutex
	sequence  int64
	orders    map[string]OrderRecord
	positions map[string]PositionRecord
}

type PlaceOrderRequest struct {
	Exchange string          `json:"exchange" binding:"required"`
	Symbol   string          `json:"symbol" binding:"required"`
	Side     string          `json:"side" binding:"required"`
	Type     string          `json:"type"`
	Amount   decimal.Decimal `json:"amount" binding:"required"`
	Price    decimal.Decimal `json:"price"`
}

type CancelOrderRequest struct {
	OrderID string `json:"order_id" binding:"required"`
}

type LiquidateRequest struct {
	PositionID string `json:"position_id"`
	Symbol     string `json:"symbol"`
}

type OrderRecord struct {
	OrderID    string          `json:"order_id"`
	PositionID string          `json:"position_id"`
	Exchange   string          `json:"exchange"`
	Symbol     string          `json:"symbol"`
	Side       string          `json:"side"`
	Type       string          `json:"type"`
	Amount     decimal.Decimal `json:"amount"`
	Price      decimal.Decimal `json:"price"`
	Status     string          `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type PositionRecord struct {
	PositionID string          `json:"position_id"`
	OrderID    string          `json:"order_id"`
	Exchange   string          `json:"exchange"`
	Symbol     string          `json:"symbol"`
	Side       string          `json:"side"`
	Size       decimal.Decimal `json:"size"`
	EntryPrice decimal.Decimal `json:"entry_price"`
	Status     string          `json:"status"`
	OpenedAt   time.Time       `json:"opened_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func NewTradingHandler() *TradingHandler {
	return &TradingHandler{
		orders:    make(map[string]OrderRecord),
		positions: make(map[string]PositionRecord),
	}
}

func (h *TradingHandler) PlaceOrder(c *gin.Context) {
	var req PlaceOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "invalid request payload",
		})
		return
	}

	if req.Amount.LessThanOrEqual(decimal.Zero) {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "amount must be greater than zero",
		})
		return
	}

	side := strings.ToUpper(strings.TrimSpace(req.Side))
	if side != "BUY" && side != "SELL" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "side must be BUY or SELL",
		})
		return
	}

	orderType := strings.ToUpper(strings.TrimSpace(req.Type))
	if orderType == "" {
		orderType = "MARKET"
	}
	if orderType != "MARKET" && orderType != "LIMIT" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "type must be MARKET or LIMIT",
		})
		return
	}

	if orderType == "LIMIT" && req.Price.LessThanOrEqual(decimal.Zero) {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "price must be greater than zero for LIMIT orders",
		})
		return
	}

	now := time.Now().UTC()

	h.mu.Lock()
	defer h.mu.Unlock()

	h.sequence++
	orderID := "ord-" + now.Format("20060102150405") + "-" + decimal.NewFromInt(h.sequence).String()
	positionID := "pos-" + now.Format("20060102150405") + "-" + decimal.NewFromInt(h.sequence).String()

	order := OrderRecord{
		OrderID:    orderID,
		PositionID: positionID,
		Exchange:   req.Exchange,
		Symbol:     req.Symbol,
		Side:       side,
		Type:       orderType,
		Amount:     req.Amount,
		Price:      req.Price,
		Status:     "OPEN",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	position := PositionRecord{
		PositionID: positionID,
		OrderID:    orderID,
		Exchange:   req.Exchange,
		Symbol:     req.Symbol,
		Side:       side,
		Size:       req.Amount,
		EntryPrice: req.Price,
		Status:     "OPEN",
		OpenedAt:   now,
		UpdatedAt:  now,
	}

	h.orders[orderID] = order
	h.positions[positionID] = position

	c.JSON(http.StatusCreated, gin.H{
		"status": "success",
		"data": gin.H{
			"order":    order,
			"position": position,
		},
	})
}

func (h *TradingHandler) CancelOrder(c *gin.Context) {
	var req CancelOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "invalid request payload",
		})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	order, ok := h.orders[req.OrderID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "order not found",
		})
		return
	}

	if order.Status != "OPEN" {
		c.JSON(http.StatusConflict, gin.H{
			"status": "error",
			"error":  "order is not open",
		})
		return
	}

	now := time.Now().UTC()
	order.Status = "CANCELED"
	order.UpdatedAt = now
	h.orders[req.OrderID] = order

	if position, exists := h.positions[order.PositionID]; exists && position.Status == "OPEN" {
		position.Status = "CLOSED"
		position.UpdatedAt = now
		h.positions[position.PositionID] = position
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   order,
	})
}

func (h *TradingHandler) Liquidate(c *gin.Context) {
	var req LiquidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "invalid request payload",
		})
		return
	}

	positionID := strings.TrimSpace(req.PositionID)
	symbol := strings.TrimSpace(req.Symbol)

	if positionID == "" && symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "position_id or symbol is required",
		})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	for id, position := range h.positions {
		if position.Status != "OPEN" {
			continue
		}

		if positionID != "" && position.PositionID != positionID {
			continue
		}

		if symbol != "" && !strings.EqualFold(position.Symbol, symbol) {
			continue
		}

		position.Status = "LIQUIDATED"
		position.UpdatedAt = now
		h.positions[id] = position

		if order, exists := h.orders[position.OrderID]; exists && order.Status == "OPEN" {
			order.Status = "CLOSED"
			order.UpdatedAt = now
			h.orders[order.OrderID] = order
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   position,
		})
		return
	}

	c.JSON(http.StatusNotFound, gin.H{
		"status": "error",
		"error":  "open position not found",
	})
}

func (h *TradingHandler) LiquidateAll(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	liquidated := make([]PositionRecord, 0)

	for id, position := range h.positions {
		if position.Status != "OPEN" {
			continue
		}

		position.Status = "LIQUIDATED"
		position.UpdatedAt = now
		h.positions[id] = position
		liquidated = append(liquidated, position)

		if order, exists := h.orders[position.OrderID]; exists && order.Status == "OPEN" {
			order.Status = "CLOSED"
			order.UpdatedAt = now
			h.orders[order.OrderID] = order
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data": gin.H{
			"count":     len(liquidated),
			"positions": liquidated,
		},
	})
}

func (h *TradingHandler) ListPositions(c *gin.Context) {
	statusFilter := strings.ToUpper(strings.TrimSpace(c.Query("status")))

	h.mu.RLock()
	defer h.mu.RUnlock()

	positions := make([]PositionRecord, 0, len(h.positions))
	for _, position := range h.positions {
		if statusFilter != "" && position.Status != statusFilter {
			continue
		}
		positions = append(positions, position)
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data": gin.H{
			"count":     len(positions),
			"positions": positions,
		},
	})
}

func (h *TradingHandler) GetPosition(c *gin.Context) {
	positionID := strings.TrimSpace(c.Param("position_id"))
	if positionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "position_id is required",
		})
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	position, ok := h.positions[positionID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "position not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   position,
	})
}
