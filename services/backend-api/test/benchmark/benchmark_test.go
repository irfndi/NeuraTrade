package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Context key types to avoid collisions
type contextKey string

const (
	userIDKey    contextKey = "user_id"
	requestIDKey contextKey = "request_id"
	timestampKey contextKey = "timestamp"
)

// BenchmarkAPIPerformance benchmarks API performance
func BenchmarkAPIPerformance(b *testing.B) {
	// Set Gin to release mode for benchmarking
	gin.SetMode(gin.ReleaseMode)

	// Create a simple router
	router := gin.New()

	// Add middleware
	router.Use(gin.Recovery())

	// Define benchmark routes
	router.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now(),
		})
	})

	router.GET("/api/v1/market/ticker/:exchange/:symbol", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"exchange":  c.Param("exchange"),
			"symbol":    c.Param("symbol"),
			"price":     50000.0,
			"volume":    1000.0,
			"timestamp": time.Now(),
		})
	})

	router.POST("/api/v1/arbitrage/opportunities", func(c *gin.Context) {
		var data struct {
			ExchangeA string  `json:"exchange_a"`
			ExchangeB string  `json:"exchange_b"`
			Symbol    string  `json:"symbol"`
			Profit    float64 `json:"profit"`
		}

		if err := c.ShouldBindJSON(&data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Opportunity calculated",
			"data":    data,
		})
	})

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Test health endpoint
		req, _ := http.NewRequest("GET", "/api/v1/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Test ticker endpoint
		req, _ = http.NewRequest("GET", "/api/v1/market/ticker/binance/BTCUSDT", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// Test arbitrage endpoint
		data := map[string]interface{}{
			"exchange_a": "binance",
			"exchange_b": "coinbase",
			"symbol":     "BTCUSDT",
			"profit":     1.5,
		}
		jsonData, _ := json.Marshal(data)
		req, _ = http.NewRequest("POST", "/api/v1/arbitrage/opportunities", bytes.NewBuffer(jsonData))
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkConcurrentRequests benchmarks concurrent request handling
func BenchmarkConcurrentRequests(b *testing.B) {
	// Set Gin to release mode for benchmarking
	gin.SetMode(gin.ReleaseMode)

	// Create a simple router
	router := gin.New()
	router.Use(gin.Recovery())

	// Define a simple endpoint
	router.GET("/api/v1/test", func(c *gin.Context) {
		// Simulate some processing
		time.Sleep(1 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"message": "test"})
	})

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark with concurrent requests
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("GET", "/api/v1/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	})
}

// BenchmarkMiddleware benchmarks middleware performance
func BenchmarkMiddleware(b *testing.B) {
	// Set Gin to release mode for benchmarking
	gin.SetMode(gin.ReleaseMode)

	// Create a router with middleware
	router := gin.New()

	// Add multiple middleware
	router.Use(func(c *gin.Context) {
		c.Header("X-Middleware-1", "value1")
		c.Next()
	})

	router.Use(func(c *gin.Context) {
		c.Header("X-Middleware-2", "value2")
		c.Next()
	})

	router.Use(func(c *gin.Context) {
		c.Set("user_id", "12345")
		c.Next()
	})

	// Define endpoint
	router.GET("/api/v1/middleware-test", func(c *gin.Context) {
		userID := c.MustGet("user_id").(string)
		c.JSON(http.StatusOK, gin.H{"user_id": userID})
	})

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", "/api/v1/middleware-test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// BenchmarkJSONHandling benchmarks JSON serialization/deserialization
func BenchmarkJSONHandling(b *testing.B) {
	// Test data structure
	type TestData struct {
		ID        int                    `json:"id"`
		Name      string                 `json:"name"`
		Data      map[string]interface{} `json:"data"`
		Timestamp time.Time              `json:"timestamp"`
	}

	// Create test data
	data := TestData{
		ID:   1,
		Name: "test",
		Data: map[string]interface{}{
			"value1": "string",
			"value2": 123,
			"value3": true,
			"value4": []int{1, 2, 3, 4, 5},
		},
		Timestamp: time.Now(),
	}

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Marshal JSON
		jsonData, err := json.Marshal(data)
		if err != nil {
			b.Fatal(err)
		}

		// Unmarshal JSON
		var result TestData
		err = json.Unmarshal(jsonData, &result)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkContextOperations benchmarks context operations
func BenchmarkContextOperations(b *testing.B) {
	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Create context
		ctx := context.Background()

		// Add values to context
		ctx = context.WithValue(ctx, userIDKey, "12345")
		ctx = context.WithValue(ctx, requestIDKey, "req-123")
		ctx = context.WithValue(ctx, timestampKey, time.Now())

		// Retrieve values from context
		userID := ctx.Value(userIDKey).(string)
		requestID := ctx.Value(requestIDKey).(string)
		timestamp := ctx.Value(timestampKey).(time.Time)

		// Use values to avoid optimization
		if userID == "" || requestID == "" || timestamp.IsZero() {
			b.Fatal("Context values are missing")
		}
	}
}

// BenchmarkMutexOperations benchmarks mutex operations
func BenchmarkMutexOperations(b *testing.B) {
	var counter int64
	var mu sync.RWMutex

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Write operation
			mu.Lock()
			counter++
			mu.Unlock()

			// Read operation
			mu.RLock()
			value := counter
			mu.RUnlock()

			// Use value to avoid optimization
			if value < 0 {
				b.Fatal("Counter should not be negative")
			}
		}
	})
}

// BenchmarkChannelOperations benchmarks channel operations
func BenchmarkChannelOperations(b *testing.B) {
	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Create channels
		ch := make(chan int, 100)
		done := make(chan bool)

		// Start goroutine
		go func() {
			for j := 0; j < 10; j++ {
				ch <- j
			}
			close(ch)
			done <- true
		}()

		// Read from channel
		for range ch {
			// Consume values
		}

		// Wait for completion
		<-done
	}
}

// BenchmarkErrorHandling benchmarks error handling operations
func BenchmarkErrorHandling(b *testing.B) {
	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Create error
		err := fmt.Errorf("test error %d", i)

		// Check error
		if err != nil {
			// Handle error
			errorMsg := err.Error()
			if errorMsg == "" {
				b.Fatal("Error message should not be empty")
			}
		}
	}
}

// BenchmarkTimeOperations benchmarks time operations
func BenchmarkTimeOperations(b *testing.B) {
	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		// Get current time
		now := time.Now()

		// Format time
		formatted := now.Format(time.RFC3339)
		if formatted == "" {
			b.Fatal("Formatted time should not be empty")
		}

		// Parse time
		parsed, err := time.Parse(time.RFC3339, formatted)
		if err != nil {
			b.Fatal(err)
		}

		// Calculate duration
		duration := now.Sub(parsed)
		if duration < 0 {
			b.Fatal("Duration should not be negative")
		}
	}
}
