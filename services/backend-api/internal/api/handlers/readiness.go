package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

// ReadinessHandler exposes paper trading readiness endpoints.
type ReadinessHandler struct {
	db DBPool
}

// NewReadinessHandler creates a readiness handler backed by the database.
func NewReadinessHandler(db DBPool) *ReadinessHandler {
	return &ReadinessHandler{db: db}
}

// PaperTradingManifest returns the unified paper trading readiness manifest.
//
// Query parameters:
//   - start: RFC3339 start time (default: 7 days ago)
//   - end: RFC3339 end time (default: now)
//   - strategies: comma-separated list (default: scalping,daily_trading,swing_trading,arbitrage)
func (h *ReadinessHandler) PaperTradingManifest(c *gin.Context) {
	startTime := time.Now().Add(-7 * 24 * time.Hour)
	if raw := c.Query("start"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			startTime = parsed
		}
	}

	endTime := time.Now()
	if raw := c.Query("end"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			endTime = parsed
		}
	}

	strategies := []string{"scalping", "daily_trading", "swing_trading", "arbitrage"}
	if raw := c.Query("strategies"); raw != "" {
		strategies = parseStrategyList(raw)
	}

	logger := &readinessNopLogger{}
	generator := services.NewReadinessManifestGenerator(h.db, logger)
	manifest, err := generator.GenerateManifest(c.Request.Context(), startTime, endTime, strategies)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusOK
	if !manifest.Acceptance.Ready {
		status = http.StatusPreconditionFailed
	}

	c.JSON(status, manifest)
}

// PaperTradingEvidence returns a single-strategy evidence artifact.
//
// Query parameters:
//   - start: RFC3339 start time (default: 7 days ago)
//   - end: RFC3339 end time (default: now)
//   - strategy: strategy name (default: scalping)
func (h *ReadinessHandler) PaperTradingEvidence(c *gin.Context) {
	startTime := time.Now().Add(-7 * 24 * time.Hour)
	if raw := c.Query("start"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			startTime = parsed
		}
	}

	endTime := time.Now()
	if raw := c.Query("end"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			endTime = parsed
		}
	}

	strategy := c.DefaultQuery("strategy", "scalping")
	strategies := []string{strategy}

	logger := &readinessNopLogger{}
	generator := services.NewPaperTradingEvidenceGenerator(h.db, logger)
	evidence, err := generator.GenerateEvidence(startTime, endTime, strategies, decimal.Zero)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, evidence)
}

func parseStrategyList(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

type readinessNopLogger struct{}

func (n *readinessNopLogger) WithFields(_ map[string]interface{}) services.Logger { return n }
func (n *readinessNopLogger) Info(_ string)                                       {}
func (n *readinessNopLogger) Warn(_ string)                                       {}
func (n *readinessNopLogger) Error(_ string)                                      {}
