package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/redis/go-redis/v9"
)

// RedisInterface defines the contract for Redis operations used by ExchangeHandler.
type RedisInterface interface {
	// Get retrieves the value associated with the key.
	Get(ctx context.Context, key string) *redis.StringCmd
	// Set stores the key-value pair with an expiration.
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	// Del deletes one or more keys.
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// CollectorInterface defines the contract for collector service operations.
type CollectorInterface interface {
	// Start initiates the collector service.
	Start() error
	// Stop terminates the collector service.
	Stop()
	// RestartWorker restarts the worker for a specific exchange.
	RestartWorker(exchangeID string) error
	// IsReady checks if the service is ready.
	IsReady() bool
	// IsInitialized checks if the service is initialized.
	IsInitialized() bool
}

// ExchangeHandler manages exchange configuration and operations.
type ExchangeHandler struct {
	ccxtService      ccxt.CCXTService
	collectorService CollectorInterface
	redisClient      RedisInterface
}

// NewExchangeHandler creates a new instance of ExchangeHandler.
//
// Parameters:
//
//	ccxtService: The CCXT service.
//	collectorService: The collector service.
//	redisClient: The Redis client.
//
// Returns:
//
//	*ExchangeHandler: The initialized handler.
func NewExchangeHandler(ccxtService ccxt.CCXTService, collectorService CollectorInterface, redisClient RedisInterface) *ExchangeHandler {
	return &ExchangeHandler{
		ccxtService:      ccxtService,
		collectorService: collectorService,
		redisClient:      redisClient,
	}
}

// GetExchangeConfig retrieves the current exchange configuration.
// It utilizes caching to improve performance.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) GetExchangeConfig(c *gin.Context) {
	cacheKey := "exchange:config"
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()

	// Try to get from cache first (with nil guard)
	if h.redisClient != nil {
		cachedData, err := h.redisClient.Get(ctx, cacheKey).Result()
		if err == nil {
			var config interface{}
			if err := json.Unmarshal([]byte(cachedData), &config); err == nil {
				c.JSON(http.StatusOK, config)
				return
			}
			// Unmarshal failed, continue to fetch fresh data
		}
	}

	// Cache miss or error, fetch from service
	config, err := h.ccxtService.GetExchangeConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get exchange configuration",
			"message": err.Error(),
		})
		return
	}

	// Cache the result for 1 hour (with nil guard)
	if h.redisClient != nil {
		if configData, err := json.Marshal(config); err == nil {
			h.redisClient.Set(ctx, cacheKey, configData, time.Hour)
		}
	}

	c.JSON(http.StatusOK, config)
}

// AddExchangeToBlacklist adds an exchange to the blacklist.
// It also restarts the associated worker if necessary.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) AddExchangeToBlacklist(c *gin.Context) {
	exchange := c.Param("exchange")
	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Exchange parameter is required",
		})
		return
	}

	response, err := h.ccxtService.AddExchangeToBlacklist(c.Request.Context(), exchange)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to add exchange to blacklist",
			"message": err.Error(),
		})
		return
	}

	// Stop the worker for this exchange if it exists
	if h.collectorService != nil {
		if err := h.collectorService.RestartWorker(exchange); err != nil {
			// Log the error but don't fail the request
			// The exchange is already blacklisted in CCXT service
			c.Header("X-Worker-Restart-Warning", "Failed to restart worker")
		}
	}

	// Invalidate cache after blacklist change
	if h.redisClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()
		h.redisClient.Del(ctx, "exchange:config", "exchange:supported")
	}

	c.JSON(http.StatusOK, response)
}

// RemoveExchangeFromBlacklist removes an exchange from the blacklist.
// It triggers a worker restart to resume data collection.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) RemoveExchangeFromBlacklist(c *gin.Context) {
	exchange := c.Param("exchange")
	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Exchange parameter is required",
		})
		return
	}

	response, err := h.ccxtService.RemoveExchangeFromBlacklist(c.Request.Context(), exchange)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to remove exchange from blacklist",
			"message": err.Error(),
		})
		return
	}

	// Restart the collector service to pick up the new exchange
	if h.collectorService != nil {
		if err := h.collectorService.RestartWorker(exchange); err != nil {
			// If worker doesn't exist, that's fine - it will be created on next refresh
			c.Header("X-Worker-Restart-Info", "Worker restart attempted")
		}
	}

	// Invalidate cache after unblacklist change
	if h.redisClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()
		h.redisClient.Del(ctx, "exchange:config", "exchange:supported")
	}

	c.JSON(http.StatusOK, response)
}

// RefreshExchanges refreshes the configuration for all non-blacklisted exchanges.
// It restarts the collector service to apply changes.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) RefreshExchanges(c *gin.Context) {
	response, err := h.ccxtService.RefreshExchanges(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to refresh exchanges",
			"message": err.Error(),
		})
		return
	}

	// Restart the collector service to pick up any new exchanges
	if h.collectorService != nil {
		// Stop and restart the entire collector service
		h.collectorService.Stop()
		if err := h.collectorService.Start(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to restart collector service",
				"message": err.Error(),
			})
			return
		}
	}

	// Invalidate cache after refresh
	if h.redisClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()
		h.redisClient.Del(ctx, "exchange:config", "exchange:supported")
	}

	c.JSON(http.StatusOK, response)
}

// AddExchange dynamically adds and initializes a new exchange.
// It restarts the collector service to include the new exchange.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) AddExchange(c *gin.Context) {
	exchange := c.Param("exchange")
	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Exchange parameter is required",
		})
		return
	}

	response, err := h.ccxtService.AddExchange(c.Request.Context(), exchange)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to add exchange",
			"message": err.Error(),
		})
		return
	}

	// Restart the collector service to pick up the new exchange
	if h.collectorService != nil {
		h.collectorService.Stop()
		if err := h.collectorService.Start(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to restart collector service",
				"message": err.Error(),
			})
			return
		}
	}

	// Invalidate cache after adding exchange
	if h.redisClient != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()
		h.redisClient.Del(ctx, "exchange:config", "exchange:supported")
	}

	c.JSON(http.StatusOK, response)
}

// GetSupportedExchanges returns the list of currently supported exchanges.
// It caches the result for 30 minutes.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) GetSupportedExchanges(c *gin.Context) {
	cacheKey := "exchange:supported"
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()

	// Try to get from cache first (with nil guard)
	if h.redisClient != nil {
		cachedData, err := h.redisClient.Get(ctx, cacheKey).Result()
		if err == nil {
			var exchanges []string
			if err := json.Unmarshal([]byte(cachedData), &exchanges); err == nil {
				c.JSON(http.StatusOK, gin.H{
					"exchanges": exchanges,
					"count":     len(exchanges),
				})
				return
			}
			// Unmarshal failed, continue to fetch fresh data
		}
	}

	// Cache miss or error, fetch from service
	exchanges := h.ccxtService.GetSupportedExchanges()

	// Cache the result for 30 minutes (with nil guard)
	if h.redisClient != nil {
		if exchangesData, err := json.Marshal(exchanges); err == nil {
			h.redisClient.Set(ctx, cacheKey, exchangesData, 30*time.Minute)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"exchanges": exchanges,
		"count":     len(exchanges),
	})
}

// GetWorkerStatus returns the status of exchange workers.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) GetWorkerStatus(c *gin.Context) {
	if h.collectorService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Collector service not available",
		})
		return
	}

	// Get worker status from collector service
	// This would need to be implemented in the collector service
	c.JSON(http.StatusOK, gin.H{
		"message": "Worker status endpoint - implementation needed in collector service",
	})
}

// RestartWorker restarts a specific exchange worker.
//
// Parameters:
//
//	c: Gin context.
func (h *ExchangeHandler) RestartWorker(c *gin.Context) {
	exchange := c.Param("exchange")
	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Exchange parameter is required",
		})
		return
	}

	if h.collectorService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Collector service not available",
		})
		return
	}

	if err := h.collectorService.RestartWorker(exchange); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to restart worker",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Worker restarted successfully",
		"exchange": exchange,
	})
}
