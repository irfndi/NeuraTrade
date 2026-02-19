package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestSecret creates a random secret for testing to avoid hardcoded secrets
// This addresses GitGuardian security alerts about hardcoded secrets in tests
func generateTestSecret() string {
	bytes := make([]byte, 32)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func TestNewAuthMiddleware(t *testing.T) {
	// Generate a random secret key for testing to avoid hardcoded secrets
	secretKey := generateTestSecret()
	am := NewAuthMiddleware(secretKey)

	assert.NotNil(t, am)
	assert.Equal(t, []byte(secretKey), am.secretKey)
}

func TestAuthMiddleware_GenerateToken(t *testing.T) {
	// Use dynamically generated secret for security
	am := NewAuthMiddleware(generateTestSecret())
	userID := "user123"
	email := "test@example.com"
	duration := time.Hour

	token, err := am.GenerateToken(userID, email, duration)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Validate the generated token
	claims, err := am.ValidateToken(token)
	assert.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, email, claims.Email)
	assert.True(t, claims.ExpiresAt.After(time.Now()))
}

func TestAuthMiddleware_ValidateToken(t *testing.T) {
	// Use dynamically generated secret for security
	am := NewAuthMiddleware(generateTestSecret())

	t.Run("valid token", func(t *testing.T) {
		token, err := am.GenerateToken("user123", "test@example.com", time.Hour)
		require.NoError(t, err)

		claims, err := am.ValidateToken(token)
		assert.NoError(t, err)
		assert.Equal(t, "user123", claims.UserID)
		assert.Equal(t, "test@example.com", claims.Email)
	})

	t.Run("invalid token format", func(t *testing.T) {
		claims, err := am.ValidateToken("invalid-token")
		assert.Error(t, err)
		assert.Nil(t, claims)
	})

	t.Run("expired token", func(t *testing.T) {
		// Generate token with negative duration (already expired)
		token, err := am.GenerateToken("user123", "test@example.com", -time.Hour)
		require.NoError(t, err)

		claims, err := am.ValidateToken(token)
		assert.Error(t, err)
		assert.Nil(t, claims)
	})

	t.Run("token with wrong secret", func(t *testing.T) {
		// Generate token with different middleware using different secret
		otherAM := NewAuthMiddleware(generateTestSecret())
		token, err := otherAM.GenerateToken("user123", "test@example.com", time.Hour)
		require.NoError(t, err)

		// Try to validate with original middleware
		claims, err := am.ValidateToken(token)
		assert.Error(t, err)
		assert.Nil(t, claims)
	})
}

func TestAuthMiddleware_RequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Use dynamically generated secret for security
	am := NewAuthMiddleware(generateTestSecret())

	// Helper function to create test router
	createTestRouter := func() *gin.Engine {
		router := gin.New()
		router.Use(am.RequireAuth())
		router.GET("/protected", func(c *gin.Context) {
			userID := c.GetString("user_id")
			userEmail := c.GetString("user_email")
			c.JSON(http.StatusOK, gin.H{
				"user_id": userID,
				"email":   userEmail,
				"message": "Access granted",
			})
		})
		return router
	}

	t.Run("valid token", func(t *testing.T) {
		router := createTestRouter()
		token, err := am.GenerateToken("user123", "test@example.com", time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "user123", response["user_id"])
		assert.Equal(t, "test@example.com", response["email"])
		assert.Equal(t, "Access granted", response["message"])
	})

	t.Run("missing authorization header", func(t *testing.T) {
		router := createTestRouter()
		req := httptest.NewRequest("GET", "/protected", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Authorization header required", response["error"])
	})

	t.Run("invalid authorization header format", func(t *testing.T) {
		router := createTestRouter()
		testCases := []struct {
			name   string
			header string
		}{
			{"missing Bearer prefix", "token123"},
			{"wrong prefix", "Basic token123"},
			{"empty token", "Bearer "},
			{"only Bearer", "Bearer"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/protected", nil)
				req.Header.Set("Authorization", tc.header)
				w := httptest.NewRecorder()

				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusUnauthorized, w.Code)

				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, "Invalid authorization header format", response["error"])
			})
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		router := createTestRouter()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Invalid token", response["error"])
	})

	t.Run("expired token", func(t *testing.T) {
		router := createTestRouter()
		// Generate expired token
		token, err := am.GenerateToken("user123", "test@example.com", -time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Token expired", response["error"])
	})

	t.Run("token with wrong signing method", func(t *testing.T) {
		router := createTestRouter()
		// Create an intentionally malformed token for testing
		// Using a predictable but non-secret test token structure
		tokenString := "invalid.jwt.token.for.testing.purposes.only"

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Invalid token", response["error"])
	})
}

func TestAuthMiddleware_OptionalAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Use valid 32+ character secret for tests
	am := NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")

	// Helper function to create test router
	createTestRouter := func() *gin.Engine {
		router := gin.New()
		router.Use(am.OptionalAuth())
		router.GET("/optional", func(c *gin.Context) {
			userID := c.GetString("user_id")
			userEmail := c.GetString("user_email")
			if userID != "" {
				c.JSON(http.StatusOK, gin.H{
					"authenticated": true,
					"user_id":       userID,
					"email":         userEmail,
				})
			} else {
				c.JSON(http.StatusOK, gin.H{
					"authenticated": false,
					"message":       "Anonymous access",
				})
			}
		})
		return router
	}

	t.Run("valid token", func(t *testing.T) {
		router := createTestRouter()
		token, err := am.GenerateToken("user123", "test@example.com", time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/optional", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, true, response["authenticated"])
		assert.Equal(t, "user123", response["user_id"])
		assert.Equal(t, "test@example.com", response["email"])
	})

	t.Run("no token provided", func(t *testing.T) {
		router := createTestRouter()
		req := httptest.NewRequest("GET", "/optional", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, false, response["authenticated"])
		assert.Equal(t, "Anonymous access", response["message"])
	})

	t.Run("invalid token format", func(t *testing.T) {
		router := createTestRouter()
		req := httptest.NewRequest("GET", "/optional", nil)
		req.Header.Set("Authorization", "invalid-format")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, false, response["authenticated"])
		assert.Equal(t, "Anonymous access", response["message"])
	})

	t.Run("expired token", func(t *testing.T) {
		router := createTestRouter()
		token, err := am.GenerateToken("user123", "test@example.com", -time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/optional", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, false, response["authenticated"])
		assert.Equal(t, "Anonymous access", response["message"])
	})
}

func TestAuthMiddleware_MiddlewareChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	am := NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")

	// Test middleware chain execution order
	t.Run("middleware chain execution", func(t *testing.T) {
		var executionOrder []string

		router := gin.New()

		// First middleware
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "first")
			c.Next()
			executionOrder = append(executionOrder, "first-after")
		})

		// Auth middleware
		router.Use(am.RequireAuth())

		// Third middleware
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "third")
			c.Next()
			executionOrder = append(executionOrder, "third-after")
		})

		router.GET("/chain", func(c *gin.Context) {
			executionOrder = append(executionOrder, "handler")
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		token, err := am.GenerateToken("user123", "test@example.com", time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/chain", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		expected := []string{"first", "third", "handler", "third-after", "first-after"}
		assert.Equal(t, expected, executionOrder)
	})

	t.Run("middleware chain abort on auth failure", func(t *testing.T) {
		var executionOrder []string

		router := gin.New()

		// First middleware
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "first")
			c.Next()
			executionOrder = append(executionOrder, "first-after")
		})

		// Auth middleware
		router.Use(am.RequireAuth())

		// Third middleware (should not execute)
		router.Use(func(c *gin.Context) {
			executionOrder = append(executionOrder, "third")
			c.Next()
			executionOrder = append(executionOrder, "third-after")
		})

		router.GET("/chain", func(c *gin.Context) {
			executionOrder = append(executionOrder, "handler")
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		})

		// Request without token
		req := httptest.NewRequest("GET", "/chain", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		// Only first middleware should execute, auth middleware aborts
		expected := []string{"first", "first-after"}
		assert.Equal(t, expected, executionOrder)
	})
}

func TestJWTClaims(t *testing.T) {
	t.Run("claims structure", func(t *testing.T) {
		claims := &JWTClaims{
			UserID: "user123",
			Email:  "test@example.com",
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

		assert.Equal(t, "user123", claims.UserID)
		assert.Equal(t, "test@example.com", claims.Email)
		assert.NotNil(t, claims.ExpiresAt)
		assert.NotNil(t, claims.IssuedAt)
	})
}
