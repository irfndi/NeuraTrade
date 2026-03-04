// Package ingest handles event ingestion from backend platform.
package agentcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Event represents an ingested event from the backend.
type Event struct {
	ID        string                 `json:"id"`
	Topic     string                 `json:"topic"`
	Type      string                 `json:"type"`
	Payload   any                    `json:"payload"`
	Timestamp time.Time              `json:"timestamp"`
	Source    string                 `json:"source"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// Config holds ingestor configuration.
type IngestConfig struct {
	BackendEventURL string
	BufferSize      int
	ReconnectDelay  time.Duration
	AdminAPIKey     string
}

// Ingestor manages event ingestion from backend.
type Ingestor struct {
	config       IngestConfig
	mu           sync.Mutex
	running      bool
	eventChan    chan Event
	shutdownChan chan struct{}
	httpClient   *http.Client
	loopCancel   context.CancelFunc
	workerWg     sync.WaitGroup
}

// NewIngestor creates a new event ingestor.
func NewIngestor(config IngestConfig) *Ingestor {
	if config.BufferSize <= 0 {
		config.BufferSize = 1024
	}
	if config.ReconnectDelay <= 0 {
		config.ReconnectDelay = 5 * time.Second
	}
	return &Ingestor{
		config:       config,
		eventChan:    make(chan Event, config.BufferSize),
		shutdownChan: make(chan struct{}),
		httpClient: &http.Client{
			Timeout: 0, // stream request; canceled by context
		},
	}
}

// Start begins the event ingestion process.
func (i *Ingestor) Start(ctx context.Context) (<-chan Event, error) {
	i.mu.Lock()
	if i.running {
		i.mu.Unlock()
		return nil, fmt.Errorf("ingestor already running")
	}

	loopCtx, cancel := context.WithCancel(ctx)
	shutdownChan := make(chan struct{})
	i.loopCancel = cancel
	i.shutdownChan = shutdownChan
	i.running = true
	i.workerWg.Add(1)
	i.mu.Unlock()

	// Start connection loop
	go i.connectLoop(loopCtx, shutdownChan)

	return i.eventChan, nil
}

// Stop gracefully stops the ingestor.
func (i *Ingestor) Stop(ctx context.Context) error {
	i.mu.Lock()
	if !i.running {
		i.mu.Unlock()
		return nil
	}

	i.running = false
	loopCancel := i.loopCancel
	shutdownChan := i.shutdownChan
	i.loopCancel = nil
	i.shutdownChan = nil
	i.mu.Unlock()

	if loopCancel != nil {
		loopCancel()
	}
	if shutdownChan != nil {
		close(shutdownChan)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		i.workerWg.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

// connectLoop maintains connection to backend event stream.
func (i *Ingestor) connectLoop(ctx context.Context, shutdownChan <-chan struct{}) {
	defer i.workerWg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-shutdownChan:
			return
		default:
			if err := i.connectAndListen(ctx); err != nil {
				// Log error and retry
				retryTimer := time.NewTimer(i.config.ReconnectDelay)
				select {
				case <-retryTimer.C:
					// Retry
				case <-ctx.Done():
					retryTimer.Stop()
					return
				case <-shutdownChan:
					retryTimer.Stop()
					return
				}
			}
		}
	}
}

// connectAndListen establishes connection and listens for events.
func (i *Ingestor) connectAndListen(ctx context.Context) error {
	if strings.TrimSpace(i.config.BackendEventURL) == "" {
		return fmt.Errorf("backend event url is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.config.BackendEventURL, nil)
	if err != nil {
		return fmt.Errorf("create event stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if adminAPIKey := strings.TrimSpace(i.config.AdminAPIKey); adminAPIKey != "" {
		req.Header.Set("X-API-Key", adminAPIKey)
	}

	resp, err := i.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to event stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("event stream status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	dataLines := make([]string, 0, 4)
	flushEvent := func() {
		if len(dataLines) == 0 {
			return
		}
		raw := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]

		var event Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return
		}
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now().UTC()
		}
		i.publishEvent(ctx, event)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flushEvent()
			continue
		}

		switch {
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case strings.HasPrefix(line, ":"):
			// SSE comment/keepalive line; ignore.
		default:
			// Fallback support for raw JSON lines.
			dataLines = append(dataLines, strings.TrimSpace(line))
		}
	}
	flushEvent()

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read event stream: %w", err)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return io.EOF
}

// publishEvent sends an event to the event channel.
func (i *Ingestor) publishEvent(ctx context.Context, event Event) {
	select {
	case i.eventChan <- event:
		// Event published
	case <-ctx.Done():
		// Context cancelled
	default:
		// Channel full, drop event (backpressure)
	}
}

// IsRunning returns whether the ingestor is currently running.
func (i *Ingestor) IsRunning() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.running
}
