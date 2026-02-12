package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/irfndi/neuratrade/internal/telemetry"
)

func TestTelemetryMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Initialize telemetry for testing
	config := telemetry.DefaultConfig()
	config.Enabled = false // Disable for testing to avoid network calls
	err := telemetry.InitTelemetry(*config)
	require.NoError(t, err)

	t.Run("regular request tracing", func(t *testing.T) {
		router := gin.New()
		router.Use(TelemetryMiddleware())
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"message": "test"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("User-Agent", "test-agent")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "test")
	})

	t.Run("health check endpoint skip", func(t *testing.T) {
		router := gin.New()
		router.Use(TelemetryMiddleware())
		router.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "healthy"})
		})

		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "healthy")
	})

	t.Run("error response tracing", func(t *testing.T) {
		router := gin.New()
		router.Use(TelemetryMiddleware())
		router.GET("/error", func(c *gin.Context) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		})

		req := httptest.NewRequest("GET", "/error", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "internal error")
	})
}

func TestHealthCheckTelemetryMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("health check middleware", func(t *testing.T) {
		router := gin.New()
		router.Use(HealthCheckTelemetryMiddleware())
		router.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "healthy"})
		})

		req := httptest.NewRequest("GET", "/health", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "healthy")
	})
}

func TestRecordError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("record error", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/test", nil)
		c.Request = req

		testErr := assert.AnError
		description := "test error description"

		// This should not panic
		RecordError(c, testErr, description)
	})
}

func TestAddSpanAttribute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("add string attribute", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/test", nil)
		c.Request = req

		// This should not panic
		AddSpanAttribute(c, "test_key", "test_value")
	})

	t.Run("add int attribute", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/test", nil)
		c.Request = req

		AddSpanAttribute(c, "test_key", 42)
	})
}

func TestStartSpan(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("start new span", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/test", nil)
		c.Request = req

		// This should not panic
		StartSpan(c, "test_span")
	})
}
