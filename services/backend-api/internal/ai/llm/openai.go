package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

const (
	OpenAIDefaultBaseURL = "https://api.openai.com/v1"
	OpenAIDefaultTimeout = 60 * time.Second
)

type OpenAIClient struct {
	config     ClientConfig
	httpClient *http.Client
	logger     *zap.Logger
}

func NewOpenAIClient(config ClientConfig) *OpenAIClient {
	timeout := config.HTTPTimeout
	if timeout == 0 {
		timeout = OpenAIDefaultTimeout
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = OpenAIDefaultBaseURL
	}

	return &OpenAIClient{
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

func (c *OpenAIClient) SetLogger(logger *zap.Logger) {
	c.logger = logger
}

func (c *OpenAIClient) Provider() Provider {
	return ProviderOpenAI
}

func (c *OpenAIClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

type openAIRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIMessage       `json:"messages"`
	Tools          []openAITool          `json:"tools,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
	Temperature    *float64              `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	TopP           *float64              `json:"top_p,omitempty"`
	Stop           []string              `json:"stop,omitempty"`
	Stream         bool                  `json:"stream,omitempty"`
	StreamOptions  *openAIStreamOptions  `json:"stream_options,omitempty"`
}

type openAIMessage struct {
	Role      string           `json:"role"`
	Content   interface{}      `json:"content"`
	ToolID    string           `json:"tool_call_id,omitempty"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type openAIResponseFormat struct {
	Type       string                 `json:"type"`
	JSONSchema map[string]interface{} `json:"json_schema,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
	Delta        *openAIDelta  `json:"delta,omitempty"`
}

type openAIDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (c *OpenAIClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	startTime := time.Now()

	openAIReq := c.convertRequest(req)
	body, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

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

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	latencyMs := time.Since(startTime).Milliseconds()

	return c.convertResponse(&openAIResp, latencyMs), nil
}

func (c *OpenAIClient) Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error) {
	eventChan := make(chan StreamEvent, 100)

	streamReq := *req
	streamReq.Stream = true

	openAIReq := c.convertRequest(&streamReq)
	openAIReq.StreamOptions = &openAIStreamOptions{IncludeUsage: true}

	body, err := json.Marshal(openAIReq)
	if err != nil {
		close(eventChan)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		close(eventChan)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
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

func (c *OpenAIClient) processStream(reader io.ReadCloser, eventChan chan<- StreamEvent) {
	defer close(eventChan)
	defer func() { _ = reader.Close() }()

	scanner := bufio.NewScanner(reader)
	var currentToolCalls map[int]*ToolCall

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Parse SSE format: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonData := strings.TrimPrefix(line, "data: ")
		if jsonData == "[DONE]" {
			eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
			return
		}

		var chunk struct {
			ID      string         `json:"id"`
			Object  string         `json:"object"`
			Created int64          `json:"created"`
			Model   string         `json:"model"`
			Choices []openAIChoice `json:"choices"`
			Usage   *openAIUsage   `json:"usage,omitempty"`
		}

		if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
			eventChan <- StreamEvent{Type: StreamEventError, Error: err}
			continue
		}

		if chunk.Object == "" || chunk.ID == "" {
			continue
		}

		if chunk.Usage != nil {
			eventChan <- StreamEvent{
				Type: StreamEventUsage,
				Usage: &UsageMetrics{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
					TotalTokens:  chunk.Usage.TotalTokens,
				},
			}
		}

		for _, choice := range chunk.Choices {
			if choice.Delta == nil {
				continue
			}

			if choice.Delta.Content != "" {
				eventChan <- StreamEvent{
					Type:  StreamEventContentDelta,
					Delta: choice.Delta.Content,
				}
			}

			for _, tc := range choice.Delta.ToolCalls {
				if currentToolCalls == nil {
					currentToolCalls = make(map[int]*ToolCall)
				}

				idx := 0
				if tc.ID != "" {
					if existing, ok := currentToolCalls[idx]; ok {
						existing.Arguments = append(existing.Arguments, tc.Function.Arguments...)
					} else {
						currentToolCalls[idx] = &ToolCall{
							ID:        tc.ID,
							Name:      tc.Function.Name,
							Arguments: json.RawMessage(tc.Function.Arguments),
						}
					}
				}
			}

			if choice.FinishReason == "stop" || choice.FinishReason == "tool_calls" {
				eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
				return
			}
		}
	}

	// Handle scanner errors
	if err := scanner.Err(); err != nil {
		eventChan <- StreamEvent{Type: StreamEventError, Error: err}
	}

	// If we exit the loop without seeing [DONE], send done event
	eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
}

func (c *OpenAIClient) convertRequest(req *CompletionRequest) *openAIRequest {
	messages := make([]openAIMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = openAIMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
			ToolID:  msg.ToolID,
		}

		if msg.ToolCall != nil {
			messages[i].ToolCalls = []openAIToolCall{{
				ID:   msg.ToolCall.ID,
				Type: "function",
				Function: openAIFunctionCall{
					Name:      msg.ToolCall.Name,
					Arguments: string(msg.ToolCall.Arguments),
				},
			}}
		}
	}

	tools := make([]openAITool, len(req.Tools))
	for i, tool := range req.Tools {
		tools[i] = openAITool{
			Type:     tool.Type,
			Function: tool.Function,
		}
	}

	var responseFormat *openAIResponseFormat
	if req.ResponseFormat != nil {
		responseFormat = &openAIResponseFormat{
			Type:       req.ResponseFormat.Type,
			JSONSchema: req.ResponseFormat.JSONSchema,
		}
	}

	return &openAIRequest{
		Model:          req.Model,
		Messages:       messages,
		Tools:          tools,
		ResponseFormat: responseFormat,
		Temperature:    req.Temperature,
		MaxTokens:      req.MaxTokens,
		TopP:           req.TopP,
		Stop:           req.StopSequences,
		Stream:         req.Stream,
	}
}

func (c *OpenAIClient) convertResponse(resp *openAIResponse, latencyMs int64) *CompletionResponse {
	if len(resp.Choices) == 0 {
		return &CompletionResponse{
			ID:        resp.ID,
			Model:     resp.Model,
			Provider:  ProviderOpenAI,
			Created:   time.Unix(resp.Created, 0),
			LatencyMs: latencyMs,
			Usage: UsageMetrics{
				InputTokens:  resp.Usage.PromptTokens,
				OutputTokens: resp.Usage.CompletionTokens,
				TotalTokens:  resp.Usage.TotalTokens,
			},
		}
	}

	choice := resp.Choices[0]
	message := Message{
		Role:    Role(choice.Message.Role),
		Content: fmt.Sprintf("%v", choice.Message.Content),
	}

	var toolCalls []ToolCall
	for _, tc := range choice.Message.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}

	usage := UsageMetrics{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		TotalTokens:  resp.Usage.TotalTokens,
	}

	cost := c.calculateCost(usage)

	return &CompletionResponse{
		ID:           resp.ID,
		Model:        resp.Model,
		Provider:     ProviderOpenAI,
		Created:      time.Unix(resp.Created, 0),
		Message:      message,
		ToolCalls:    toolCalls,
		Usage:        usage,
		Cost:         cost,
		LatencyMs:    latencyMs,
		FinishReason: choice.FinishReason,
	}
}

func (c *OpenAIClient) calculateCost(usage UsageMetrics) CostMetrics {
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

func (c *OpenAIClient) handleErrorResponse(statusCode int, body []byte) error {
	var apiErr openAIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return fmt.Errorf("OpenAI API error (status %d): %s", statusCode, string(body))
	}

	switch statusCode {
	case http.StatusTooManyRequests:
		retryAfter := 30 * time.Second
		if retryHeader := string(body); retryHeader != "" {
			if secs, err := strconv.Atoi(retryHeader); err == nil {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
		return ErrRateLimited{Provider: ProviderOpenAI, RetryAfter: retryAfter}
	case http.StatusBadRequest:
		if apiErr.Error.Code == "context_length_exceeded" {
			return ErrContextLengthExceeded{Provider: ProviderOpenAI}
		}
	case http.StatusForbidden:
		if apiErr.Error.Type == "content_filter" {
			return ErrContentFiltered{Provider: ProviderOpenAI, Reason: apiErr.Error.Message}
		}
	}

	return fmt.Errorf("OpenAI API error: %s (type: %s, code: %s)", apiErr.Error.Message, apiErr.Error.Type, apiErr.Error.Code)
}
