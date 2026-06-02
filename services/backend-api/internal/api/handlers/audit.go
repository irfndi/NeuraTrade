package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
)

// AuditHandler serves trade audit log queries.
// All endpoints require admin authentication.
type AuditHandler struct {
	logger *services.TradeAuditLogger
}

// NewAuditHandler creates a handler for audit log query endpoints.
func NewAuditHandler(logger *services.TradeAuditLogger) *AuditHandler {
	return &AuditHandler{logger: logger}
}

// GetTrades returns trade audit log entries matching the query filters.
//
// Query parameters:
//   - chat_id: filter by Telegram chat ID (optional)
//   - symbol: filter by trading symbol (optional)
//   - from: RFC3339 start timestamp (optional)
//   - to: RFC3339 end timestamp (optional)
//   - limit: max entries to return (default 100, max 1000)
func (h *AuditHandler) GetTrades(c *gin.Context) {
	if h.logger == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "audit logger not initialized"})
		return
	}

	chatID := c.Query("chat_id")
	symbol := c.Query("symbol")

	var fromTime, toTime time.Time
	if fromStr := c.Query("from"); fromStr != "" {
		parsed, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from timestamp, use RFC3339 format"})
			return
		}
		fromTime = parsed
	}
	if toStr := c.Query("to"); toStr != "" {
		parsed, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to timestamp, use RFC3339 format"})
			return
		}
		toTime = parsed
	}

	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit, must be a positive integer"})
			return
		}
		if parsed > 1000 {
			parsed = 1000
		}
		limit = parsed
	}

	entries, err := h.logger.QueryTrades(c.Request.Context(), chatID, symbol, fromTime, toTime, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query audit log: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"entries": entries, "count": len(entries)})
}
