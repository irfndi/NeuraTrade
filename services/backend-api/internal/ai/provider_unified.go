package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// UnifiedProviderClient provides unified API access for all providers
type UnifiedProviderClient struct {
	providerID         string
	apiKey             string
	baseURL            string
	model              string
	httpClient         *http.Client
	useAnthropicFormat bool
}

// NewUnifiedProviderClient creates a new unified provider client
func NewUnifiedProviderClient(providerID, apiKey, baseURL, model string) *UnifiedProviderClient {
	useAnthropic := ProviderUsesAnthropicFormat(providerID, baseURL)

	return &UnifiedProviderClient{
		providerID: providerID,
		apiKey:     apiKey,
		baseURL:    baseURL,
		model:      model,
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // Increased for MiniMax
		},
		useAnthropicFormat: useAnthropic,
	}
}

// Chat sends a chat completion request
func (c *UnifiedProviderClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	var body []byte
	var err error

	if c.useAnthropicFormat {
		body, err = c.buildAnthropicRequest(req)
	} else {
		body, err = c.buildOpenAIRequest(req)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	apiURL := c.baseURL
	if !c.useAnthropicFormat {
		apiURL += "/chat/completions"
	} else {
		// Anthropic format uses /messages endpoint
		apiURL += "/messages"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	if c.useAnthropicFormat {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		httpReq.Header.Set("x-api-key", c.apiKey)
	}

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
		return nil, fmt.Errorf("%s API error (status %d): %s", c.providerID, resp.StatusCode, string(respBody))
	}

	if c.useAnthropicFormat {
		return c.parseAnthropicResponse(respBody)
	}
	return c.parseOpenAIResponse(respBody)
}

func (c *UnifiedProviderClient) buildAnthropicRequest(req *ChatRequest) ([]byte, error) {
	messages := make([]map[string]interface{}, 0)
	systemPrompt := ""

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			continue
		}
		messages = append(messages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	requestBody := map[string]interface{}{
		"model":      c.model,
		"messages":   messages,
		"max_tokens": req.MaxTokens,
	}

	if systemPrompt != "" {
		requestBody["system"] = systemPrompt
	}

	return json.Marshal(requestBody)
}

func (c *UnifiedProviderClient) buildOpenAIRequest(req *ChatRequest) ([]byte, error) {
	requestBody := map[string]interface{}{
		"model":       c.model,
		"messages":    req.Messages,
		"temperature": req.Temperature,
		"max_tokens":  req.MaxTokens,
	}
	return json.Marshal(requestBody)
}

func (c *UnifiedProviderClient) parseAnthropicResponse(body []byte) (*ChatResponse, error) {
	type AnthropicResponse struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	var resp AnthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Content) == 0 {
		return nil, fmt.Errorf("no content in response")
	}

	return &ChatResponse{
		Content: resp.Content[0].Text,
		Model:   resp.Model,
	}, nil
}

func (c *UnifiedProviderClient) parseOpenAIResponse(body []byte) (*ChatResponse, error) {
	type OpenAIResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
	}

	var resp OpenAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &ChatResponse{
		Content: resp.Choices[0].Message.Content,
		Model:   resp.Model,
	}, nil
}
