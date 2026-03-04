package agentcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAgentRuntimeEndToEndEventToCommandToAudit(t *testing.T) {
	const apiKey = "test-admin-api-key"

	commandCalls := make(chan string, 4)
	var streamOnce sync.Once

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/agent/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "stream unsupported", http.StatusInternalServerError)
				return
			}

			streamOnce.Do(func() {
				event := Event{
					ID:        "evt-collector-1",
					Topic:     "collector.health",
					Type:      "CollectorDegraded",
					Payload:   "binance",
					Timestamp: time.Now().UTC(),
					Source:    "backend-api",
				}
				payload, _ := json.Marshal(event)
				fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
			})

			<-r.Context().Done()
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/pause-exchange":
			var req struct {
				ExchangeID string `json:"exchange_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			commandCalls <- req.ExchangeID
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auditLogger := NewLogger(AuditConfig{Level: LevelInfo, MaxEntries: 500})
	defer auditLogger.Close()

	backendClient := NewBackendClient(ClientConfig{
		BaseURL:     server.URL,
		Timeout:     2 * time.Second,
		MaxRetries:  0,
		AdminAPIKey: apiKey,
	})

	ingestor := NewIngestor(IngestConfig{
		BackendEventURL: server.URL + "/api/v1/agent/events",
		BufferSize:      64,
		ReconnectDelay:  50 * time.Millisecond,
		AdminAPIKey:     apiKey,
	})

	policyEngine := NewEngine(PolicyConfig{})
	registry := NewRegistry()
	registry.Register("pause_exchange_on_errors", Playbook{
		Name:        "pause_exchange_on_errors",
		Description: "Pause exchange when collector is degraded",
		Execute: func(ctx context.Context, event any) error {
			exchangeID, ok := event.(string)
			if !ok || strings.TrimSpace(exchangeID) == "" {
				return fmt.Errorf("invalid event payload type: %T", event)
			}
			auditLogger.Log(ctx, ActionCommandSent, "integration_test", map[string]any{
				"command":     "pause_exchange",
				"exchange_id": exchangeID,
			})
			return backendClient.PauseExchange(ctx, exchangeID)
		},
	})

	runtime := NewAgentRuntime(AgentRuntimeConfig{
		AuditLogger:      auditLogger,
		BackendClient:    backendClient,
		EventIngestor:    ingestor,
		PolicyEngine:     policyEngine,
		PlaybookRegistry: registry,
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runtime.Start(runCtx); err != nil {
		t.Fatalf("start runtime: %v", err)
	}

	select {
	case exchange := <-commandCalls:
		if exchange != "binance" {
			t.Fatalf("unexpected exchange id: %q", exchange)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for backend command call")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if hasAuditAction(auditLogger.GetEntries(), ActionEventReceived) &&
			hasAuditAction(auditLogger.GetEntries(), ActionCommandSent) &&
			hasAuditAction(auditLogger.GetEntries(), ActionPlaybookCompleted) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected audit trail not found; entries=%+v", auditLogger.GetEntries())
		}
		time.Sleep(25 * time.Millisecond)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown runtime: %v", err)
	}
}

func hasAuditAction(entries []Entry, action ActionType) bool {
	for _, entry := range entries {
		if entry.ActionType == action {
			return true
		}
	}
	return false
}
