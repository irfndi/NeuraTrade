package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// Package middleware provides HTTP middleware components for authentication,
// authorization, telemetry, and other cross-cutting concerns.

// AdminMiddleware provides admin authentication middleware.
type AdminMiddleware struct {
	apiKey string
}

// generateSecureKey generates a cryptographically secure random key.
func generateSecureKey(length int) string {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a less secure but functional key in edge cases
		log.Printf("WARNING: Failed to generate secure random key: %v", err)
		return "fallback-key-" + hex.EncodeToString([]byte(os.Getenv("HOSTNAME")))[:20]
	}
	return hex.EncodeToString(bytes)
}

// isProductionEnvironment checks if we're running in production.
func isProductionEnvironment() bool {
	env := strings.ToLower(os.Getenv("ENVIRONMENT"))
	ginMode := strings.ToLower(os.Getenv("GIN_MODE"))
	return env == "production" || env == "prod" || ginMode == "release"
}

// NewAdminMiddleware creates a new admin authentication middleware.
// It retrieves the API key from the environment.
// In non-production environments, it will generate a temporary key if not set.
//
// Returns:
//
//	*AdminMiddleware: Initialized middleware.
func NewAdminMiddleware() *AdminMiddleware {
	// Get admin API key from environment variable
	apiKey := os.Getenv("ADMIN_API_KEY")

	// Handle missing API key based on environment
	if apiKey == "" {
		if isProductionEnvironment() {
			log.Fatal("ADMIN_API_KEY environment variable must be set in production")
		}
		// Generate temporary key for non-production environments
		apiKey = generateSecureKey(32)
		log.Println("INFO: Generated temporary admin key for non-production environment")
	}

	// Prevent use of default/example keys in any environment
	// #nosec G101 -- these are explicit denylisted example values, not embedded credentials
	if apiKey == "admin-dev-key-change-in-production" || apiKey == "admin-secret-key-change-me" {
		if isProductionEnvironment() {
			log.Fatal("ADMIN_API_KEY cannot use default/example values in production")
		}
		log.Printf("WARNING: Using example ADMIN_API_KEY in non-production environment")
	}

	// Ensure minimum security requirements
	if len(apiKey) < 32 {
		if isProductionEnvironment() {
			log.Fatal("ADMIN_API_KEY must be at least 32 characters long for security in production")
		}
		// Pad short keys in non-production
		log.Printf("WARNING: ADMIN_API_KEY is shorter than 32 characters in non-production environment")
	}

	return &AdminMiddleware{
		apiKey: apiKey,
	}
}

// RequireAdminAuth middleware validates admin API keys.
// It checks Authorization and X-API-Key headers.
// Uses constant-time comparison to prevent timing attacks.
//
// Returns:
//
//	gin.HandlerFunc: Gin handler.
func (am *AdminMiddleware) RequireAdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestPath := c.Request.URL.Path

		// Check for API key in Authorization header (Bearer token)
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			tokenParts := strings.Split(authHeader, " ")
			if len(tokenParts) == 2 && tokenParts[0] == "Bearer" {
				// Use constant-time comparison to prevent timing attacks
				if subtle.ConstantTimeCompare([]byte(tokenParts[1]), []byte(am.apiKey)) == 1 {
					c.Next()
					return
				}
				// Log invalid Bearer token (without exposing actual keys)
				log.Printf("WARN: Admin auth failed for %s - invalid Bearer token", requestPath)
			}
		}

		// Check for API key in X-API-Key header
		apiKeyHeader := c.GetHeader("X-API-Key")
		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(apiKeyHeader), []byte(am.apiKey)) == 1 {
			c.Next()
			return
		}

		// Log authentication failure with helpful debugging info
		if apiKeyHeader == "" {
			log.Printf("WARN: Admin auth failed for %s - no X-API-Key header provided", requestPath)
		} else {
			log.Printf("WARN: Admin auth failed for %s - X-API-Key mismatch", requestPath)
		}

		// Query parameter authentication removed for security reasons
		// API keys should only be passed via headers

		// No valid API key found
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "Unauthorized",
			"message": "Valid admin API key required for this endpoint",
			"code":    "ADMIN_AUTH_FAILED",
		})
		c.Abort()
	}
}

// ValidateAdminKey validates an admin API key.
//
// Parameters:
//
//	key: API key to check.
//
// Returns:
//
//	bool: True if valid.
func (am *AdminMiddleware) ValidateAdminKey(key string) bool {
	return key == am.apiKey
}
