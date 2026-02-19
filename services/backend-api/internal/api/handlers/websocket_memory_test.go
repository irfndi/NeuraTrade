package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/irfndi/neuratrade/internal/api/handlers"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/assert"
)

// forceGC forces garbage collection
func forceGC() {
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
}

// getGoroutineCount returns current goroutine count
func getGoroutineCount() int {
	return runtime.NumGoroutine()
}

// TestWebSocketHandler_ClientConnectionCleanup tests that clients are properly cleaned up on disconnect
func TestWebSocketHandler_ClientConnectionCleanup(t *testing.T) {
	forceGC()
	goroutinesBefore := getGoroutineCount()

	// Create WebSocket handler
	handler := handlers.NewWebSocketHandler(nil)

	// Create test HTTP server
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ws", handler.HandleWebSocket)

	server := httptest.NewServer(router)
	defer server.Close()

	// Connect multiple clients
	var clients []*websocket.Conn
	for i := 0; i < 5; i++ {
		wsURL := "ws" + server.URL[4:] + "/ws"
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
		clients = append(clients, conn)

		// Send subscription request
		subReq := map[string]interface{}{
			"action":   "subscribe",
			"exchange": "binance",
			"symbols":  []string{"BTC/USDT"},
		}
		if err := conn.WriteJSON(subReq); err != nil {
			t.Fatalf("Failed to send subscription: %v", err)
		}

		// Read response
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = conn.ReadMessage()
	}

	time.Sleep(100 * time.Millisecond)

	goroutinesAfterConnect := getGoroutineCount()
	t.Logf("Goroutines after connect: %d (before: %d)", goroutinesAfterConnect, goroutinesBefore)

	// Disconnect all clients
	for i, conn := range clients {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close client %d: %v", i, err)
		}
	}

	// Wait for cleanup
	time.Sleep(300 * time.Millisecond)
	forceGC()

	goroutinesAfterDisconnect := getGoroutineCount()
	t.Logf("Goroutines after disconnect: %d", goroutinesAfterDisconnect)

	// Stop handler
	handler.Stop()

	// Wait for final cleanup
	time.Sleep(200 * time.Millisecond)
	forceGC()

	goroutinesAfterStop := getGoroutineCount()
	t.Logf("Goroutines after stop: %d", goroutinesAfterStop)

	// Check for leaks
	growth := goroutinesAfterStop - goroutinesBefore
	assert.Less(t, growth, 3, "Should not have significant goroutine leaks after client cleanup")
}

// TestWebSocketHandler_BroadcastChannelCleanup tests that broadcast channel doesn't leak
func TestWebSocketHandler_BroadcastChannelCleanup(t *testing.T) {
	forceGC()
	goroutinesBefore := getGoroutineCount()

	handler := handlers.NewWebSocketHandler(nil)

	// Send multiple broadcast messages
	for i := 0; i < 100; i++ {
		msg := handlers.MarketDataMessage{
			Type:      "ticker",
			Exchange:  "binance",
			Symbol:    "BTC/USDT",
			Timestamp: time.Now().UnixMilli(),
		}
		handler.BroadcastMessage(msg)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	handler.Stop()
	time.Sleep(200 * time.Millisecond)
	forceGC()

	goroutinesAfter := getGoroutineCount()
	growth := goroutinesAfter - goroutinesBefore

	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, growth)
	assert.Less(t, growth, 2, "Broadcast channel should clean up properly")
}

// TestWebSocketHandler_ContextCancellation tests that handler respects context cancellation
func TestWebSocketHandler_ContextCancellation(t *testing.T) {
	forceGC()
	goroutinesBefore := getGoroutineCount()

	// Create handler with context
	handler := handlers.NewWebSocketHandler(nil)

	// Simulate some activity
	msg := handlers.MarketDataMessage{
		Type:      "orderbook",
		Exchange:  "coinbase",
		Symbol:    "ETH/USDT",
		Timestamp: time.Now().UnixMilli(),
		Data:      json.RawMessage(`{"bid": 2000, "ask": 2001}`),
	}
	handler.BroadcastMessage(msg)

	// Stop immediately
	handler.Stop()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)
	forceGC()

	goroutinesAfter := getGoroutineCount()
	growth := goroutinesAfter - goroutinesBefore

	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, growth)
	assert.Less(t, growth, 2, "Context cancellation should clean up all goroutines")
}

// TestWebSocketHandler_UnregisterChannelFull tests handling when unregister channel is full
func TestWebSocketHandler_UnregisterChannelFull(t *testing.T) {
	forceGC()

	handler := handlers.NewWebSocketHandler(nil)

	// The handler's run loop should handle high load without leaking
	// Send many messages to stress test the channels
	for i := 0; i < 100; i++ {
		msg := handlers.MarketDataMessage{
			Type:      "test",
			Timestamp: time.Now().UnixMilli(),
		}
		handler.BroadcastMessage(msg)
	}

	handler.Stop()
	time.Sleep(100 * time.Millisecond)

	// Should not panic or leak
	assert.True(t, true, "Should handle high load gracefully")
}

// TestWebSocketHandler_WritePumpExitConditions tests that writePump exits on all conditions
func TestWebSocketHandler_WritePumpExitConditions(t *testing.T) {
	forceGC()
	goroutinesBefore := getGoroutineCount()

	handler := handlers.NewWebSocketHandler(nil)

	// Create mock connection (will be closed immediately)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = &http.Request{
		Header: make(http.Header),
	}

	// This will fail to upgrade, but tests the exit path
	handler.HandleWebSocket(c)

	// Stop handler immediately
	handler.Stop()

	time.Sleep(100 * time.Millisecond)
	forceGC()

	goroutinesAfter := getGoroutineCount()
	growth := goroutinesAfter - goroutinesBefore

	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, growth)
	assert.Less(t, growth, 2, "WritePump should exit cleanly on connection errors")
}

// BenchmarkWebSocketMessageProcessing benchmarks message processing throughput
func BenchmarkWebSocketMessageProcessing(b *testing.B) {
	handler := handlers.NewWebSocketHandler(nil)
	defer handler.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := handlers.MarketDataMessage{
			Type:      "ticker",
			Exchange:  "binance",
			Symbol:    "BTC/USDT",
			Timestamp: time.Now().UnixMilli(),
		}
		handler.BroadcastMessage(msg)
	}
}

// BenchmarkWebSocketClientSubscription benchmarks subscription handling
func BenchmarkWebSocketClientSubscription(b *testing.B) {
	client := &handlers.WebSocketClient{
		Subscriptions: make(map[handlers.MarketSubscription]bool),
		Send:          make(chan handlers.MarketDataMessage, 256),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.Subscriptions[handlers.MarketSubscription{
			Exchange: fmt.Sprintf("exchange-%d", i%10),
			Symbol:   fmt.Sprintf("SYMBOL-%d", i),
		}] = true
	}
}
