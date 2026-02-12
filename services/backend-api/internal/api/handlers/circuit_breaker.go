package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
)

// CircuitBreakerHandler handles circuit breaker admin endpoints.
type CircuitBreakerHandler struct {
	collectorService *services.CollectorService
}

// NewCircuitBreakerHandler creates a new circuit breaker handler.
func NewCircuitBreakerHandler(collectorService *services.CollectorService) *CircuitBreakerHandler {
	return &CircuitBreakerHandler{
		collectorService: collectorService,
	}
}

// CircuitBreakerStatsResponse represents the response for circuit breaker stats.
type CircuitBreakerStatsResponse struct {
	Breakers map[string]services.CircuitBreakerStats `json:"breakers"`
	Names    []string                                `json:"names"`
}

// GetCircuitBreakerStats returns the status and statistics of all circuit breakers.
//
//	@Summary		Get circuit breaker stats
//	@Description	Returns statistics for all circuit breakers
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	CircuitBreakerStatsResponse
//	@Router			/api/v1/admin/circuit-breakers [get]
func (h *CircuitBreakerHandler) GetCircuitBreakerStats(c *gin.Context) {
	stats := h.collectorService.GetCircuitBreakerStats()
	names := h.collectorService.GetCircuitBreakerNames()

	c.JSON(http.StatusOK, CircuitBreakerStatsResponse{
		Breakers: stats,
		Names:    names,
	})
}

// ResetCircuitBreakerRequest represents a request to reset a circuit breaker.
type ResetCircuitBreakerRequest struct {
	Name string `json:"name" binding:"required"`
}

// ResetCircuitBreakerResponse represents the response for a reset operation.
type ResetCircuitBreakerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
}

// ResetCircuitBreaker resets a specific circuit breaker by name.
//
//	@Summary		Reset a circuit breaker
//	@Description	Manually resets a circuit breaker to closed state
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Circuit breaker name"
//	@Success		200		{object}	ResetCircuitBreakerResponse
//	@Failure		404		{object}	ResetCircuitBreakerResponse
//	@Router			/api/v1/admin/circuit-breakers/{name}/reset [post]
func (h *CircuitBreakerHandler) ResetCircuitBreaker(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, ResetCircuitBreakerResponse{
			Success: false,
			Message: "circuit breaker name is required",
		})
		return
	}

	success := h.collectorService.ResetCircuitBreaker(name)
	if !success {
		c.JSON(http.StatusNotFound, ResetCircuitBreakerResponse{
			Success: false,
			Message: "circuit breaker not found",
			Name:    name,
		})
		return
	}

	c.JSON(http.StatusOK, ResetCircuitBreakerResponse{
		Success: true,
		Message: "circuit breaker reset successfully",
		Name:    name,
	})
}

// ResetAllCircuitBreakers resets all circuit breakers.
//
//	@Summary		Reset all circuit breakers
//	@Description	Manually resets all circuit breakers to closed state
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	ResetCircuitBreakerResponse
//	@Router			/api/v1/admin/circuit-breakers/reset-all [post]
func (h *CircuitBreakerHandler) ResetAllCircuitBreakers(c *gin.Context) {
	h.collectorService.ResetAllCircuitBreakers()

	c.JSON(http.StatusOK, ResetCircuitBreakerResponse{
		Success: true,
		Message: "all circuit breakers reset successfully",
	})
}
