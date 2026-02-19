package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserAuthFlow_CompleteFlow tests the complete authentication flow
func TestUserAuthFlow_CompleteFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")

	// Create a router that simulates the complete auth flow
	createAuthFlowRouter := func() *gin.Engine {
		router := gin.New()

		// Public routes (no auth required)
		public := router.Group("/api/v1")
		{
			public.POST("/register", func(c *gin.Context) {
				var req struct {
					Email    string `json:"email"`
					Password string `json:"password"`
					Name     string `json:"name"`
				}

				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
					return
				}

				// Simulate user registration
				userID := "user_" + req.Email

				// Generate JWT token
				token, err := auth.GenerateToken(userID, req.Email, 24*time.Hour)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
					return
				}

				c.JSON(http.StatusCreated, gin.H{
					"message": "User registered successfully",
					"user_id": userID,
					"token":   token,
				})
			})

			public.POST("/login", func(c *gin.Context) {
				var req struct {
					Email    string `json:"email"`
					Password string `json:"password"`
				}

				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
					return
				}

				// Simulate login validation
				if req.Email == "" || req.Password == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Email and password required"})
					return
				}

				// Simulate invalid credentials
				if req.Password == TestWrongPassword {
					c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
					return
				}

				userID := "user_" + req.Email

				// Generate JWT token
				token, err := auth.GenerateToken(userID, req.Email, 24*time.Hour)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"message": "Login successful",
					"user_id": userID,
					"token":   token,
				})
			})
		}

		// Protected routes (auth required)
		protected := router.Group("/api/v1")
		protected.Use(auth.RequireAuth())
		{
			protected.GET("/profile", func(c *gin.Context) {
				userID := c.GetString("user_id")
				userEmail := c.GetString("user_email")

				c.JSON(http.StatusOK, gin.H{
					"user_id": userID,
					"email":   userEmail,
					"name":    "Test User",
					"bio":     "This is a test user profile",
				})
			})

			protected.PUT("/profile", func(c *gin.Context) {
				userID := c.GetString("user_id")

				var req struct {
					Name string `json:"name"`
					Bio  string `json:"bio"`
				}

				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"message": "Profile updated successfully",
					"user_id": userID,
					"name":    req.Name,
					"bio":     req.Bio,
				})
			})

			protected.DELETE("/account", func(c *gin.Context) {
				userID := c.GetString("user_id")

				c.JSON(http.StatusOK, gin.H{
					"message": "Account deleted successfully",
					"user_id": userID,
				})
			})
		}

		return router
	}

	t.Run("successful registration and profile access", func(t *testing.T) {
		router := createAuthFlowRouter()

		// Step 1: Register user
		registerReq := map[string]string{
			"email":    "test@example.com",
			"password": TestPassword123,
			"name":     "Test User",
		}
		body, _ := json.Marshal(registerReq)

		req := httptest.NewRequest("POST", "/api/v1/register", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var registerResp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &registerResp)
		assert.NoError(t, err)
		assert.Equal(t, "User registered successfully", registerResp["message"])
		assert.NotEmpty(t, registerResp["token"])

		// Step 2: Use token to access protected profile
		token := registerResp["token"].(string)

		req = httptest.NewRequest("GET", "/api/v1/profile", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var profileResp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &profileResp)
		assert.NoError(t, err)
		assert.Equal(t, "user_test@example.com", profileResp["user_id"])
		assert.Equal(t, "test@example.com", profileResp["email"])
	})

	t.Run("successful login and profile update", func(t *testing.T) {
		router := createAuthFlowRouter()

		// Step 1: Login
		loginReq := map[string]string{
			"email":    "user@example.com",
			"password": TestCorrectPassword,
		}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var loginResp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &loginResp)
		assert.NoError(t, err)
		assert.Equal(t, "Login successful", loginResp["message"])
		assert.NotEmpty(t, loginResp["token"])

		// Step 2: Update profile with token
		token := loginResp["token"].(string)

		updateReq := map[string]string{
			"name": "Updated Name",
			"bio":  "Updated bio description",
		}
		body, _ = json.Marshal(updateReq)

		req = httptest.NewRequest("PUT", "/api/v1/profile", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var updateResp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &updateResp)
		assert.NoError(t, err)
		assert.Equal(t, "Profile updated successfully", updateResp["message"])
		assert.Equal(t, "user_user@example.com", updateResp["user_id"])
		assert.Equal(t, "Updated Name", updateResp["name"])
	})

	t.Run("login with invalid credentials", func(t *testing.T) {
		router := createAuthFlowRouter()

		loginReq := map[string]string{
			"email":    "user@example.com",
			"password": TestWrongPassword,
		}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "Invalid credentials", resp["error"])
	})

	t.Run("access protected route without token", func(t *testing.T) {
		router := createAuthFlowRouter()

		req := httptest.NewRequest("GET", "/api/v1/profile", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Equal(t, "Authorization header required", resp["error"])
	})

	t.Run("access protected route with expired token", func(t *testing.T) {
		router := createAuthFlowRouter()

		// Generate expired token
		expiredToken, err := auth.GenerateToken("user123", "test@example.com", -time.Hour)
		require.NoError(t, err)

		req := httptest.NewRequest("DELETE", "/api/v1/account", nil)
		req.Header.Set("Authorization", "Bearer "+expiredToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NoError(t, err)
		assert.Contains(t, resp["error"], "Token expired")
	})
}

// TestUserAuthFlow_TokenValidation tests various token validation scenarios
func TestUserAuthFlow_TokenValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")

	// Create a simple protected endpoint
	createProtectedEndpoint := func() *gin.Engine {
		router := gin.New()
		router.Use(auth.RequireAuth())
		router.GET("/protected", func(c *gin.Context) {
			userID := c.GetString("user_id")
			c.JSON(http.StatusOK, gin.H{"user_id": userID})
		})
		return router
	}

	t.Run("valid token with different user IDs", func(t *testing.T) {
		router := createProtectedEndpoint()

		testCases := []struct {
			userID string
			email  string
		}{
			{"user1", "user1@example.com"},
			{"admin_user", "admin@example.com"},
			{"test_123", "test123@example.com"},
		}

		for _, tc := range testCases {
			t.Run("user_"+tc.userID, func(t *testing.T) {
				token, err := auth.GenerateToken(tc.userID, tc.email, time.Hour)
				require.NoError(t, err)

				req := httptest.NewRequest("GET", "/protected", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusOK, w.Code)
				var resp map[string]interface{}
				err = json.Unmarshal(w.Body.Bytes(), &resp)
				assert.NoError(t, err)
				assert.Equal(t, tc.userID, resp["user_id"])
			})
		}
	})

	t.Run("token with different expiration times", func(t *testing.T) {
		router := createProtectedEndpoint()

		// Test short-lived token
		shortToken, err := auth.GenerateToken("user1", "user1@example.com", time.Minute)
		require.NoError(t, err)

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+shortToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Test long-lived token
		longToken, err := auth.GenerateToken("user2", "user2@example.com", 24*time.Hour)
		require.NoError(t, err)

		req = httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+longToken)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("malformed tokens", func(t *testing.T) {
		router := createProtectedEndpoint()

		malformedTokens := []string{
			"not.a.token",
			"Bearer.token.here",
			"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.invalid.signature",
			"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", // incomplete
		}

		for i, token := range malformedTokens {
			t.Run("malformed_"+strconv.Itoa(i+1), func(t *testing.T) {
				req := httptest.NewRequest("GET", "/protected", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusUnauthorized, w.Code)
				var resp map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				assert.NoError(t, err)
				assert.Contains(t, resp["error"], "Invalid token")
			})
		}
	})
}
