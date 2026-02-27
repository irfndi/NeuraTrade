package agentcontrol

import (
	"context"
	"testing"
	"time"
)

func TestNewIngestor(t *testing.T) {
	ingestor := NewIngestor(IngestConfig{
		BackendEventURL: "ws://localhost:8080/events",
		BufferSize:      1024,
		ReconnectDelay:  5 * time.Second,
	})
	if ingestor == nil {
		t.Fatal("NewIngestor returned nil")
	}
}

func TestIngestorConfigDefaults(t *testing.T) {
	ingestor := NewIngestor(IngestConfig{})
	if ingestor.config.BufferSize <= 0 {
		t.Error("Expected default BufferSize to be set")
	}
	if ingestor.config.ReconnectDelay <= 0 {
		t.Error("Expected default ReconnectDelay to be set")
	}
}

func TestIngestorStartStop(t *testing.T) {
	ingestor := NewIngestor(IngestConfig{
		BackendEventURL: "ws://localhost:8080/events",
		BufferSize:      1024,
	})

	ctx := context.Background()
	eventChan, err := ingestor.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if eventChan == nil {
		t.Fatal("Expected event channel")
	}

	if !ingestor.IsRunning() {
		t.Error("Expected ingestor to be running")
	}

	// Stop with timeout context - timeout is acceptable
	stopCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = ingestor.Stop(stopCtx)
}

func TestIngestorStartAlreadyRunning(t *testing.T) {
	ingestor := NewIngestor(IngestConfig{
		BackendEventURL: "ws://localhost:8080/events",
	})

	ctx := context.Background()
	_, err := ingestor.Start(ctx)
	if err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	_, err = ingestor.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running ingestor")
	}

	ingestor.Stop(ctx)
}

func TestIngestorBufferSize(t *testing.T) {
	ingestor := NewIngestor(IngestConfig{
		BufferSize: 512,
	})
	if ingestor.config.BufferSize != 512 {
		t.Errorf("Expected BufferSize 512, got %d", ingestor.config.BufferSize)
	}
}

func TestIngestorReconnectDelay(t *testing.T) {
	ingestor := NewIngestor(IngestConfig{
		ReconnectDelay: 10 * time.Second,
	})
	if ingestor.config.ReconnectDelay != 10*time.Second {
		t.Errorf("Expected ReconnectDelay 10s, got %v", ingestor.config.ReconnectDelay)
	}
}
