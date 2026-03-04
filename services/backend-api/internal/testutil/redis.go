package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"
)

// GetTestRedisOptions returns Redis options for testing with configurable address
func GetTestRedisOptions() *redis.Options {
	redisAddr := os.Getenv("REDIS_TEST_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379" // fallback for local development
	}

	return &redis.Options{
		Addr: redisAddr,
		DB:   1, // Use test database
	}
}

// GetTestRedisClient returns a Redis client configured for testing
func GetTestRedisClient() *redis.Client {
	return redis.NewClient(GetTestRedisOptions())
}

// GenerateTestSecret generates a cryptographically secure random secret for testing.
// The secret is at least 64 characters long to meet strict JWT test requirements.
// This avoids hardcoded secrets in test files.
func GenerateTestSecret() string {
	// Generate 32 random bytes (64 hex chars)
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to environment variable if crypto/rand fails
		if envSecret := os.Getenv("TEST_JWT_SECRET"); envSecret != "" && len(envSecret) >= 64 {
			return envSecret
		}
		// A failure to read from rand.Read is a critical problem in the environment.
		// Panicking is better than using a hardcoded, insecure secret.
		panic(fmt.Errorf("generate random bytes for test secret: %w; TEST_JWT_SECRET missing or shorter than 64 chars", err))
	}
	return hex.EncodeToString(bytes)
}

// MustGenerateTestSecret generates a test secret or panics if generation fails.
// Use this in test setup where a secret is required.
//
// Note: The length check below is defensive coding to protect against future
// changes to GenerateTestSecret(). GenerateTestSecret() currently guarantees
// 64-character hex output (32 random bytes), but this check ensures we fail
// fast if that guarantee ever changes. This is a safeguard - the function
// will panic if the generated secret is shorter than 64 characters.
func MustGenerateTestSecret() string {
	secret := GenerateTestSecret()
	if len(secret) < 64 {
		panic("generated test secret is too short (minimum 64 characters required)")
	}
	return secret
}
