package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

const (
	AnthropicDefaultBaseURL = "https://api.anthropic.com/v1"
	AnthropicDefaultTimeout = 120 * time.Second
	AnthropicVersion        = "2023-06-01"
)

type AnthropicClient struct {
	config     ClientConfig
	httpClient *http.Client
	logger     *zap.Logger
}

func NewAnthropicClient(config ClientConfig) *AnthropicClient {
	timeout := config.HTTPTimeout
	if timeout == 0 {
		timeout = AnthropicDefaultTimeout
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = AnthropicDefaultBaseURL
	}

	return &AnthropicClient{
		config: ClientConfig{
			APIKey:      config.APIKey,
			BaseURL:     baseURL,
			HTTPTimeout: timeout,
			MaxRetries:  config.MaxRetries,
			ModelInfo:   config.ModelInfo,
		},
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: zap.NewNop(),
	}
}

func (c *AnthropicClient) SetLogger(logger *zap.Logger) {
	c.logger = logger
}

func (c *AnthropicClient) Provider() Provider {
	return ProviderAnthropic
}

func (c *AnthropicClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type       string               `json:"type"`
	Text       string               `json:"text,omitempty"`
	ToolUse    *anthropicToolUse    `json:"tool_use,omitempty"`
	ToolResult *anthropicToolResult `json:"tool_result,omitempty"`
}

type anthropicToolUse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Model        string             `json:"model"`
	Content      []anthropicContent `json:"content"`
	StopReason   string             `json:"stop_reason"`
	StopSequence string             `json:"stop_sequence,omitempty"`
	Usage        anthropicUsage     `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *AnthropicClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	startTime := time.Now()

	anthropicReq := c.convertRequest(req)
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.config.APIKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp.StatusCode, respBody)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	latencyMs := time.Since(startTime).Milliseconds()

	return c.convertResponse(&anthropicResp, latencyMs), nil
}

func (c *AnthropicClient) Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error) {
	eventChan := make(chan StreamEvent, 100)

	streamReq := *req
	streamReq.Stream = true

	anthropicReq := c.convertRequest(&streamReq)
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		close(eventChan)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		close(eventChan)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.config.APIKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		close(eventChan)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		close(eventChan)
		return nil, c.handleErrorResponse(resp.StatusCode, respBody)
	}

	go c.processStream(resp.Body, eventChan)

	return eventChan, nil
}

func (c *AnthropicClient) processStream(reader io.ReadCloser, eventChan chan<- StreamEvent) {
	defer close(eventChan)
	defer func() { _ = reader.Close() }()

	scanner := bufio.NewScanner(reader)
	var currentEventType string

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Parse event type line: "event: content_block_delta"
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Parse data line: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonData := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type         string             `json:"type"`
			Index        int                `json:"index,omitempty"`
			Delta        *anthropicDelta    `json:"delta,omitempty"`
			Message      *anthropicResponse `json:"message,omitempty"`
			ContentBlock *anthropicContent  `json:"content_block,omitempty"`
		}

		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			eventChan <- StreamEvent{Type: StreamEventError, Error: err}
			continue
		}

		// Use the event type from the SSE line if available, otherwise use the JSON type
		eventType := event.Type
		if currentEventType != "" {
			eventType = currentEventType
		}

		switch eventType {
		case "content_block_delta":
			if event.Delta != nil && event.Delta.Text != "" {
				eventChan <- StreamEvent{
					Type:  StreamEventContentDelta,
					Delta: event.Delta.Text,
				}
			}
		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.ToolUse != nil {
				eventChan <- StreamEvent{
					Type: StreamEventToolCallDelta,
					ToolCall: &ToolCall{
						ID:   event.ContentBlock.ToolUse.ID,
						Name: event.ContentBlock.ToolUse.Name,
					},
				}
			}
		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
				return
			}
		case "message_start":
			if event.Message != nil {
				eventChan <- StreamEvent{
					Type: StreamEventUsage,
					Usage: &UsageMetrics{
						InputTokens: event.Message.Usage.InputTokens,
					},
				}
			}
		case "message_stop":
			eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
			return
		}
	}

	// Handle scanner errors
	if err := scanner.Err(); err != nil {
		eventChan <- StreamEvent{Type: StreamEventError, Error: err}
	}

	// If we exit the loop without seeing message_stop, send done event
	eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
}

type anthropicDelta struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}

func (c *AnthropicClient) convertRequest(req *CompletionRequest) *anthropicRequest {
	var systemPrompt string
	var messages []anthropicMessage

	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			systemPrompt = msg.Content
			continue
		}

		content := []anthropicContent{{
			Type: "text",
			Text: msg.Content,
		}}

		if msg.ToolCall != nil {
			content = []anthropicContent{{
				Type: "tool_use",
				ToolUse: &anthropicToolUse{
					ID:    msg.ToolCall.ID,
					Name:  msg.ToolCall.Name,
					Input: msg.ToolCall.Arguments,
				},
			}}
		}

		if msg.Role == RoleTool {
			content = []anthropicContent{{
				Type: "tool_result",
				ToolResult: &anthropicToolResult{
					ToolUseID: msg.ToolID,
					Content:   msg.Content,
				},
			}}
		}

		role := string(msg.Role)
		if msg.Role == RoleTool {
			role = "user"
		}

		messages = append(messages, anthropicMessage{
			Role:    role,
			Content: content,
		})
	}

	tools := make([]anthropicTool, len(req.Tools))
	for i, tool := range req.Tools {
		tools[i] = anthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	return &anthropicRequest{
		Model:         req.Model,
		Messages:      messages,
		System:        systemPrompt,
		Tools:         tools,
		MaxTokens:     maxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.StopSequences,
		Stream:        req.Stream,
	}
}

func (c *AnthropicClient) convertResponse(resp *anthropicResponse, latencyMs int64) *CompletionResponse {
	message := Message{
		Role: RoleAssistant,
	}

	var toolCalls []ToolCall
	var textContent string

	for _, content := range resp.Content {
		if content.Type == "text" {
			textContent += content.Text
		} else if content.Type == "tool_use" && content.ToolUse != nil {
			toolCalls = append(toolCalls, ToolCall{
				ID:        content.ToolUse.ID,
				Name:      content.ToolUse.Name,
				Arguments: content.ToolUse.Input,
			})
		}
	}

	message.Content = textContent

	usage := UsageMetrics{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		TotalTokens:  resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}

	cost := c.calculateCost(usage)

	return &CompletionResponse{
		ID:           resp.ID,
		Model:        resp.Model,
		Provider:     ProviderAnthropic,
		Created:      time.Now(),
		Message:      message,
		ToolCalls:    toolCalls,
		Usage:        usage,
		Cost:         cost,
		LatencyMs:    latencyMs,
		FinishReason: resp.StopReason,
	}
}

func (c *AnthropicClient) calculateCost(usage UsageMetrics) CostMetrics {
	if c.config.ModelInfo == nil {
		return CostMetrics{}
	}

	million := decimal.NewFromInt(1000000)
	inputCost := decimal.NewFromInt(int64(usage.InputTokens)).Div(million).Mul(c.config.ModelInfo.Cost.InputCost)
	outputCost := decimal.NewFromInt(int64(usage.OutputTokens)).Div(million).Mul(c.config.ModelInfo.Cost.OutputCost)

	return CostMetrics{
		InputCost:  inputCost,
		OutputCost: outputCost,
		TotalCost:  inputCost.Add(outputCost),
	}
}

func (c *AnthropicClient) handleErrorResponse(statusCode int, body []byte) error {
	var apiErr anthropicError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return fmt.Errorf("anthropic API error (status %d): %s", statusCode, string(body))
	}

	switch statusCode {
	case http.StatusTooManyRequests:
		return RateLimitedError{Provider: ProviderAnthropic, RetryAfter: 30 * time.Second}
	case http.StatusBadRequest:
		if apiErr.Error.Type == "invalid_request_error" {
			return fmt.Errorf("anthropic API error: %s", apiErr.Error.Message)
		}
	}

	return fmt.Errorf("anthropic API error: %s (type: %s)", apiErr.Error.Message, apiErr.Error.Type)
}
