package testutil

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func mustSetEnv(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Failed to set env %s: %v", key, err)
	}
}

func mustUnsetEnv(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Failed to unset env %s: %v", key, err)
	}
}

func deferRestoreEnv(t *testing.T, key string, originalValue string) {
	t.Helper()
	t.Cleanup(func() {
		if originalValue == "" {
			mustUnsetEnv(t, key)
		} else {
			mustSetEnv(t, key, originalValue)
		}
	})
}

func TestGetTestRedisOptions(t *testing.T) {
	// Test with default environment (no REDIS_TEST_ADDR set)
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	mustUnsetEnv(t, "REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	options := GetTestRedisOptions()
	assert.NotNil(t, options)
	assert.Equal(t, "localhost:6379", options.Addr)
	assert.Equal(t, 1, options.DB)

	// Test with custom environment variable
	testAddr := "localhost:6380"
	mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)

	options = GetTestRedisOptions()
	assert.NotNil(t, options)
	assert.Equal(t, testAddr, options.Addr)
	assert.Equal(t, 1, options.DB)
}

func TestGetTestRedisClient(t *testing.T) {
	// Test client creation with default options
	client := GetTestRedisClient()
	assert.NotNil(t, client)
	assert.Equal(t, "localhost:6379", client.Options().Addr)
	assert.Equal(t, 1, client.Options().DB)

	// Test client creation with custom environment
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	testAddr := "localhost:6380"
	mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)

	client = GetTestRedisClient()
	assert.NotNil(t, client)
	assert.Equal(t, testAddr, client.Options().Addr)
	assert.Equal(t, 1, client.Options().DB)

	// Restore environment
	mustSetEnv(t, "REDIS_TEST_ADDR", originalAddr)
}

func TestGetTestRedisOptions_NilReturn(t *testing.T) {
	// This test ensures the function never returns nil
	options := GetTestRedisOptions()
	assert.NotNil(t, options)
}

func TestGetTestRedisClient_NilReturn(t *testing.T) {
	// This test ensures the function never returns nil
	client := GetTestRedisClient()
	assert.NotNil(t, client)
}

func TestGetTestRedisOptions_DefaultDB(t *testing.T) {
	// Test that the default database is always set to 1 (test database)
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	mustUnsetEnv(t, "REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	options := GetTestRedisOptions()
	assert.Equal(t, 1, options.DB)
}

func TestGetTestRedisClient_DefaultDB(t *testing.T) {
	// Test that the client is configured to use database 1 (test database)
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	mustUnsetEnv(t, "REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	client := GetTestRedisClient()
	assert.Equal(t, 1, client.Options().DB)
}

func TestGetTestRedisOptions_FallbackAddress(t *testing.T) {
	// Test that the function falls back to localhost:6379 when no env var is set
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	mustUnsetEnv(t, "REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	options := GetTestRedisOptions()
	assert.Equal(t, "localhost:6379", options.Addr)
}

func TestGetTestRedisOptions_EnvironmentPriority(t *testing.T) {
	// Test that environment variable takes priority over default
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	// Set custom address
	customAddr := "redis.example.com:6379"
	mustSetEnv(t, "REDIS_TEST_ADDR", customAddr)

	options := GetTestRedisOptions()
	assert.Equal(t, customAddr, options.Addr)
}

func TestGetTestRedisClient_EnvironmentPriority(t *testing.T) {
	// Test that client uses environment variable when set
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	// Set custom address
	customAddr := "redis.example.com:6379"
	mustSetEnv(t, "REDIS_TEST_ADDR", customAddr)

	client := GetTestRedisClient()
	assert.Equal(t, customAddr, client.Options().Addr)
}

func TestGetTestRedisOptions_ConcurrentAccess(t *testing.T) {
	// Test that the function is safe to call concurrently
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	// Set a test address
	testAddr := "localhost:6379"
	mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			options := GetTestRedisOptions()
			assert.NotNil(t, options)
			assert.Equal(t, testAddr, options.Addr)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestGetTestRedisClient_ConcurrentAccess(t *testing.T) {
	// Test that the function is safe to call concurrently
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	// Set a test address
	testAddr := "localhost:6379"
	mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			client := GetTestRedisClient()
			assert.NotNil(t, client)
			assert.Equal(t, testAddr, client.Options().Addr)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestGetTestRedisOptions_EmptyEnvironmentVariable(t *testing.T) {
	// Test behavior when REDIS_TEST_ADDR is set to empty string
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)
	mustSetEnv(t, "REDIS_TEST_ADDR", "")

	options := GetTestRedisOptions()
	// Should fall back to default when empty string is set
	assert.Equal(t, "localhost:6379", options.Addr)
}

func TestGetTestRedisClient_EmptyEnvironmentVariable(t *testing.T) {
	// Test behavior when REDIS_TEST_ADDR is set to empty string
	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)
	mustSetEnv(t, "REDIS_TEST_ADDR", "")

	client := GetTestRedisClient()
	// Should fall back to default when empty string is set
	assert.Equal(t, "localhost:6379", client.Options().Addr)
}

func TestGetTestRedisOptions_ValidPortInAddress(t *testing.T) {
	// Test that the function correctly handles addresses with ports
	testCases := []string{
		"localhost:6379",
		"localhost:6380",
		"127.0.0.1:6379",
		"redis.example.com:6379",
	}

	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	for _, testAddr := range testCases {
		mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)
		options := GetTestRedisOptions()
		assert.Equal(t, testAddr, options.Addr)
	}
}

func TestGetTestRedisClient_ValidPortInAddress(t *testing.T) {
	// Test that the client correctly handles addresses with ports
	testCases := []string{
		"localhost:6379",
		"localhost:6380",
		"127.0.0.1:6379",
		"redis.example.com:6379",
	}

	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	for _, testAddr := range testCases {
		mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)
		client := GetTestRedisClient()
		assert.Equal(t, testAddr, client.Options().Addr)
	}
}

func TestGetTestRedisOptions_DBConsistency(t *testing.T) {
	// Test that the database is always set to 1 regardless of address
	testCases := []string{
		"localhost:6379",
		"localhost:6380",
		"127.0.0.1:6379",
		"redis.example.com:6379",
	}

	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	for _, testAddr := range testCases {
		mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)
		options := GetTestRedisOptions()
		assert.Equal(t, 1, options.DB)
	}
}

func TestGetTestRedisClient_DBConsistency(t *testing.T) {
	// Test that the client always uses database 1 regardless of address
	testCases := []string{
		"localhost:6379",
		"localhost:6380",
		"127.0.0.1:6379",
		"redis.example.com:6379",
	}

	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)

	for _, testAddr := range testCases {
		mustSetEnv(t, "REDIS_TEST_ADDR", testAddr)
		client := GetTestRedisClient()
		assert.Equal(t, 1, client.Options().DB)
	}
}

func TestGetTestRedisOptions_MiniredisIntegration(t *testing.T) {
	// Test integration with miniredis for actual Redis testing
	s, err := miniredis.Run()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment; skipping integration test")
			return
		}
		assert.NoError(t, err)
	}
	t.Cleanup(func() {
		s.Close()
	})

	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)
	mustSetEnv(t, "REDIS_TEST_ADDR", s.Addr())

	options := GetTestRedisOptions()
	assert.Equal(t, s.Addr(), options.Addr)
	assert.Equal(t, 1, options.DB)

	// Test that we can create a client with these options and it works
	client := redis.NewClient(options)
	assert.NotNil(t, client)

	// Test basic Redis operation
	err = client.Set(context.Background(), "test_key", "test_value", 0).Err()
	assert.NoError(t, err)

	value, err := client.Get(context.Background(), "test_key").Result()
	assert.NoError(t, err)
	assert.Equal(t, "test_value", value)
}

func TestGetTestRedisClient_MiniredisIntegration(t *testing.T) {
	// Test integration with miniredis for actual Redis testing
	s, err := miniredis.Run()
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment; skipping integration test")
			return
		}
		assert.NoError(t, err)
	}
	t.Cleanup(func() {
		s.Close()
	})

	originalAddr := os.Getenv("REDIS_TEST_ADDR")
	deferRestoreEnv(t, "REDIS_TEST_ADDR", originalAddr)
	mustSetEnv(t, "REDIS_TEST_ADDR", s.Addr())

	client := GetTestRedisClient()
	assert.NotNil(t, client)
	assert.Equal(t, s.Addr(), client.Options().Addr)
	assert.Equal(t, 1, client.Options().DB)

	// Test basic Redis operation
	err = client.Set(context.Background(), "test_key", "test_value", 0).Err()
	assert.NoError(t, err)

	value, err := client.Get(context.Background(), "test_key").Result()
	assert.NoError(t, err)
	assert.Equal(t, "test_value", value)
}
