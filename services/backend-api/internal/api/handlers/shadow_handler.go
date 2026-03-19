package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
)

const maxShadowQueryWindow = 7 * 24 * time.Hour

type ShadowHandler struct {
	coordinator *services.ShadowEvaluationCoordinator
}

func NewShadowHandler(coordinator *services.ShadowEvaluationCoordinator) *ShadowHandler {
	return &ShadowHandler{coordinator: coordinator}
}

func (h *ShadowHandler) ListVariants(c *gin.Context) {
	if h.coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "shadow evaluation coordinator is unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"variants": h.coordinator.ListVariants()})
}

func (h *ShadowHandler) CreateVariant(c *gin.Context) {
	if h.coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "shadow evaluation coordinator is unavailable"})
		return
	}
	var req services.ShadowVariantConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	variant, err := h.coordinator.UpsertVariant(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"variant": variant})
}

func (h *ShadowHandler) DeleteVariant(c *gin.Context) {
	if h.coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "shadow evaluation coordinator is unavailable"})
		return
	}
	variantID := strings.TrimSpace(c.Param("id"))
	if variantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "variant id is required"})
		return
	}
	if !h.coordinator.DeleteVariant(variantID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "shadow variant not found or protected"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true, "variant_id": strings.ToLower(variantID)})
}

func (h *ShadowHandler) VariantDiagnostics(c *gin.Context) {
	if h.coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "shadow evaluation coordinator is unavailable"})
		return
	}
	variantID := strings.TrimSpace(c.Param("id"))
	if variantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "variant id is required"})
		return
	}
	diagnostics, err := h.coordinator.VariantDiagnostics(c.Request.Context(), variantID)
	if err != nil {
		if errors.Is(err, services.ErrVariantNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, diagnostics)
}

func (h *ShadowHandler) Comparison(c *gin.Context) {
	if h.coordinator == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "shadow evaluation coordinator is unavailable"})
		return
	}
	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)
	if value := strings.TrimSpace(c.Query("start")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start parameter, expected RFC3339"})
			return
		}
		start = parsed.UTC()
	}
	if value := strings.TrimSpace(c.Query("end")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end parameter, expected RFC3339"})
			return
		}
		end = parsed.UTC()
	}
	if start.After(end) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start must be before end"})
		return
	}
	if end.Sub(start) > maxShadowQueryWindow {
		c.JSON(http.StatusBadRequest, gin.H{"error": "time range too large, limit is 168h (7 days)"})
		return
	}
	report, err := h.coordinator.CompareLiveVsShadow(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}
