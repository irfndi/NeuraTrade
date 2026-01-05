package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfandi/celebrum-ai-go/internal/api"
	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TelegramE2ETestSuite is a test suite for end-to-end Telegram flow testing
type TelegramE2ETestSuite struct {
	suite.Suite
	db          *database.PostgresDB
	redisClient *database.RedisClient
	router      *gin.Engine
	adminAPIKey string
	testUserID  string
	testChatID  string
	testEmail   string
}

// SetupSuite runs once before all tests
func (s *TelegramE2ETestSuite) SetupSuite() {
	// Check for required environment variables
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		s.T().Skip("DATABASE_URL not set, skipping E2E test")
	}

	// Setup database connection
	var err error
	s.db, err = database.NewPostgresConnection(&config.DatabaseConfig{
		DatabaseURL:     dbURL,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: "60s",
	})
	require.NoError(s.T(), err)

	// Setup Redis if available
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = os.Getenv("REDIS_HOST")
		if redisAddr != "" {
			redisPort := os.Getenv("REDIS_PORT")
			if redisPort == "" {
				redisPort = "6379"
			}
			redisAddr = fmt.Sprintf("%s:%s", redisAddr, redisPort)
		}
	}
	if redisAddr != "" {
		s.redisClient = &database.RedisClient{
			Client: redis.NewClient(&redis.Options{Addr: redisAddr}),
		}
	}

	// Setup Router
	gin.SetMode(gin.TestMode)
	s.router = gin.New()

	// Load admin API key from environment with fallback for local testing
	s.adminAPIKey = os.Getenv("TEST_ADMIN_API_KEY")
	if s.adminAPIKey == "" {
		s.adminAPIKey = "test-admin-key-e2e-secure-key-32chars"
	}
	cfg := &config.TelegramConfig{
		AdminAPIKey: s.adminAPIKey,
		ServiceURL:  "http://telegram-service:3002",
	}

	// Load JWT secret from environment with fallback for local testing
	jwtSecret := os.Getenv("TEST_JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "test-jwt-secret-e2e"
	}

	// Create required middlewares
	authMiddleware := middleware.NewAuthMiddleware(jwtSecret)

	// Setup routes
	api.SetupRoutes(s.router, s.db, s.redisClient, nil, nil, nil, nil, nil, nil, cfg, authMiddleware)

	// Create test user
	s.testChatID = fmt.Sprintf("e2e_test_%d", time.Now().UnixNano())
	s.testEmail = fmt.Sprintf("e2e_test_%s@celebrum.ai", uuid.New().String())
	s.testUserID = uuid.New().String()

	_, err = s.db.Pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
		VALUES ($1, $2, 'hashed_password', $3, 'free', NOW(), NOW())
	`, s.testUserID, s.testEmail, s.testChatID)
	require.NoError(s.T(), err)
}

// TearDownSuite runs once after all tests
func (s *TelegramE2ETestSuite) TearDownSuite() {
	if s.db != nil {
		// Clean up test data with error logging
		if _, err := s.db.Pool.Exec(context.Background(), "DELETE FROM user_alerts WHERE user_id = $1", s.testUserID); err != nil {
			s.T().Logf("failed to clean up user_alerts: %v", err)
		}
		if _, err := s.db.Pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", s.testUserID); err != nil {
			s.T().Logf("failed to clean up users: %v", err)
		}
		s.db.Close()
	}
	if s.redisClient != nil {
		s.redisClient.Close()
	}
}

// TestCompleteUserFlowE2E tests the complete user registration and notification flow
func (s *TelegramE2ETestSuite) TestCompleteUserFlowE2E() {
	t := s.T()

	// Step 1: Lookup user by chat ID (simulates /start command flow)
	t.Run("Step1_LookupUserByChatID", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+s.testChatID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		userData := resp["user"].(map[string]interface{})
		assert.Equal(t, s.testUserID, userData["id"])
		assert.Equal(t, "free", userData["subscription_tier"])
	})

	// Step 2: Check default notification preferences (simulates /settings command)
	t.Run("Step2_CheckDefaultPreferences", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+s.testUserID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, true, resp["enabled"])
		assert.Equal(t, 0.5, resp["profit_threshold"])
	})

	// Step 3: Disable notifications (simulates /stop command)
	t.Run("Step3_DisableNotifications", func(t *testing.T) {
		body := []byte(`{"enabled": false}`)
		req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+s.testUserID, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "success", resp["status"])
	})

	// Step 4: Verify notifications are disabled
	t.Run("Step4_VerifyNotificationsDisabled", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+s.testUserID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, false, resp["enabled"])
	})

	// Step 5: Re-enable notifications (simulates /resume command)
	t.Run("Step5_ReEnableNotifications", func(t *testing.T) {
		body := []byte(`{"enabled": true}`)
		req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+s.testUserID, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	// Step 6: Verify notifications are enabled again
	t.Run("Step6_VerifyNotificationsEnabled", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+s.testUserID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, true, resp["enabled"])
	})
}

// TestUserNotFoundE2E tests handling of non-existent users
func (s *TelegramE2ETestSuite) TestUserNotFoundE2E() {
	t := s.T()

	t.Run("LookupNonExistentUser", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/nonexistent_chat_id", nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "User not found", resp["error"])
	})
}

// TestAuthenticationE2E tests authentication requirements
func (s *TelegramE2ETestSuite) TestAuthenticationE2E() {
	t := s.T()

	t.Run("NoAPIKey", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+s.testChatID, nil)
		require.NoError(t, err)
		// No API key
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("InvalidAPIKey", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+s.testChatID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", "wrong-api-key")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("ValidAPIKey", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+s.testChatID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestConcurrentAccessE2E tests concurrent access to user data
func (s *TelegramE2ETestSuite) TestConcurrentAccessE2E() {
	t := s.T()

	t.Run("ConcurrentReads", func(t *testing.T) {
		// Perform 10 concurrent reads
		done := make(chan bool, 10)
		errors := make(chan error, 10)

		for i := 0; i < 10; i++ {
			go func() {
				req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+s.testChatID, nil)
				if err != nil {
					errors <- err
					done <- true
					return
				}
				req.Header.Set("X-API-Key", s.adminAPIKey)
				w := httptest.NewRecorder()
				s.router.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					errors <- fmt.Errorf("unexpected status: %d", w.Code)
				}
				done <- true
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Check for errors
		close(errors)
		for err := range errors {
			t.Errorf("Concurrent read error: %v", err)
		}
	})

	t.Run("ConcurrentToggleNotifications", func(t *testing.T) {
		// Perform alternating enable/disable operations
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			enabled := i%2 == 0
			go func(enable bool) {
				body := []byte(fmt.Sprintf(`{"enabled": %v}`, enable))
				req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+s.testUserID, bytes.NewBuffer(body))
				if err != nil {
					done <- true
					return
				}
				req.Header.Set("X-API-Key", s.adminAPIKey)
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.router.ServeHTTP(w, req)
				done <- true
			}(enabled)
		}

		// Wait for all goroutines
		for i := 0; i < 10; i++ {
			<-done
		}

		// Final state should be consistent (one of enabled or disabled)
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+s.testUserID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		// The final state is non-deterministic, so we just check that the key exists and is a boolean.
		assert.Contains(t, resp, "enabled")
		assert.IsType(t, true, resp["enabled"])
	})
}

// TestValidationE2E tests input validation
func (s *TelegramE2ETestSuite) TestValidationE2E() {
	t := s.T()

	t.Run("EmptyChatID", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/", nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		// Should return 404 or 400 for missing ID
		assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusBadRequest)
	})

	t.Run("InvalidJSONBody", func(t *testing.T) {
		body := []byte(`{invalid json}`)
		req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+s.testUserID, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestHealthEndpointE2E tests the health endpoint
func (s *TelegramE2ETestSuite) TestHealthEndpointE2E() {
	t := s.T()

	t.Run("HealthCheck", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/health", nil)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "healthy", resp["status"])
	})
}

// TestMultipleUsersFlowE2E tests handling of multiple users independently
func (s *TelegramE2ETestSuite) TestMultipleUsersFlowE2E() {
	t := s.T()

	// Create multiple test users
	type testUser struct {
		id     string
		chatID string
		email  string
		tier   string
	}

	users := []testUser{
		{id: uuid.New().String(), chatID: fmt.Sprintf("multi_user_1_%d", time.Now().UnixNano()), email: fmt.Sprintf("user1_%s@celebrum.ai", uuid.New().String()), tier: "free"},
		{id: uuid.New().String(), chatID: fmt.Sprintf("multi_user_2_%d", time.Now().UnixNano()), email: fmt.Sprintf("user2_%s@celebrum.ai", uuid.New().String()), tier: "premium"},
		{id: uuid.New().String(), chatID: fmt.Sprintf("multi_user_3_%d", time.Now().UnixNano()), email: fmt.Sprintf("user3_%s@celebrum.ai", uuid.New().String()), tier: "enterprise"},
	}

	// Insert test users
	for _, user := range users {
		_, err := s.db.Pool.Exec(context.Background(), `
			INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
			VALUES ($1, $2, 'hashed_password', $3, $4, NOW(), NOW())
		`, user.id, user.email, user.chatID, user.tier)
		require.NoError(t, err)
	}

	// Cleanup after test
	defer func() {
		for _, user := range users {
			_, _ = s.db.Pool.Exec(context.Background(), "DELETE FROM user_alerts WHERE user_id = $1", user.id)
			_, _ = s.db.Pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.id)
		}
	}()

	t.Run("IndependentUserLookups", func(t *testing.T) {
		for _, user := range users {
			req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+user.chatID, nil)
			require.NoError(t, err)
			req.Header.Set("X-API-Key", s.adminAPIKey)
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var resp map[string]interface{}
			err = json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			userData := resp["user"].(map[string]interface{})
			assert.Equal(t, user.id, userData["id"])
			assert.Equal(t, user.tier, userData["subscription_tier"])
		}
	})

	t.Run("ConcurrentUserOperations", func(t *testing.T) {
		done := make(chan bool, len(users)*2)
		errors := make(chan error, len(users)*2)

		// Concurrent reads for each user
		for _, user := range users {
			go func(u testUser) {
				req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+u.chatID, nil)
				if err != nil {
					errors <- err
					done <- true
					return
				}
				req.Header.Set("X-API-Key", s.adminAPIKey)
				w := httptest.NewRecorder()
				s.router.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					errors <- fmt.Errorf("user %s: unexpected status %d", u.chatID, w.Code)
				}
				done <- true
			}(user)

			// Also toggle notifications concurrently
			go func(u testUser) {
				body := []byte(`{"enabled": false}`)
				req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+u.id, bytes.NewBuffer(body))
				if err != nil {
					errors <- err
					done <- true
					return
				}
				req.Header.Set("X-API-Key", s.adminAPIKey)
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.router.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					errors <- fmt.Errorf("notification toggle for %s: unexpected status %d", u.id, w.Code)
				}
				done <- true
			}(user)
		}

		// Wait for all operations
		for i := 0; i < len(users)*2; i++ {
			<-done
		}

		close(errors)
		for err := range errors {
			t.Errorf("Concurrent operation error: %v", err)
		}
	})

	t.Run("IsolatedNotificationPreferences", func(t *testing.T) {
		// Disable notifications for user 1
		body := []byte(`{"enabled": false}`)
		req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+users[0].id, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Enable notifications for user 2
		body = []byte(`{"enabled": true}`)
		req, err = http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+users[1].id, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify user 1 has notifications disabled
		req, err = http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+users[0].id, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w = httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		var resp1 map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp1)
		require.NoError(t, err)
		assert.Equal(t, false, resp1["enabled"])

		// Verify user 2 has notifications enabled
		req, err = http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+users[1].id, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w = httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		var resp2 map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp2)
		require.NoError(t, err)
		assert.Equal(t, true, resp2["enabled"])
	})
}

// TestSubscriptionTierAccessE2E tests access based on subscription tiers
func (s *TelegramE2ETestSuite) TestSubscriptionTierAccessE2E() {
	t := s.T()

	tiers := []string{"free", "premium", "enterprise"}

	for _, tier := range tiers {
		t.Run("Tier_"+tier, func(t *testing.T) {
			// Create user with specific tier
			userID := uuid.New().String()
			chatID := fmt.Sprintf("tier_test_%s_%d", tier, time.Now().UnixNano())
			email := fmt.Sprintf("tier_%s_%s@celebrum.ai", tier, uuid.New().String())

			_, err := s.db.Pool.Exec(context.Background(), `
				INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
				VALUES ($1, $2, 'hashed_password', $3, $4, NOW(), NOW())
			`, userID, email, chatID, tier)
			require.NoError(t, err)

			// Cleanup after subtest
			defer func() {
				_, _ = s.db.Pool.Exec(context.Background(), "DELETE FROM user_alerts WHERE user_id = $1", userID)
				_, _ = s.db.Pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
			}()

			// Verify tier is returned correctly
			req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+chatID, nil)
			require.NoError(t, err)
			req.Header.Set("X-API-Key", s.adminAPIKey)
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var resp map[string]interface{}
			err = json.Unmarshal(w.Body.Bytes(), &resp)
			require.NoError(t, err)

			userData := resp["user"].(map[string]interface{})
			assert.Equal(t, tier, userData["subscription_tier"])
		})
	}
}

// TestUserRegistrationFlowE2E simulates the webhook-triggered user registration flow
func (s *TelegramE2ETestSuite) TestUserRegistrationFlowE2E() {
	t := s.T()

	// Simulate a new user coming from Telegram /start command
	newChatID := fmt.Sprintf("new_user_%d", time.Now().UnixNano())

	t.Run("NewUserNotFound", func(t *testing.T) {
		// First lookup should return 404
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+newChatID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	// Simulate user registration (normally done by Telegram service)
	newUserID := uuid.New().String()
	newEmail := fmt.Sprintf("newuser_%s@celebrum.ai", uuid.New().String())

	t.Run("RegisterNewUser", func(t *testing.T) {
		_, err := s.db.Pool.Exec(context.Background(), `
			INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
			VALUES ($1, $2, 'hashed_password', $3, 'free', NOW(), NOW())
		`, newUserID, newEmail, newChatID)
		require.NoError(t, err)
	})

	// Cleanup
	defer func() {
		_, _ = s.db.Pool.Exec(context.Background(), "DELETE FROM user_alerts WHERE user_id = $1", newUserID)
		_, _ = s.db.Pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", newUserID)
	}()

	t.Run("UserFoundAfterRegistration", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+newChatID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		userData := resp["user"].(map[string]interface{})
		assert.Equal(t, newUserID, userData["id"])
		assert.Equal(t, "free", userData["subscription_tier"]) // New users get free tier
	})

	t.Run("DefaultNotificationPreferencesForNewUser", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+newUserID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		// New users should have notifications enabled by default
		assert.Equal(t, true, resp["enabled"])
		assert.Equal(t, 0.5, resp["profit_threshold"]) // Default threshold
	})
}

// TestNotificationPreferencesPersistenceE2E tests that notification preferences persist correctly
func (s *TelegramE2ETestSuite) TestNotificationPreferencesPersistenceE2E() {
	t := s.T()

	t.Run("PersistDisabledState", func(t *testing.T) {
		// Disable notifications
		body := []byte(`{"enabled": false}`)
		req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+s.testUserID, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify persisted in database
		var count int
		err = s.db.Pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM user_alerts
			WHERE user_id = $1 AND alert_type = 'arbitrage' AND is_active = false
		`, s.testUserID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "Disabled record should exist in database")
	})

	t.Run("PersistEnabledState", func(t *testing.T) {
		// Re-enable notifications
		body := []byte(`{"enabled": true}`)
		req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+s.testUserID, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify disabled record removed from database
		var count int
		err = s.db.Pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM user_alerts
			WHERE user_id = $1 AND alert_type = 'arbitrage' AND is_active = false
		`, s.testUserID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "Disabled record should be removed from database")
	})
}

// TestRateLimitingE2E tests rate limiting behavior on endpoints
func (s *TelegramE2ETestSuite) TestRateLimitingE2E() {
	t := s.T()

	t.Run("RapidFireRequests", func(t *testing.T) {
		// Send multiple rapid requests
		successCount := 0
		for i := 0; i < 20; i++ {
			req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+s.testChatID, nil)
			require.NoError(t, err)
			req.Header.Set("X-API-Key", s.adminAPIKey)
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				successCount++
			}
		}
		// All requests should succeed (no rate limiting in current implementation)
		assert.Equal(t, 20, successCount)
	})
}

// TestErrorRecoveryE2E tests system behavior under error conditions
func (s *TelegramE2ETestSuite) TestErrorRecoveryE2E() {
	t := s.T()

	t.Run("RecoverFromBadRequest", func(t *testing.T) {
		// Send invalid request
		body := []byte(`{invalid}`)
		req, err := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+s.testUserID, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// System should still work normally after error
		req, err = http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+s.testUserID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w = httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("RecoverFromNotFoundError", func(t *testing.T) {
		// Try to access non-existent user
		req, err := http.NewRequest("GET", "/api/v1/telegram/internal/users/nonexistent", nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)

		// System should still work for valid requests
		req, err = http.NewRequest("GET", "/api/v1/telegram/internal/users/"+s.testChatID, nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", s.adminAPIKey)
		w = httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// Run the test suite
func TestTelegramE2ESuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test suite in short mode")
	}
	suite.Run(t, new(TelegramE2ETestSuite))
}
