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
	MLXDefaultBaseURL = "http://localhost:8080/v1"
	MLXDefaultTimeout = 300 * time.Second
)

type MLXClient struct {
	config     ClientConfig
	httpClient *http.Client
	logger     *zap.Logger
}

func NewMLXClient(config ClientConfig) *MLXClient {
	timeout := config.HTTPTimeout
	if timeout == 0 {
		timeout = MLXDefaultTimeout
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = MLXDefaultBaseURL
	}

	return &MLXClient{
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

func (c *MLXClient) SetLogger(logger *zap.Logger) {
	c.logger = logger
}

func (c *MLXClient) Provider() Provider {
	return ProviderMLX
}

func (c *MLXClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

type mlxRequest struct {
	Model       string       `json:"model"`
	Messages    []mlxMessage `json:"messages"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	TopP        *float64     `json:"top_p,omitempty"`
	Stop        []string     `json:"stop,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
}

type mlxMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type mlxResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []mlxChoice `json:"choices"`
	Usage   mlxUsage    `json:"usage"`
}

type mlxChoice struct {
	Index        int        `json:"index"`
	Message      mlxMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
	Delta        *mlxDelta  `json:"delta,omitempty"`
}

type mlxDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type mlxUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type mlxError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (c *MLXClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	startTime := time.Now()

	mlxReq := c.convertRequest(req)
	body, err := json.Marshal(mlxReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	retryPolicy := resolveTransportRetryPolicy(c.config.MaxRetries)
	result, err := executeWithTransportRetry(ctx, retryPolicy, func(attemptCtx context.Context) (*http.Response, error) {
		httpReq, reqErr := http.NewRequestWithContext(
			attemptCtx,
			http.MethodPost,
			c.config.BaseURL+"/chat/completions",
			bytes.NewReader(body),
		)
		if reqErr != nil {
			return nil, fmt.Errorf("failed to create request: %w", reqErr)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		if c.config.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
		}

		resp, doErr := c.httpClient.Do(httpReq)
		if doErr != nil {
			return nil, fmt.Errorf("failed to send request: %w", doErr)
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	if result.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(result.StatusCode, result.Headers, result.Body)
	}

	var mlxResp mlxResponse
	if err := json.Unmarshal(result.Body, &mlxResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	latencyMs := time.Since(startTime).Milliseconds()

	return c.convertResponse(&mlxResp, latencyMs), nil
}

func (c *MLXClient) Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error) {
	eventChan := make(chan StreamEvent, 100)

	streamReq := *req
	streamReq.Stream = true

	mlxReq := c.convertRequest(&streamReq)
	body, err := json.Marshal(mlxReq)
	if err != nil {
		close(eventChan)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	retryPolicy := resolveTransportRetryPolicy(c.config.MaxRetries)
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		httpReq, reqErr := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			c.config.BaseURL+"/chat/completions",
			bytes.NewReader(body),
		)
		if reqErr != nil {
			close(eventChan)
			return nil, fmt.Errorf("failed to create request: %w", reqErr)
		}

		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if c.config.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
		}

		resp, err = c.httpClient.Do(httpReq)
		if err != nil {
			if attempt >= retryPolicy.MaxRetries || !isRetryableTransportError(err) {
				close(eventChan)
				return nil, fmt.Errorf("failed to send request: %w", err)
			}
			if waitErr := waitForRetry(ctx, retryPolicy.backoffForRetry(attempt+1, 0)); waitErr != nil {
				close(eventChan)
				return nil, waitErr
			}
			continue
		}

		if resp.StatusCode == http.StatusOK {
			break
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if attempt >= retryPolicy.MaxRetries || !isRetryableHTTPStatus(resp.StatusCode) {
			close(eventChan)
			return nil, c.handleErrorResponse(resp.StatusCode, resp.Header, respBody)
		}
		retryAfter := parseRetryAfterHeader(resp.Header.Get("Retry-After"))
		if waitErr := waitForRetry(ctx, retryPolicy.backoffForRetry(attempt+1, retryAfter)); waitErr != nil {
			close(eventChan)
			return nil, waitErr
		}
	}

	go c.processStream(resp.Body, eventChan)

	return eventChan, nil
}

func (c *MLXClient) processStream(reader io.ReadCloser, eventChan chan<- StreamEvent) {
	defer close(eventChan)
	defer func() { _ = reader.Close() }()

	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonData := strings.TrimPrefix(line, "data: ")
		if jsonData == "[DONE]" {
			eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
			return
		}

		var event mlxResponse
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			eventChan <- StreamEvent{Type: StreamEventError, Error: err}
			continue
		}

		for _, choice := range event.Choices {
			if choice.Delta != nil && choice.Delta.Content != "" {
				eventChan <- StreamEvent{
					Type:  StreamEventContentDelta,
					Delta: choice.Delta.Content,
				}
			}

			if choice.FinishReason == "stop" {
				eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
				return
			}
		}

		if event.Usage.TotalTokens > 0 {
			eventChan <- StreamEvent{
				Type: StreamEventUsage,
				Usage: &UsageMetrics{
					InputTokens:  event.Usage.PromptTokens,
					OutputTokens: event.Usage.CompletionTokens,
					TotalTokens:  event.Usage.TotalTokens,
				},
			}
		}
	}

	if err := scanner.Err(); err != nil {
		eventChan <- StreamEvent{Type: StreamEventError, Error: err}
	}

	eventChan <- StreamEvent{Type: StreamEventDone, Done: true}
}

func (c *MLXClient) convertRequest(req *CompletionRequest) *mlxRequest {
	messages := make([]mlxMessage, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = mlxMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	return &mlxRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
		Stop:        req.StopSequences,
		Stream:      req.Stream,
	}
}

func (c *MLXClient) convertResponse(resp *mlxResponse, latencyMs int64) *CompletionResponse {
	if len(resp.Choices) == 0 {
		return &CompletionResponse{
			ID:        resp.ID,
			Model:     resp.Model,
			Provider:  ProviderMLX,
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
		Content: choice.Message.Content,
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
		Provider:     ProviderMLX,
		Created:      time.Unix(resp.Created, 0),
		Message:      message,
		Usage:        usage,
		Cost:         cost,
		LatencyMs:    latencyMs,
		FinishReason: choice.FinishReason,
	}
}

func (c *MLXClient) calculateCost(usage UsageMetrics) CostMetrics {
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

func (c *MLXClient) handleErrorResponse(statusCode int, headers http.Header, body []byte) error {
	var apiErr mlxError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return ProviderAPIError{
			Provider:   ProviderMLX,
			StatusCode: statusCode,
			Message:    string(body),
		}
	}

	return ProviderAPIError{
		Provider:   ProviderMLX,
		StatusCode: statusCode,
		Message:    apiErr.Error.Message,
		Type:       apiErr.Error.Type,
		Code:       apiErr.Error.Code,
		RetryAfter: parseRetryAfterHeader(headers.Get("Retry-After")),
	}
}
