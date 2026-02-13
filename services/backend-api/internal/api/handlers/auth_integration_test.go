package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthIntegration_RequireAuth tests authentication middleware integration with handlers
func TestAuthIntegration_RequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := middleware.NewAuthMiddleware("test-secret-key")

	// Create a test router with auth middleware
	createProtectedRouter := func() *gin.Engine {
		router := gin.New()
		protected := router.Group("/api/v1")
		protected.Use(auth.RequireAuth())
		{
			protected.GET("/profile", func(c *gin.Context) {
				userID := c.GetString("user_id")
				userEmail := c.GetString("user_email")
				c.JSON(http.StatusOK, gin.H{
					"user_id": userID,
					"email":   userEmail,
					"message": "Profile accessed successfully",
				})
			})
			protected.POST("/update-profile", func(c *gin.Context) {
				userID := c.GetString("user_id")
				c.JSON(http.StatusOK, gin.H{
					"user_id": userID,
					"message": "Profile updated successfully",
				})
			})
		}
		return router
	}

	t.Run("valid token access", func(t *testing.T) {
		router := createProtectedRouter()

		// Generate valid token
		token, err := auth.GenerateToken("user123", "test@example.com", time.Hour)
		require.NoError(t, err)

		// Test GET request
		req := httptest.NewRequest("GET", "/api/v1/profile", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "user123", response["user_id"])
		assert.Equal(t, "test@example.com", response["email"])
		assert.Equal(t, "Profile accessed successfully", response["message"])
	})

	t.Run("missing authorization header", func(t *testing.T) {
		router := createProtectedRouter()

		req := httptest.NewRequest("GET", "/api/v1/profile", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Authorization header required", response["error"])
	})

	t.Run("invalid token format", func(t *testing.T) {
		router := createProtectedRouter()

		req := httptest.NewRequest("GET", "/api/v1/profile", nil)
		req.Header.Set("Authorization", "InvalidToken")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Invalid authorization header format", response["error"])
	})

	t.Run("expired token", func(t *testing.T) {
		router := createProtectedRouter()

		// Generate expired token
		token, err := auth.GenerateToken("user123", "test@example.com", -time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/api/v1/profile", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response["error"], "Token expired")
	})

	t.Run("POST request with valid token", func(t *testing.T) {
		router := createProtectedRouter()

		// Generate valid token
		token, err := auth.GenerateToken("user456", "user456@example.com", time.Hour)
		require.NoError(t, err)

		// Create request body
		requestBody := map[string]interface{}{
			"name": "Updated Name",
			"bio":  "Updated bio",
		}
		body, _ := json.Marshal(requestBody)

		req := httptest.NewRequest("POST", "/api/v1/update-profile", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "user456", response["user_id"])
		assert.Equal(t, "Profile updated successfully", response["message"])
	})
}

// TestAuthIntegration_OptionalAuth tests optional authentication middleware integration
func TestAuthIntegration_OptionalAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := middleware.NewAuthMiddleware("test-secret-key")

	// Create a test router with optional auth middleware
	createOptionalAuthRouter := func() *gin.Engine {
		router := gin.New()
		api := router.Group("/api/v1")
		api.Use(auth.OptionalAuth())
		{
			api.GET("/public-data", func(c *gin.Context) {
				userID := c.GetString("user_id")
				userEmail := c.GetString("user_email")

				response := gin.H{
					"data": "This is public data",
				}

				if userID != "" {
					response["authenticated"] = true
					response["user_id"] = userID
					response["email"] = userEmail
					response["personalized"] = "Welcome back, " + userEmail
				} else {
					response["authenticated"] = false
					response["message"] = "Public access"
				}

				c.JSON(http.StatusOK, response)
			})
		}
		return router
	}

	t.Run("with valid token", func(t *testing.T) {
		router := createOptionalAuthRouter()

		// Generate valid token
		token, err := auth.GenerateToken("user789", "user789@example.com", time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/api/v1/public-data", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, true, response["authenticated"])
		assert.Equal(t, "user789", response["user_id"])
		assert.Equal(t, "user789@example.com", response["email"])
		assert.Equal(t, "Welcome back, user789@example.com", response["personalized"])
		assert.Equal(t, "This is public data", response["data"])
	})

	t.Run("without token", func(t *testing.T) {
		router := createOptionalAuthRouter()

		req := httptest.NewRequest("GET", "/api/v1/public-data", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, false, response["authenticated"])
		assert.Equal(t, "Public access", response["message"])
		assert.Equal(t, "This is public data", response["data"])
		// Ensure user fields are not present
		assert.Nil(t, response["user_id"])
		assert.Nil(t, response["email"])
	})

	t.Run("with invalid token format", func(t *testing.T) {
		router := createOptionalAuthRouter()

		req := httptest.NewRequest("GET", "/api/v1/public-data", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should still work as public access since optional auth ignores invalid tokens
		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, false, response["authenticated"])
		assert.Equal(t, "Public access", response["message"])
	})

	t.Run("with expired token", func(t *testing.T) {
		router := createOptionalAuthRouter()

		// Generate expired token
		token, err := auth.GenerateToken("user789", "user789@example.com", -time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/api/v1/public-data", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Should still work as public access since optional auth ignores expired tokens
		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, false, response["authenticated"])
		assert.Equal(t, "Public access", response["message"])
	})
}

// TestAuthIntegration_MiddlewareChain tests middleware chain execution with auth
func TestAuthIntegration_MiddlewareChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := middleware.NewAuthMiddleware("test-secret-key")

	t.Run("middleware execution order with auth", func(t *testing.T) {
		var executionOrder []string

		router := gin.New()

		// First middleware - logging
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "logging-start")
			c.Next()
			executionOrder = append(executionOrder, "logging-end")
		})

		// Auth middleware
		router.Use(auth.RequireAuth())

		// Third middleware - should not execute on auth failure
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "post-auth-start")
			c.Next()
			executionOrder = append(executionOrder, "post-auth-end")
		})

		router.GET("/test", func(c *gin.Context) {
			executionOrder = append(executionOrder, "handler")
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		// Test with missing auth - should abort chain
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		// Should execute logging start but not post-auth or handler
		expected := []string{"logging-start", "logging-end"}
		assert.Equal(t, expected, executionOrder)
	})

	t.Run("successful middleware chain execution", func(t *testing.T) {
		var executionOrder []string

		router := gin.New()

		// First middleware
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "first-start")
			c.Next()
			executionOrder = append(executionOrder, "first-end")
		})

		// Auth middleware
		router.Use(auth.RequireAuth())

		// Third middleware
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "third-start")
			c.Next()
			executionOrder = append(executionOrder, "third-end")
		})

		router.GET("/test", func(c *gin.Context) {
			executionOrder = append(executionOrder, "handler")
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		// Generate valid token
		token, err := auth.GenerateToken("user123", "test@example.com", time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		// Should execute all middleware in correct order
		expected := []string{"first-start", "third-start", "handler", "third-end", "first-end"}
		assert.Equal(t, expected, executionOrder)
	})
}
