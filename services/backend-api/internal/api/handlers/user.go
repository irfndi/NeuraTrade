package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// UserHandler manages user-related API endpoints.
type UserHandler struct {
	db      DBQuerier
	redis   *redis.Client
	querier DBQuerier // For testing with mocks
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
//
// Returns:
//
//	*UserHandler: Initialized handler.
func NewUserHandler(db DBQuerier, redisClient *redis.Client) *UserHandler {
	return &UserHandler{
		db:    db,
		redis: redisClient,
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
//
// Returns:
//
//	*UserHandler: Initialized handler.
func NewUserHandlerWithQuerier(querier any, redisClient *redis.Client) *UserHandler {
	resolvedQuerier, err := resolveDBQuerier(querier)
	if err != nil {
		panic(err)
	}

	return &UserHandler{
		db:      resolvedQuerier,
		redis:   redisClient,
		querier: resolvedQuerier,
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

	// Check if user already exists
	exists, err := h.userExists(c.Request.Context(), req.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check user existence"})
		return
	}
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
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
	`

	var err2 error
	if h.querier != nil {
		_, err2 = h.querier.Exec(c.Request.Context(), query,
			userID, req.Email, string(hashedPassword), req.TelegramChatID, "free")
	} else if h.db != nil {
		_, err2 = h.db.Exec(c.Request.Context(), query,
			userID, req.Email, string(hashedPassword), req.TelegramChatID, "free")
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database not available"})
		return
	}
	err = err2

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

	// Generate JWT token (simplified - in production use proper JWT library)
	token := h.generateSimpleToken(user.ID)

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
	// In a real implementation, you would extract user ID from JWT token
	// For now, we'll use a query parameter or header
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		userID = c.Query("user_id")
	}

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
	userID := c.GetHeader("X-User-ID")
	if userID == "" {
		userID = c.Query("user_id")
	}

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID required"})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		log.Printf("Warning: Could not get old user data for cache invalidation: %v", err)
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

	// Return false if database is not available
	if h.db == nil {
		return false, fmt.Errorf("database not available")
	}

	var count int
	query := "SELECT COUNT(*) FROM users WHERE email = $1"
	err := h.db.QueryRow(ctx, query, email).Scan(&count)
	if err != nil {
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
				log.Printf("Failed to unmarshal cached user %s: %v", userID, unmarshalErr)
			}
		} else if err != redis.Nil {
			// Log non-cache-miss Redis errors
			log.Printf("Redis error getting user %s: %v", userID, err)
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
				log.Printf("Failed to cache user %s: %v", userID, err)
			}
		} else {
			log.Printf("Failed to marshal user %s for caching: %v", userID, err)
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
		log.Printf("Failed to invalidate user cache for ID %s: %v", userID, err)
	} else {
		log.Printf("Invalidated user cache for ID %s", userID)
	}

	// Invalidate old telegram chat ID cache if it existed
	if oldUser != nil && oldUser.TelegramChatID != nil {
		oldTelegramKey := fmt.Sprintf("user:telegram:%s", *oldUser.TelegramChatID)
		if err := h.redis.Del(ctx, oldTelegramKey).Err(); err != nil {
			log.Printf("Failed to invalidate old telegram cache for chat ID %s: %v", *oldUser.TelegramChatID, err)
		} else {
			log.Printf("Invalidated old telegram cache for chat ID %s", *oldUser.TelegramChatID)
		}
	}

	// Invalidate new telegram chat ID cache if it's being set
	if newTelegramChatID != nil {
		newTelegramKey := fmt.Sprintf("user:telegram:%s", *newTelegramChatID)
		if err := h.redis.Del(ctx, newTelegramKey).Err(); err != nil {
			log.Printf("Failed to invalidate new telegram cache for chat ID %s: %v", *newTelegramChatID, err)
		} else {
			log.Printf("Invalidated new telegram cache for chat ID %s", *newTelegramChatID)
		}
	}
}

// generateSimpleToken creates a simple token (in production, use proper JWT)
func (h *UserHandler) generateSimpleToken(userID string) string {
	// This is a simplified token generation
	// In production, use a proper JWT library like github.com/golang-jwt/jwt
	return fmt.Sprintf("token_%s_%d", userID, time.Now().Unix())
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
				log.Printf("Failed to unmarshal cached user for chat %s: %v", chatID, unmarshalErr)
			}
		} else if err != redis.Nil {
			// Log non-cache-miss Redis errors
			log.Printf("Redis error getting user for chat %s: %v", chatID, err)
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
		FROM users WHERE telegram_chat_id = $1
	`

	err := h.db.QueryRow(ctx, query, chatID).Scan(
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
				log.Printf("Failed to cache user for chat %s: %v", chatID, err)
			}
		} else {
			log.Printf("Failed to marshal user for chat %s for caching: %v", chatID, err)
		}
	}

	return &user, nil
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
