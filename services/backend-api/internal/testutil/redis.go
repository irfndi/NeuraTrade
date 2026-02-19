package testutil

import (
	"crypto/rand"
	"encoding/hex"
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
// The secret is at least 32 characters long to meet JWT requirements.
// This avoids hardcoded secrets in test files.
func GenerateTestSecret() string {
	// Generate 32 random bytes (64 hex chars)
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to environment variable if crypto/rand fails
		if envSecret := os.Getenv("TEST_JWT_SECRET"); envSecret != "" && len(envSecret) >= 32 {
			return envSecret
		}
		// A failure to read from rand.Read is a critical problem in the environment.
		// Panicking is better than using a hardcoded, insecure secret.
		panic("failed to generate random bytes for test secret and TEST_JWT_SECRET is not set")
	}
	return hex.EncodeToString(bytes)
}

// MustGenerateTestSecret generates a test secret or panics if generation fails.
// Use this in test setup where a secret is required.
func MustGenerateTestSecret() string {
	secret := GenerateTestSecret()
	if len(secret) < 32 {
		panic("generated test secret is too short (minimum 32 characters required)")
	}
	return secret
}
