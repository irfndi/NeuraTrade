package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusMetricsHandler serves Prometheus metrics.
type PrometheusMetricsHandler struct{}

// NewPrometheusMetricsHandler creates a new Prometheus metrics handler.
func NewPrometheusMetricsHandler() *PrometheusMetricsHandler {
	return &PrometheusMetricsHandler{}
}

// Handler returns an http.Handler that exposes Prometheus metrics.
func (h *PrometheusMetricsHandler) Handler() http.Handler {
	return promhttp.Handler()
}

// RegisterRoutes registers the /metrics endpoint on the provided router group.
func (h *PrometheusMetricsHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/metrics", gin.WrapH(h.Handler()))
}
