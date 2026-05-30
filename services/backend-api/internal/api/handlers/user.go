package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"net/http"
	"strings"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// TokenGenerator interface for generating JWT tokens
type TokenGenerator interface {
	GenerateToken(userID, email string, duration time.Duration) (string, error)
}

// UserHandler manages user-related API endpoints.
type UserHandler struct {
	db       DBQuerier
	redis    *redis.Client
	querier  DBQuerier // For testing with mocks
	tokenGen TokenGenerator
}

// RegisterRequest represents the user registration request body.
type RegisterRequest struct {
	// Email is the user's email address.
	Email string `json:"email" binding:"required,email"`
	// Password is the user's password.
	Password string `json:"password" binding:"required,min=8"`
	// TelegramChatID is the optional Telegram chat ID.
	TelegramChatID *string `json:"telegram_chat_id,omitempty"`
}

// LoginRequest represents the user login request body.
type LoginRequest struct {
	// Email is the user's email address.
	Email string `json:"email" binding:"required,email"`
	// Password is the user's password.
	Password string `json:"password" binding:"required"`
}

// UserResponse represents the user data in API responses.
type UserResponse struct {
	// ID is the unique user identifier.
	ID string `json:"id"`
	// Email is the user's email address.
	Email string `json:"email"`
	// TelegramChatID is the linked Telegram chat ID.
	TelegramChatID *string `json:"telegram_chat_id,omitempty"`
	// SubscriptionTier is the user's subscription level (e.g., "free", "premium").
	SubscriptionTier string `json:"subscription_tier"`
	// CreatedAt is the account creation time.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is the last update time.
	UpdatedAt time.Time `json:"updated_at"`
}

// LoginResponse represents the response for a successful login.
type LoginResponse struct {
	// User contains the user details.
	User UserResponse `json:"user"`
	// Token is the authentication token.
	Token string `json:"token"`
}

// UpdateProfileRequest represents the user profile update request body.
type UpdateProfileRequest struct {
	// TelegramChatID is the new Telegram chat ID.
	TelegramChatID *string `json:"telegram_chat_id,omitempty"`
}

// NewUserHandler creates a new instance of UserHandler.
//
// Parameters:
//
//	db: Database connection.
//	redisClient: Redis client.
//	tokenGen: Token generator for JWT tokens.
//
// Returns:
//
//	*UserHandler: Initialized handler.
func NewUserHandler(db DBQuerier, redisClient *redis.Client, tokenGen ...TokenGenerator) *UserHandler {
	var resolvedTokenGen TokenGenerator
	if len(tokenGen) > 0 {
		resolvedTokenGen = tokenGen[0]
	}

	return &UserHandler{
		db:       db,
		redis:    redisClient,
		tokenGen: resolvedTokenGen,
	}
}

type DBQuerier interface {
	Query(ctx context.Context, sql string, args ...interface{}) (database.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) database.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (database.Result, error)
}

// NewUserHandlerWithQuerier creates a UserHandler with a custom querier for testing.
//
// Parameters:
//
//	querier: The database querier interface.
//	redisClient: Redis client.
//	tokenGen: Token generator for JWT tokens.
//
// Returns:
//
//	*UserHandler: Initialized handler.
func NewUserHandlerWithQuerier(querier any, redisClient *redis.Client, tokenGen ...TokenGenerator) *UserHandler {
	resolvedQuerier, err := resolveDBQuerier(querier)
	if err != nil {
		panic(err)
	}

	var resolvedTokenGen TokenGenerator
	if len(tokenGen) > 0 {
		resolvedTokenGen = tokenGen[0]
	}

	return &UserHandler{
		db:       resolvedQuerier,
		redis:    redisClient,
		querier:  resolvedQuerier,
		tokenGen: resolvedTokenGen,
	}
}

// RegisterUser handles user registration.
// It creates a new user account if the email is not already taken.
//
// Parameters:
//
//	c: Gin context.
func (h *UserHandler) RegisterUser(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate required fields
	if req.Email == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email and password are required"})
		return
	}

	// Legacy SQLite deployments may still have users(telegram_id, risk_level, created_at)
	// without email/password columns. Keep Telegram onboarding working in that mode.
	if h.querier == nil && h.db != nil && h.usesLegacyUsersSchema(c.Request.Context()) {
		h.registerLegacyTelegramUser(c, req)
		return
	}

	// Check if user already exists
	exists, err := h.userExists(c.Request.Context(), req.Email)
	if err != nil {
		if h.db != nil && req.TelegramChatID != nil && strings.TrimSpace(*req.TelegramChatID) != "" {
			h.registerLegacyTelegramUser(c, req)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check user existence"})
		return
	}

	// For telegram users: if user exists, update telegram_chat_id if different
	if exists && req.TelegramChatID != nil && *req.TelegramChatID != "" {
		existingUser, err := h.getUserByEmail(c.Request.Context(), req.Email)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch existing user"})
			return
		}

		// If telegram_chat_id is different or NULL, update it
		if existingUser.TelegramChatID == nil || *existingUser.TelegramChatID != *req.TelegramChatID {
			var updateErr error
			if h.querier != nil {
				updateQuery := `UPDATE users SET telegram_chat_id = $1, updated_at = NOW() WHERE email = $2`
				_, updateErr = h.querier.Exec(c.Request.Context(), updateQuery, req.TelegramChatID, req.Email)
			} else if h.db != nil {
				updateQuery := `UPDATE users SET telegram_chat_id = ?, updated_at = CURRENT_TIMESTAMP WHERE email = ?`
				_, updateErr = h.db.Exec(c.Request.Context(), updateQuery, req.TelegramChatID, req.Email)
			}
			if updateErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update telegram chat ID"})
				return
			}
			// Invalidate cache for old and new chat IDs
			if h.redis != nil {
				if existingUser.TelegramChatID != nil {
					h.redis.Del(c.Request.Context(), fmt.Sprintf("user:telegram:%s", *existingUser.TelegramChatID))
				}
				h.redis.Del(c.Request.Context(), fmt.Sprintf("user:telegram:%s", *req.TelegramChatID))
			}
		}

		// Return existing user (updated with new telegram_chat_id)
		userResponse := UserResponse{
			ID:               existingUser.ID,
			Email:            existingUser.Email,
			TelegramChatID:   req.TelegramChatID,
			SubscriptionTier: existingUser.SubscriptionTier,
			CreatedAt:        existingUser.CreatedAt,
			UpdatedAt:        time.Now(),
		}
		c.JSON(http.StatusOK, gin.H{"user": userResponse, "already_exists": true})
		return
	}

	// For non-telegram users: if exists, return conflict
	if exists {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Generate user ID
	userID := uuid.New().String()

	// Insert user into database
	query := `
		INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`

	var err2 error
	dbAvailable := false
	if h.querier != nil {
		_, err2 = h.querier.Exec(c.Request.Context(), query,
			userID, req.Email, string(hashedPassword), req.TelegramChatID, "free")
		dbAvailable = true
	} else if h.db != nil {
		_, err2 = h.db.Exec(c.Request.Context(), query,
			userID, req.Email, string(hashedPassword), req.TelegramChatID, "free")
		dbAvailable = true
	}

	// Handle database errors
	if err2 != nil {
		if dbAvailable {
			// Database is available but query failed - return error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
		// Database not available (dev mode) - log warning and continue
		zaplogrus.Warnf("[WARN] User registration proceeded without database persistence: %v", err2)
	}

	// Return user response (without password)
	now := time.Now()
	userResponse := UserResponse{
		ID:               userID,
		Email:            req.Email,
		TelegramChatID:   req.TelegramChatID,
		SubscriptionTier: "free",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	c.JSON(http.StatusCreated, gin.H{"user": userResponse})
}

// LoginUser handles user authentication.
// It verifies credentials and returns a JWT token.
//
// Parameters:
//
//	c: Gin context.
func (h *UserHandler) LoginUser(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Get user from database
	user, err := h.getUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Generate JWT token
	var token string
	if h.tokenGen != nil {
		var err error
		token, err = h.tokenGen.GenerateToken(user.ID, user.Email, 24*time.Hour)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
			return
		}
	} else {
		// Fallback should only happen in tests if tokenGen is not provided
		// In production, tokenGen must be provided
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generator not configured"})
		return
	}

	userResponse := UserResponse{
		ID:               user.ID,
		Email:            user.Email,
		TelegramChatID:   user.TelegramChatID,
		SubscriptionTier: user.SubscriptionTier,
		CreatedAt:        user.CreatedAt,
		UpdatedAt:        user.UpdatedAt,
	}

	response := LoginResponse{
		User:  userResponse,
		Token: token,
	}

	c.JSON(http.StatusOK, response)
}

// GetUserProfile returns the current user's profile.
// It retrieves user details based on the authenticated user ID.
//
// Parameters:
//
//	c: Gin context.
func (h *UserHandler) GetUserProfile(c *gin.Context) {
	// Extract user ID from JWT token context (set by AuthMiddleware)
	userID := c.GetString("user_id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID required"})
		return
	}

	user, err := h.getUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	userResponse := UserResponse{
		ID:               user.ID,
		Email:            user.Email,
		TelegramChatID:   user.TelegramChatID,
		SubscriptionTier: user.SubscriptionTier,
		CreatedAt:        user.CreatedAt,
		UpdatedAt:        user.UpdatedAt,
	}

	c.JSON(http.StatusOK, gin.H{"user": userResponse})
}

// UpdateUserProfile updates user profile information.
// It allows updating optional fields like Telegram Chat ID.
//
// Parameters:
//
//	c: Gin context.
func (h *UserHandler) UpdateUserProfile(c *gin.Context) {
	// Extract user ID from JWT token context (set by AuthMiddleware)
	userID := c.GetString("user_id")

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID required"})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Update user profile
	query := `
		UPDATE users
		SET telegram_chat_id = $2, updated_at = $3
		WHERE id = $1
	`

	// Get the old user data for cache invalidation
	oldUser, err := h.getUserByID(c.Request.Context(), userID)
	if err != nil {
		zaplogrus.Warnf("Warning: Could not get old user data for cache invalidation: %v", err)
	}

	var execErr error
	// Use querier if available (for testing), otherwise use database
	if h.querier != nil {
		_, execErr = h.querier.Exec(c.Request.Context(), query,
			userID, req.TelegramChatID, time.Now())
	} else if h.db != nil {
		_, execErr = h.db.Exec(c.Request.Context(), query,
			userID, req.TelegramChatID, time.Now())
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database not available"})
		return
	}

	if execErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		return
	}

	// Invalidate cache after successful update
	h.invalidateUserCache(c.Request.Context(), userID, oldUser, req.TelegramChatID)

	// Return updated user
	updatedUser, err := h.getUserByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated user"})
		return
	}

	userResponse := UserResponse{
		ID:               updatedUser.ID,
		Email:            updatedUser.Email,
		TelegramChatID:   updatedUser.TelegramChatID,
		SubscriptionTier: updatedUser.SubscriptionTier,
		CreatedAt:        updatedUser.CreatedAt,
		UpdatedAt:        updatedUser.UpdatedAt,
	}

	c.JSON(http.StatusOK, gin.H{"user": userResponse})
}

// Helper functions

func (h *UserHandler) userExists(ctx context.Context, email string) (bool, error) {
	// Use querier if available (for testing), otherwise use database
	if h.querier != nil {
		var count int
		query := "SELECT COUNT(*) FROM users WHERE email = $1"
		err := h.querier.QueryRow(ctx, query, email).Scan(&count)
		if err != nil {
			return false, err
		}
		return count > 0, nil
	}

	// Return error if database is not available
	if h.db == nil {
		return false, fmt.Errorf("database not available")
	}

	var count int
	query := "SELECT COUNT(*) FROM users WHERE email = $1"
	err := h.db.QueryRow(ctx, query, email).Scan(&count)
	if err != nil {
		zaplogrus.Warnf("[USER] Failed to check existing user by email %q: %v", email, err)
		return false, err
	}
	return count > 0, nil
}

func (h *UserHandler) getUserByEmail(ctx context.Context, email string) (*models.User, error) {
	// Use querier if available (for testing), otherwise use database
	if h.querier != nil {
		var user models.User
		query := `
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE email = $1
		`

		err := h.querier.QueryRow(ctx, query, email).Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.TelegramChatID,
			&user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		return &user, nil
	}

	// Return error if database is not available
	if h.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	var user models.User
	query := `
		SELECT id, email, password_hash, telegram_chat_id,
		       subscription_tier, created_at, updated_at
		FROM users WHERE email = $1
	`

	err := h.db.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.TelegramChatID,
		&user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (h *UserHandler) getUserByID(ctx context.Context, userID string) (*models.User, error) {
	// Use querier if available (for testing), otherwise use database
	if h.querier != nil {
		var user models.User
		query := `
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE id = $1
		`

		err := h.querier.QueryRow(ctx, query, userID).Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.TelegramChatID,
			&user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		return &user, nil
	}

	cacheKey := fmt.Sprintf("user:id:%s", userID)

	// Try to get from Redis cache first
	if h.redis != nil {
		cachedData, err := h.redis.Get(ctx, cacheKey).Result()
		if err == nil {
			var user models.User
			if unmarshalErr := json.Unmarshal([]byte(cachedData), &user); unmarshalErr == nil {
				return &user, nil
			} else {
				zaplogrus.Warnf("Failed to unmarshal cached user %s: %v", userID, unmarshalErr)
			}
		} else if err != redis.Nil {
			// Log non-cache-miss Redis errors
			zaplogrus.Infof("Redis error getting user %s: %v", userID, err)
		}
	}

	// Cache miss or Redis unavailable, check database availability
	if h.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Query database
	var user models.User
	query := `
		SELECT id, email, password_hash, telegram_chat_id,
		       subscription_tier, created_at, updated_at
		FROM users WHERE id = $1
	`

	err := h.db.QueryRow(ctx, query, userID).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.TelegramChatID,
		&user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// Cache the result in Redis with 5-minute TTL
	if h.redis != nil {
		userJSON, err := json.Marshal(user)
		if err == nil {
			if err := h.redis.Set(ctx, cacheKey, string(userJSON), 5*time.Minute).Err(); err != nil {
				zaplogrus.Warnf("Failed to cache user %s: %v", userID, err)
			}
		} else {
			zaplogrus.Warnf("Failed to marshal user %s for caching: %v", userID, err)
		}
	}

	return &user, nil
}

// invalidateUserCache removes cached user data after profile updates
func (h *UserHandler) invalidateUserCache(ctx context.Context, userID string, oldUser *models.User, newTelegramChatID *string) {
	if h.redis == nil {
		return
	}

	// Invalidate user ID cache
	userCacheKey := fmt.Sprintf("user:id:%s", userID)
	if err := h.redis.Del(ctx, userCacheKey).Err(); err != nil {
		zaplogrus.Warnf("Failed to invalidate user cache for ID %s: %v", userID, err)
	} else {
		zaplogrus.Warnf("Invalidated user cache for ID %s", userID)
	}

	// Invalidate old telegram chat ID cache if it existed
	if oldUser != nil && oldUser.TelegramChatID != nil {
		oldTelegramKey := fmt.Sprintf("user:telegram:%s", *oldUser.TelegramChatID)
		if err := h.redis.Del(ctx, oldTelegramKey).Err(); err != nil {
			zaplogrus.Warnf("Failed to invalidate old telegram cache for chat ID %s: %v", *oldUser.TelegramChatID, err)
		} else {
			zaplogrus.Warnf("Invalidated old telegram cache for chat ID %s", *oldUser.TelegramChatID)
		}
	}

	// Invalidate new telegram chat ID cache if it's being set
	if newTelegramChatID != nil {
		newTelegramKey := fmt.Sprintf("user:telegram:%s", *newTelegramChatID)
		if err := h.redis.Del(ctx, newTelegramKey).Err(); err != nil {
			zaplogrus.Warnf("Failed to invalidate new telegram cache for chat ID %s: %v", *newTelegramChatID, err)
		} else {
			zaplogrus.Warnf("Invalidated new telegram cache for chat ID %s", *newTelegramChatID)
		}
	}
}

// GetUserByTelegramChatID retrieves user by Telegram chat ID (for Telegram bot) with Redis caching.
//
// Parameters:
//
//	ctx: Context.
//	chatID: Telegram chat ID.
//
// Returns:
//
//	*models.User: User if found.
//	error: Error if lookup fails.
func (h *UserHandler) GetUserByTelegramChatID(ctx context.Context, chatID string) (*models.User, error) {
	// Use querier if available (for testing), otherwise use database
	if h.querier != nil {
		var user models.User
		query := `
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE telegram_chat_id = $1
		`

		err := h.querier.QueryRow(ctx, query, chatID).Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.TelegramChatID,
			&user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt,
		)

		if err != nil {
			return nil, err
		}

		return &user, nil
	}

	cacheKey := fmt.Sprintf("user:telegram:%s", chatID)

	// Try to get from Redis cache first
	if h.redis != nil {
		cachedData, err := h.redis.Get(ctx, cacheKey).Result()
		if err == nil {
			var user models.User
			if unmarshalErr := json.Unmarshal([]byte(cachedData), &user); unmarshalErr == nil {
				return &user, nil
			} else {
				zaplogrus.Warnf("Failed to unmarshal cached user for chat %s: %v", chatID, unmarshalErr)
			}
		} else if err != redis.Nil {
			// Log non-cache-miss Redis errors
			zaplogrus.Infof("Redis error getting user for chat %s: %v", chatID, err)
		}
	}

	// Cache miss or Redis unavailable, query database
	if h.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	var user models.User
	query := `
		SELECT id, email, password_hash, telegram_chat_id,
		       subscription_tier, created_at, updated_at
		FROM users WHERE telegram_chat_id = $1
	`

	err := h.db.QueryRow(ctx, query, chatID).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.TelegramChatID,
		&user.SubscriptionTier, &user.CreatedAt, &user.UpdatedAt,
	)

	if err != nil {
		if legacyUser, legacyErr := h.getLegacyUserByTelegramID(ctx, chatID); legacyErr == nil {
			return legacyUser, nil
		}
		if h.usesLegacyUsersSchema(ctx) {
			return h.getLegacyUserByTelegramID(ctx, chatID)
		}
		return nil, err
	}

	// Cache the result in Redis with 5-minute TTL
	if h.redis != nil {
		userJSON, err := json.Marshal(user)
		if err == nil {
			if err := h.redis.Set(ctx, cacheKey, string(userJSON), 5*time.Minute).Err(); err != nil {
				zaplogrus.Warnf("Failed to cache user for chat %s: %v", chatID, err)
			}
		} else {
			zaplogrus.Warnf("Failed to marshal user for chat %s for caching: %v", chatID, err)
		}
	}

	return &user, nil
}

func (h *UserHandler) usesLegacyUsersSchema(ctx context.Context) bool {
	if h.db == nil {
		return false
	}

	rows, err := h.db.Query(ctx, "PRAGMA table_info(users)")
	if err != nil {
		return false
	}
	defer rows.Close()

	hasTelegramID := false
	hasTelegramChatID := false

	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notNull   int
			defaultV  sql.NullString
			primaryID int
		)
		if scanErr := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryID); scanErr != nil {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "telegram_id":
			hasTelegramID = true
		case "telegram_chat_id":
			hasTelegramChatID = true
		}
	}

	return hasTelegramID && !hasTelegramChatID
}

func (h *UserHandler) registerLegacyTelegramUser(c *gin.Context, req RegisterRequest) {
	if req.TelegramChatID == nil || strings.TrimSpace(*req.TelegramChatID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "telegram_chat_id is required for legacy registration"})
		return
	}

	chatID := strings.TrimSpace(*req.TelegramChatID)
	var existingID int64
	var createdAt time.Time
	err := h.db.QueryRow(c.Request.Context(),
		"SELECT id, created_at FROM users WHERE telegram_id = ?",
		chatID,
	).Scan(&existingID, &createdAt)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{
			"user": UserResponse{
				ID:               fmt.Sprintf("%d", existingID),
				Email:            req.Email,
				TelegramChatID:   &chatID,
				SubscriptionTier: "free",
				CreatedAt:        createdAt,
				UpdatedAt:        createdAt,
			},
			"already_exists": true,
		})
		return
	}
	if !isNoRowsLookupError(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check user existence"})
		return
	}

	if _, err := h.db.Exec(c.Request.Context(),
		`INSERT INTO users (telegram_id, risk_level, created_at) VALUES (?, 'medium', CURRENT_TIMESTAMP)`,
		chatID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	var newID int64
	if err := h.db.QueryRow(c.Request.Context(),
		"SELECT id, created_at FROM users WHERE telegram_id = ?",
		chatID,
	).Scan(&newID, &createdAt); err != nil {
		c.JSON(http.StatusCreated, gin.H{
			"user": UserResponse{
				ID:               "0",
				Email:            req.Email,
				TelegramChatID:   &chatID,
				SubscriptionTier: "free",
				CreatedAt:        time.Now(),
				UpdatedAt:        time.Now(),
			},
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user": UserResponse{
			ID:               fmt.Sprintf("%d", newID),
			Email:            req.Email,
			TelegramChatID:   &chatID,
			SubscriptionTier: "free",
			CreatedAt:        createdAt,
			UpdatedAt:        createdAt,
		},
	})
}

func (h *UserHandler) getLegacyUserByTelegramID(ctx context.Context, chatID string) (*models.User, error) {
	var (
		id        int64
		telegram  string
		riskLevel string
		createdAt time.Time
	)

	err := h.db.QueryRow(ctx,
		"SELECT id, telegram_id, risk_level, created_at FROM users WHERE telegram_id = ?",
		chatID,
	).Scan(&id, &telegram, &riskLevel, &createdAt)
	if err != nil {
		return nil, err
	}

	email := fmt.Sprintf("telegram_%s@neuratrade.ai", telegram)
	return &models.User{
		ID:               fmt.Sprintf("%d", id),
		Email:            email,
		PasswordHash:     "",
		TelegramChatID:   &telegram,
		SubscriptionTier: "free",
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}, nil
}

func isNoRowsLookupError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "no rows")
}

// CreateTelegramUser creates a new user from Telegram registration.
//
// Parameters:
//
//	ctx: Context.
//	chatID: Telegram chat ID.
//	username: Telegram username.
//
// Returns:
//
//	*models.User: Created user.
//	error: Error if creation fails.
func (h *UserHandler) CreateTelegramUser(ctx context.Context, chatID string, username string) (*models.User, error) {
	// Validate chat ID
	if chatID == "" {
		return nil, fmt.Errorf("telegram chat ID cannot be empty")
	}

	userID := uuid.New().String()
	now := time.Now()

	// Create a temporary email based on Telegram username or chat ID
	email := fmt.Sprintf("telegram_%s@neuratrade.ai", chatID)
	if username != "" {
		email = fmt.Sprintf("telegram_%s@neuratrade.ai", username)
	}

	query := `
		INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	// Use querier if available (for testing), otherwise use database
	if h.querier != nil {
		_, err := h.querier.Exec(ctx, query, userID, email, "", chatID, "free", now, now)
		if err != nil {
			return nil, err
		}
	} else {
		// Return error if database is not available
		if h.db == nil {
			return nil, fmt.Errorf("database not available")
		}

		_, err := h.db.Exec(ctx, query, userID, email, "", chatID, "free", now, now)
		if err != nil {
			return nil, err
		}
	}

	// Return the created user
	return &models.User{
		ID:               userID,
		Email:            email,
		PasswordHash:     "",
		TelegramChatID:   &chatID,
		SubscriptionTier: "free",
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}
