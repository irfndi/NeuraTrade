// Package middleware provides HTTP middleware components for authentication,
// authorization, rate limiting, telemetry, and other cross-cutting concerns.
// These components are used throughout the NeuraTrade API for request processing.
package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims represents the JWT token claims.
// Contains user identification and authentication information.
type JWTClaims struct {
	// UserID is the user identifier.
	UserID string `json:"user_id"`
	// Email is the user email.
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// AuthMiddleware provides JWT authentication middleware.
// Validates Bearer tokens in the Authorization header.
type AuthMiddleware struct {
	secretKey []byte
}

// NewAuthMiddleware creates a new JWT authentication middleware.
//
// Parameters:
//   - secretKey: Secret key for signing and verifying tokens.
//
// Returns:
//   - *AuthMiddleware: Initialized middleware instance.
func NewAuthMiddleware(secretKey string) *AuthMiddleware {
	return &AuthMiddleware{
		secretKey: []byte(secretKey),
	}
}

// RequireAuth middleware validates JWT tokens.
// Requires a valid Bearer token in the Authorization header.
//
// Returns:
//   - gin.HandlerFunc: Gin middleware handler.
//
// Response on failure:
//   - 401: Missing or invalid authorization header.
//   - 401: Invalid or expired token.
func (am *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Check Bearer prefix (case-insensitive as per RFC 6750)
		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || strings.ToLower(tokenParts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		tokenString := tokenParts[1]
		// Check for empty token
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		// Parse and validate token
		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			// Validate signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return am.secretKey, nil
		})

		if err != nil {
			// Check if it's an expiration error
			if errors.Is(err, jwt.ErrTokenExpired) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired"})
				c.Abort()
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		// Check if token is valid
		if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
			// Set user context
			c.Set("user_id", claims.UserID)
			c.Set("user_email", claims.Email)
			c.Next()
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}
	}
}

// OptionalAuth middleware validates JWT tokens but doesn't require them.
// If a valid token is present, user context is set.
//
// Returns:
//
//	gin.HandlerFunc: Gin handler.
func (am *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// No token provided, continue without authentication
			c.Next()
			return
		}

		// If token is provided, validate it
		tokenParts := strings.Split(authHeader, " ")
		if len(tokenParts) != 2 || strings.ToLower(tokenParts[0]) != "bearer" {
			c.Next()
			return
		}

		tokenString := tokenParts[1]
		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return am.secretKey, nil
		})

		if err == nil && token.Valid {
			if claims, ok := token.Claims.(*JWTClaims); ok {
				if claims.ExpiresAt == nil || claims.ExpiresAt.After(time.Now()) {
					c.Set("user_id", claims.UserID)
					c.Set("user_email", claims.Email)
				}
			}
		}

		c.Next()
	}
}

// GenerateToken creates a new JWT token for a user.
//
// Parameters:
//
//	userID: User identifier.
//	email: User email.
//	duration: Token validity duration.
//
// Returns:
//
//	string: Signed token string.
//	error: Error if generation fails.
func (am *AuthMiddleware) GenerateToken(userID, email string, duration time.Duration) (string, error) {
	claims := &JWTClaims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(am.secretKey)
}

// ValidateToken validates a JWT token and returns claims.
//
// Parameters:
//
//	tokenString: Token string to validate.
//
// Returns:
//
//	*JWTClaims: Token claims.
//	error: Error if validation fails.
func (am *AuthMiddleware) ValidateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return am.secretKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}
