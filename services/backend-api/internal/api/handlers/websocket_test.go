package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupWebSocketTest(t *testing.T) (*WebSocketHandler, *miniredis.Miniredis, *gin.Engine) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	redisClient := &database.RedisClient{Client: client}
	handler := NewWebSocketHandler(redisClient)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/ws", handler.HandleWebSocket)

	return handler, mr, router
}

func TestWebSocketHandler_NewHandler(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	redisClient := &database.RedisClient{Client: client}

	handler := NewWebSocketHandler(redisClient)
	assert.NotNil(t, handler)
	assert.Equal(t, 0, handler.GetClientCount())
	handler.Stop()
}

func TestWebSocketHandler_Connection(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, handler.GetClientCount())
}

func TestWebSocketHandler_Subscribe(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	subReq := SubscriptionRequest{
		Action:   "subscribe",
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT", "ETH/USDT"},
	}
	err = ws.WriteJSON(subReq)
	require.NoError(t, err)

	_, message, err := ws.ReadMessage()
	require.NoError(t, err)

	var msg MarketDataMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)
	assert.Equal(t, "subscription", msg.Type)
}

func TestWebSocketHandler_PingPong(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	pingReq := SubscriptionRequest{Action: "ping"}
	err = ws.WriteJSON(pingReq)
	require.NoError(t, err)

	_, message, err := ws.ReadMessage()
	require.NoError(t, err)

	var msg MarketDataMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)
	assert.Equal(t, "pong", msg.Type)
}

func TestWebSocketHandler_BroadcastMessage(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	subReq := SubscriptionRequest{
		Action:   "subscribe",
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT"},
	}
	err = ws.WriteJSON(subReq)
	require.NoError(t, err)

	_, _, err = ws.ReadMessage()
	require.NoError(t, err)

	testData, _ := json.Marshal(map[string]interface{}{"price": 50000.0})
	broadcastMsg := MarketDataMessage{
		Type:      "ticker",
		Exchange:  "binance",
		Symbol:    "BTC/USDT",
		Data:      testData,
		Timestamp: time.Now().UnixMilli(),
	}

	handler.BroadcastMessage(broadcastMsg)

	done := make(chan bool)
	go func() {
		_, message, err := ws.ReadMessage()
		if err == nil {
			var msg MarketDataMessage
			if json.Unmarshal(message, &msg) == nil && msg.Type == "ticker" {
				done <- true
				return
			}
		}
		done <- false
	}()

	select {
	case success := <-done:
		assert.True(t, success)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for broadcast message")
	}
}

func TestWebSocketHandler_Unsubscribe(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	subReq := SubscriptionRequest{
		Action:   "subscribe",
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT"},
	}
	err = ws.WriteJSON(subReq)
	require.NoError(t, err)
	_, _, err = ws.ReadMessage()
	require.NoError(t, err)

	unsubReq := SubscriptionRequest{
		Action:   "unsubscribe",
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT"},
	}
	err = ws.WriteJSON(unsubReq)
	require.NoError(t, err)

	_, message, err := ws.ReadMessage()
	require.NoError(t, err)

	var msg MarketDataMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)
	assert.Equal(t, "subscription", msg.Type)
}

func TestWebSocketHandler_InvalidAction(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	invalidReq := SubscriptionRequest{Action: "invalid"}
	err = ws.WriteJSON(invalidReq)
	require.NoError(t, err)

	_, message, err := ws.ReadMessage()
	require.NoError(t, err)

	var msg MarketDataMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)
	assert.Equal(t, "error", msg.Type)
}

func TestWebSocketHandler_GetStats(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	subReq := SubscriptionRequest{
		Action:   "subscribe",
		Exchange: "binance",
		Symbols:  []string{"BTC/USDT", "ETH/USDT"},
	}
	err = ws.WriteJSON(subReq)
	require.NoError(t, err)
	_, _, err = ws.ReadMessage()
	require.NoError(t, err)

	stats := handler.GetStats()
	assert.Equal(t, 1, stats["connected_clients"])
	assert.Equal(t, 2, stats["total_subscriptions"])
}

func TestWebSocketHandler_MultipleClients(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	var conns []*websocket.Conn
	for i := 0; i < 3; i++ {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		conns = append(conns, ws)
	}
	defer func() {
		for _, ws := range conns {
			ws.Close()
		}
	}()

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 3, handler.GetClientCount())
}

func TestWebSocketHandler_PublishToRedis(t *testing.T) {
	handler, _, _ := setupWebSocketTest(t)
	defer handler.Stop()

	msg := MarketDataMessage{
		Type:      "ticker",
		Exchange:  "binance",
		Timestamp: time.Now().UnixMilli(),
	}

	err := handler.PublishToRedis("market:binance", msg)
	require.NoError(t, err)
}

func TestWebSocketHandler_Stop(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)

	server := httptest.NewServer(router)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, handler.GetClientCount())

	handler.Stop()
	server.Close()

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 0, handler.GetClientCount())
	ws.Close()
}

func TestWebSocketHandler_InvalidJSON(t *testing.T) {
	handler, _, router := setupWebSocketTest(t)
	defer handler.Stop()

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	err = ws.WriteMessage(websocket.TextMessage, []byte("invalid json"))
	require.NoError(t, err)

	_, message, err := ws.ReadMessage()
	require.NoError(t, err)

	var msg MarketDataMessage
	err = json.Unmarshal(message, &msg)
	require.NoError(t, err)
	assert.Equal(t, "error", msg.Type)
}

func TestWebSocketHandler_SubscribeToRedisChannels(t *testing.T) {
	handler, _, _ := setupWebSocketTest(t)
	defer handler.Stop()

	err := handler.SubscribeToRedisChannels("market:test")
	require.NoError(t, err)
}
