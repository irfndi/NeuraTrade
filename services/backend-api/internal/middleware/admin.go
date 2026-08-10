package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
)

// Package middleware provides HTTP middleware components for authentication,
// authorization, telemetry, and other cross-cutting concerns.

// AdminMiddleware provides admin authentication middleware.
type AdminMiddleware struct {
	apiKey string
}

// APIKey returns the admin API key bytes. It is exposed so that other
// middlewares (e.g. AuthMiddleware.RequireAuthOrAdmin) can accept the
// admin key as an alternative auth method on research/operator routes
// without having to construct a separate AdminMiddleware.
func (am *AdminMiddleware) APIKey() string {
	if am == nil {
		return ""
	}
	return am.apiKey
}

// generateSecureKey generates a cryptographically secure random key.
// It returns an error on failure so callers can fail closed instead of
// falling back to a predictable key derived from public metadata.
func generateSecureKey(length int) (string, error) {
	bytes := make([]byte, length/2)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate secure random key: %w", err)
	}
	return hex.EncodeToString(bytes), nil
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

// getAdminAPIKeyFromConfig reads admin API key from config.json
func getAdminAPIKeyFromConfig() string {
	configPath := strings.TrimSpace(os.Getenv("NEURATRADE_HOME"))
	if configPath != "" {
		configPath = filepath.Join(configPath, "config.json")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configPath = filepath.Join(homeDir, ".neuratrade", "config.json")
	}

	// #nosec G304,G703 -- config path is derived from NEURATRADE_HOME or user home
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}

	if apiKey, ok := config["admin_api_key"].(string); ok {
		return strings.TrimSpace(apiKey)
	}
	if security, ok := config["security"].(map[string]interface{}); ok {
		if apiKey, ok := security["admin_api_key"].(string); ok {
			return strings.TrimSpace(apiKey)
		}
	}

	return ""
}

func NewAdminMiddleware() (*AdminMiddleware, error) {
	// Prefer explicit environment override first for tests/ops, then config.json fallback.
	apiKey := strings.TrimSpace(os.Getenv("ADMIN_API_KEY"))
	if apiKey == "" {
		apiKey = getAdminAPIKeyFromConfig()
	}

	// Handle missing API key based on environment
	if apiKey == "" {
		if isProductionEnvironment() {
			return nil, fmt.Errorf("ADMIN_API_KEY must be set in config.json or environment variable in production")
		}
		// Generate temporary key for non-production environments. Fail
		// startup (fail-closed) if secure randomness is unavailable rather
		// than deriving a predictable key from hostname or other metadata.
		generated, genErr := generateSecureKey(32)
		if genErr != nil {
			return nil, fmt.Errorf("generate temporary admin key: %w", genErr)
		}
		apiKey = generated
		zaplogrus.Info("Generated temporary admin key for non-production environment")
	}

	// Deny default/example keys in ANY environment: an operator who
	// configures one of these well-known values must be stopped regardless
	// of environment so the footgun cannot reach production unnoticed.
	// #nosec G101 -- these are explicit denylisted example values, not embedded credentials
	for _, exampleKey := range []string{
		"admin-dev-key-change-in-production",
		"admin-secret-key-change-me",
		"change-me-in-production",
	} {
		if strings.EqualFold(strings.TrimSpace(apiKey), exampleKey) {
			return nil, fmt.Errorf("ADMIN_API_KEY cannot use the default/example value %q in any environment", exampleKey)
		}
	}

	// Ensure minimum security requirements
	if len(apiKey) < 32 {
		if isProductionEnvironment() {
			return nil, fmt.Errorf("ADMIN_API_KEY must be at least 32 characters long for security in production")
		}
		// Pad short keys in non-production
		zaplogrus.Warn("ADMIN_API_KEY is shorter than 32 characters in non-production environment")
	}

	return &AdminMiddleware{
		apiKey: apiKey,
	}, nil
}

// MustNewAdminMiddleware is like NewAdminMiddleware but panics on error.
func MustNewAdminMiddleware() *AdminMiddleware {
	am, err := NewAdminMiddleware()
	if err != nil {
		panic(err)
	}
	return am
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
				zaplogrus.Warnf("Admin auth failed for %s - invalid Bearer token", requestPath)
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
			zaplogrus.Warnf("Admin auth failed for %s - no X-API-Key header provided", requestPath)
		} else {
			zaplogrus.Warnf("Admin auth failed for %s - X-API-Key mismatch", requestPath)
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
	if am == nil {
		return false
	}
	// Constant-time comparison, matching the guarantee used by
	// RequireAdminAuth, to avoid leaking key prefixes via timing.
	return subtle.ConstantTimeCompare([]byte(key), []byte(am.apiKey)) == 1
}
