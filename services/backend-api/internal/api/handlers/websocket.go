package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/telemetry"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return true
	},
}

type MarketDataMessage struct {
	Type      string          `json:"type"`
	Exchange  string          `json:"exchange,omitempty"`
	Symbol    string          `json:"symbol,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

type SubscriptionRequest struct {
	Action   string   `json:"action"`
	Channels []string `json:"channels,omitempty"`
	Exchange string   `json:"exchange,omitempty"`
	Symbols  []string `json:"symbols,omitempty"`
}

type MarketSubscription struct {
	Exchange string
	Symbol   string
}

type WebSocketClient struct {
	conn          *websocket.Conn
	subscriptions map[MarketSubscription]bool
	send          chan MarketDataMessage
	handler       *WebSocketHandler
	clientID      string
	mu            sync.Mutex
}

type WebSocketHandler struct {
	redis      *database.RedisClient
	clients    map[*WebSocketClient]bool
	register   chan *WebSocketClient
	unregister chan *WebSocketClient
	broadcast  chan MarketDataMessage
	mu         sync.RWMutex
	logger     *slog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewWebSocketHandler(redis *database.RedisClient) *WebSocketHandler {
	ctx, cancel := context.WithCancel(context.Background())
	h := &WebSocketHandler{
		redis:      redis,
		clients:    make(map[*WebSocketClient]bool),
		register:   make(chan *WebSocketClient, 256),
		unregister: make(chan *WebSocketClient, 256),
		broadcast:  make(chan MarketDataMessage, 1024),
		logger:     telemetry.Logger(),
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}
	go h.run()
	return h
}

func (h *WebSocketHandler) run() {
	defer close(h.done)
	for {
		select {
		case <-h.ctx.Done():
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Debug("WebSocket client connected", "client_id", client.clientID, "total_clients", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Debug("WebSocket client disconnected", "client_id", client.clientID, "total_clients", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			var toRemove []*WebSocketClient
			for client := range h.clients {
				client.mu.Lock()
				shouldSend := false
				if message.Type == "ticker" || message.Type == "orderbook" {
					sub := MarketSubscription{Exchange: message.Exchange, Symbol: message.Symbol}
					if client.subscriptions[sub] {
						shouldSend = true
					}
				} else {
					shouldSend = true
				}
				client.mu.Unlock()

				if shouldSend {
					select {
					case client.send <- message:
					default:
						close(client.send)
						toRemove = append(toRemove, client)
					}
				}
			}
			h.mu.RUnlock()

			if len(toRemove) > 0 {
				h.mu.Lock()
				for _, client := range toRemove {
					delete(h.clients, client)
				}
				h.mu.Unlock()
			}
		}
	}
}

func (h *WebSocketHandler) Stop() {
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(100 * time.Millisecond):
	}
	h.mu.Lock()
	for client := range h.clients {
		close(client.send)
		delete(h.clients, client)
	}
	h.mu.Unlock()
}

func (h *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("Failed to upgrade WebSocket connection", "error", err)
		return
	}

	client := &WebSocketClient{
		conn:          conn,
		subscriptions: make(map[MarketSubscription]bool),
		send:          make(chan MarketDataMessage, 256),
		handler:       h,
		clientID:      generateClientID(),
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (h *WebSocketHandler) BroadcastMessage(message MarketDataMessage) {
	select {
	case h.broadcast <- message:
	default:
		h.logger.Warn("Broadcast channel full, dropping message")
	}
}

func (h *WebSocketHandler) PublishToRedis(channel string, message MarketDataMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return h.redis.Client.Publish(h.ctx, channel, data).Err()
}

func (h *WebSocketHandler) SubscribeToRedisChannels(channels ...string) error {
	pubsub, err := h.redis.Subscribe(h.ctx, channels...)
	if err != nil {
		return err
	}
	go func() {
		ch := pubsub.Channel()
		for {
			select {
			case <-h.ctx.Done():
				_ = pubsub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var marketMsg MarketDataMessage
				if err := json.Unmarshal([]byte(msg.Payload), &marketMsg); err != nil {
					h.logger.Warn("Failed to unmarshal Redis message", "error", err)
					continue
				}
				h.BroadcastMessage(marketMsg)
			}
		}
	}()
	return nil
}

func (c *WebSocketClient) readPump() {
	defer func() {
		c.handler.unregister <- c
		_ = c.conn.Close()
	}()

	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.handler.logger.Debug("WebSocket read error", "error", err)
			}
			break
		}

		var req SubscriptionRequest
		if err := json.Unmarshal(message, &req); err != nil {
			c.sendError("Invalid JSON format")
			continue
		}

		switch req.Action {
		case "subscribe":
			c.handleSubscribe(req)
		case "unsubscribe":
			c.handleUnsubscribe(req)
		case "ping":
			c.sendPong()
		default:
			c.sendError("Unknown action: " + req.Action)
		}
	}
}

func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			data, err := json.Marshal(message)
			if err != nil {
				c.handler.logger.Warn("Failed to marshal message", "error", err)
				continue
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *WebSocketClient) handleSubscribe(req SubscriptionRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if req.Exchange != "" && len(req.Symbols) > 0 {
		for _, symbol := range req.Symbols {
			sub := MarketSubscription{Exchange: req.Exchange, Symbol: symbol}
			c.subscriptions[sub] = true
		}
		c.sendConfirmation("subscribed", req.Exchange, req.Symbols)
	} else if len(req.Channels) > 0 {
		for _, ch := range req.Channels {
			c.subscriptions[MarketSubscription{Exchange: ch, Symbol: "*"}] = true
		}
		c.sendConfirmation("subscribed", "", req.Channels)
	} else {
		c.sendError("Invalid subscription request")
	}
}

func (c *WebSocketClient) handleUnsubscribe(req SubscriptionRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if req.Exchange != "" && len(req.Symbols) > 0 {
		for _, symbol := range req.Symbols {
			sub := MarketSubscription{Exchange: req.Exchange, Symbol: symbol}
			delete(c.subscriptions, sub)
		}
		c.sendConfirmation("unsubscribed", req.Exchange, req.Symbols)
	} else {
		c.sendError("Invalid unsubscription request")
	}
}

func (c *WebSocketClient) sendConfirmation(action, exchange string, symbols []string) {
	msg := MarketDataMessage{
		Type:      "subscription",
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(gin.H{
		"action":   action,
		"exchange": exchange,
		"symbols":  symbols,
	})
	msg.Data = data
	select {
	case c.send <- msg:
	default:
	}
}

func (c *WebSocketClient) sendError(errMsg string) {
	msg := MarketDataMessage{
		Type:      "error",
		Timestamp: time.Now().UnixMilli(),
	}
	data, _ := json.Marshal(gin.H{"error": errMsg})
	msg.Data = data
	select {
	case c.send <- msg:
	default:
	}
}

func (c *WebSocketClient) sendPong() {
	msg := MarketDataMessage{
		Type:      "pong",
		Timestamp: time.Now().UnixMilli(),
	}
	select {
	case c.send <- msg:
	default:
	}
}

func generateClientID() string {
	return fmt.Sprintf("ws-%d", time.Now().UnixNano())
}

func (h *WebSocketHandler) GetClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *WebSocketHandler) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	subscriptionCount := 0
	for client := range h.clients {
		client.mu.Lock()
		subscriptionCount += len(client.subscriptions)
		client.mu.Unlock()
	}

	return map[string]interface{}{
		"connected_clients":   len(h.clients),
		"total_subscriptions": subscriptionCount,
	}
}
