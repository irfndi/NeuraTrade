package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

var (
	errTradingOrderNotFound    = errors.New("trading order not found")
	errTradingOrderNotOpen     = errors.New("trading order is not open")
	errTradingPositionNotFound = errors.New("trading position not found")
)

type TradingHandler struct {
	db        DBQuerier
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

func NewTradingHandler(querier ...any) *TradingHandler {
	h := &TradingHandler{
		orders:    make(map[string]OrderRecord),
		positions: make(map[string]PositionRecord),
	}

	if len(querier) == 0 || querier[0] == nil {
		return h
	}

	resolvedQuerier, err := resolveDBQuerier(querier[0])
	if err != nil {
		panic(err)
	}
	h.db = resolvedQuerier

	if err := h.initializeTradingStore(); err != nil {
		panic(err)
	}

	return h
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
	orderID, positionID := h.generateIDs(now)

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

	if h.usesPersistentStore() {
		if err := h.insertTradingRecords(c.Request.Context(), order, position); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "failed to persist trading records",
			})
			return
		}
	} else {
		h.mu.Lock()
		h.orders[orderID] = order
		h.positions[positionID] = position
		h.mu.Unlock()
	}

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

	if h.usesPersistentStore() {
		order, err := h.cancelOrderPersistent(c.Request.Context(), req.OrderID)
		if err != nil {
			switch {
			case errors.Is(err, errTradingOrderNotFound):
				c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "order not found"})
			case errors.Is(err, errTradingOrderNotOpen):
				c.JSON(http.StatusConflict, gin.H{"status": "error", "error": "order is not open"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to cancel order"})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   order,
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

	if h.usesPersistentStore() {
		position, err := h.liquidatePersistent(c.Request.Context(), positionID, symbol)
		if err != nil {
			if errors.Is(err, errTradingPositionNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "open position not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to liquidate position"})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   position,
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
	if h.usesPersistentStore() {
		positions, err := h.liquidateAllPersistent(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to liquidate positions"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data": gin.H{
				"count":     len(positions),
				"positions": positions,
			},
		})
		return
	}

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

	if h.usesPersistentStore() {
		positions, err := h.listPositionsPersistent(c.Request.Context(), statusFilter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to list positions"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data": gin.H{
				"count":     len(positions),
				"positions": positions,
			},
		})
		return
	}

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

	if h.usesPersistentStore() {
		position, err := h.getPositionPersistent(c.Request.Context(), positionID)
		if err != nil {
			if errors.Is(err, errTradingPositionNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "position not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "error": "failed to get position"})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   position,
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

func (h *TradingHandler) usesPersistentStore() bool {
	return h != nil && h.db != nil
}

func (h *TradingHandler) generateIDs(now time.Time) (string, string) {
	h.mu.Lock()
	h.sequence++
	seq := h.sequence
	h.mu.Unlock()

	base := now.Format("20060102150405") + "-" + decimal.NewFromInt(seq).String()
	return "ord-" + base, "pos-" + base
}

func (h *TradingHandler) initializeTradingStore() error {
	ctx := contextWithTimeout()
	_, err := h.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS trading_orders (
			order_id TEXT PRIMARY KEY,
			position_id TEXT NOT NULL,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			type TEXT NOT NULL,
			amount NUMERIC NOT NULL,
			price NUMERIC NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("create trading_orders failed: %w", err)
	}

	_, err = h.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS trading_positions (
			position_id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			size NUMERIC NOT NULL,
			entry_price NUMERIC NOT NULL,
			status TEXT NOT NULL,
			opened_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("create trading_positions failed: %w", err)
	}

	_, err = h.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_trading_orders_position_id ON trading_orders(position_id)`)
	if err != nil {
		return fmt.Errorf("create trading_orders index failed: %w", err)
	}

	_, err = h.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_trading_positions_symbol_status ON trading_positions(symbol, status)`)
	if err != nil {
		return fmt.Errorf("create trading_positions index failed: %w", err)
	}

	return nil
}

func (h *TradingHandler) insertTradingRecords(ctx context.Context, order OrderRecord, position PositionRecord) error {
	if _, err := h.db.Exec(ctx, `
		INSERT INTO trading_orders (
			order_id, position_id, exchange, symbol, side, type, amount, price, status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, order.OrderID, order.PositionID, order.Exchange, order.Symbol, order.Side, order.Type, order.Amount, order.Price, order.Status, order.CreatedAt, order.UpdatedAt); err != nil {
		return err
	}

	if _, err := h.db.Exec(ctx, `
		INSERT INTO trading_positions (
			position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, position.PositionID, position.OrderID, position.Exchange, position.Symbol, position.Side, position.Size, position.EntryPrice, position.Status, position.OpenedAt, position.UpdatedAt); err != nil {
		return err
	}

	return nil
}

func (h *TradingHandler) cancelOrderPersistent(ctx context.Context, orderID string) (OrderRecord, error) {
	var order OrderRecord
	err := h.db.QueryRow(ctx, `
		SELECT order_id, position_id, exchange, symbol, side, type, amount, price, status, created_at, updated_at
		FROM trading_orders
		WHERE order_id = $1
	`, orderID).Scan(
		&order.OrderID,
		&order.PositionID,
		&order.Exchange,
		&order.Symbol,
		&order.Side,
		&order.Type,
		&order.Amount,
		&order.Price,
		&order.Status,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if err != nil {
		if isNoRowsError(err) {
			return OrderRecord{}, errTradingOrderNotFound
		}
		return OrderRecord{}, err
	}

	if order.Status != "OPEN" {
		return OrderRecord{}, errTradingOrderNotOpen
	}

	now := time.Now().UTC()
	if _, err := h.db.Exec(ctx, `
		UPDATE trading_orders
		SET status = 'CANCELED', updated_at = $2
		WHERE order_id = $1
	`, orderID, now); err != nil {
		return OrderRecord{}, err
	}

	if _, err := h.db.Exec(ctx, `
		UPDATE trading_positions
		SET status = 'CLOSED', updated_at = $2
		WHERE position_id = $1 AND status = 'OPEN'
	`, order.PositionID, now); err != nil {
		return OrderRecord{}, err
	}

	order.Status = "CANCELED"
	order.UpdatedAt = now
	return order, nil
}

func (h *TradingHandler) liquidatePersistent(ctx context.Context, positionID, symbol string) (PositionRecord, error) {
	query := `
		SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at
		FROM trading_positions
		WHERE status = 'OPEN'`
	args := make([]interface{}, 0, 1)
	if positionID != "" {
		query += " AND position_id = $1"
		args = append(args, positionID)
	} else {
		query += " AND LOWER(symbol) = LOWER($1)"
		args = append(args, symbol)
	}
	query += " LIMIT 1"

	var position PositionRecord
	err := h.db.QueryRow(ctx, query, args...).Scan(
		&position.PositionID,
		&position.OrderID,
		&position.Exchange,
		&position.Symbol,
		&position.Side,
		&position.Size,
		&position.EntryPrice,
		&position.Status,
		&position.OpenedAt,
		&position.UpdatedAt,
	)
	if err != nil {
		if isNoRowsError(err) {
			return PositionRecord{}, errTradingPositionNotFound
		}
		return PositionRecord{}, err
	}

	now := time.Now().UTC()
	if _, err := h.db.Exec(ctx, `
		UPDATE trading_positions
		SET status = 'LIQUIDATED', updated_at = $2
		WHERE position_id = $1
	`, position.PositionID, now); err != nil {
		return PositionRecord{}, err
	}

	if _, err := h.db.Exec(ctx, `
		UPDATE trading_orders
		SET status = 'CLOSED', updated_at = $2
		WHERE order_id = $1 AND status = 'OPEN'
	`, position.OrderID, now); err != nil {
		return PositionRecord{}, err
	}

	position.Status = "LIQUIDATED"
	position.UpdatedAt = now
	return position, nil
}

func (h *TradingHandler) liquidateAllPersistent(ctx context.Context) ([]PositionRecord, error) {
	rows, err := h.db.Query(ctx, `
		SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at
		FROM trading_positions
		WHERE status = 'OPEN'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := make([]PositionRecord, 0)
	for rows.Next() {
		var p PositionRecord
		if err := rows.Scan(
			&p.PositionID,
			&p.OrderID,
			&p.Exchange,
			&p.Symbol,
			&p.Side,
			&p.Size,
			&p.EntryPrice,
			&p.Status,
			&p.OpenedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		positions = append(positions, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	for i := range positions {
		if _, err := h.db.Exec(ctx, `
			UPDATE trading_positions
			SET status = 'LIQUIDATED', updated_at = $2
			WHERE position_id = $1
		`, positions[i].PositionID, now); err != nil {
			return nil, err
		}

		if _, err := h.db.Exec(ctx, `
			UPDATE trading_orders
			SET status = 'CLOSED', updated_at = $2
			WHERE order_id = $1 AND status = 'OPEN'
		`, positions[i].OrderID, now); err != nil {
			return nil, err
		}

		positions[i].Status = "LIQUIDATED"
		positions[i].UpdatedAt = now
	}

	return positions, nil
}

func (h *TradingHandler) listPositionsPersistent(ctx context.Context, statusFilter string) ([]PositionRecord, error) {
	query := `
		SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at
		FROM trading_positions`
	args := make([]interface{}, 0, 1)
	if statusFilter != "" {
		query += " WHERE status = $1"
		args = append(args, statusFilter)
	}
	query += " ORDER BY opened_at DESC"

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := make([]PositionRecord, 0)
	for rows.Next() {
		var p PositionRecord
		if err := rows.Scan(
			&p.PositionID,
			&p.OrderID,
			&p.Exchange,
			&p.Symbol,
			&p.Side,
			&p.Size,
			&p.EntryPrice,
			&p.Status,
			&p.OpenedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		positions = append(positions, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return positions, nil
}

func (h *TradingHandler) getPositionPersistent(ctx context.Context, positionID string) (PositionRecord, error) {
	var p PositionRecord
	err := h.db.QueryRow(ctx, `
		SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at
		FROM trading_positions
		WHERE position_id = $1
	`, positionID).Scan(
		&p.PositionID,
		&p.OrderID,
		&p.Exchange,
		&p.Symbol,
		&p.Side,
		&p.Size,
		&p.EntryPrice,
		&p.Status,
		&p.OpenedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if isNoRowsError(err) {
			return PositionRecord{}, errTradingPositionNotFound
		}
		return PositionRecord{}, err
	}

	return p, nil
}

func isNoRowsError(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows)
}

func contextWithTimeout() context.Context {
	return context.Background()
}
