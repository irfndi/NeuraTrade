// Package ai provides AI client functionality for making LLM calls and parsing tool calls.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Client is an AI client for making LLM calls with tool support.
type Client struct {
	httpClient *http.Client
	registry   *Registry
	logger     *zap.Logger
	mu         sync.RWMutex
	tools      map[string]*ToolDefinition
}

// ToolDefinition defines a tool that can be called by the AI.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Handler     ToolHandler
}

// ToolHandler is a function that handles tool execution.
type ToolHandler func(ctx context.Context, params map[string]interface{}) (interface{}, error)

// ToolCall represents a parsed tool call from LLM response.
type ToolCall struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"parameters"`
	Result interface{}
	Error  error
}

// Message represents a chat message.
type Message struct {
	Role    string     `json:"role"`
	Content string     `json:"content"`
	Tools   []ToolCall `json:"tool_calls,omitempty"`
}

// ChatRequest represents a chat completion request.
type ChatRequest struct {
	Model       string            `json:"model"`
	Messages    []Message         `json:"messages"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
}

// ChatResponse represents a chat completion response.
type ChatResponse struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Model     string     `json:"model"`
}

// ClientOption configures the AI client.
type ClientOption func(*Client)

// WithClientHTTPClient sets a custom HTTP client.
func WithClientHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithClientLogger sets the logger.
func WithClientLogger(logger *zap.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// NewClient creates a new AI client.
func NewClient(aiRegistry *Registry, opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		registry:   aiRegistry,
		logger:     zap.NewNop(),
		tools:      make(map[string]*ToolDefinition),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// RegisterTool registers a tool that can be called by the AI.
func (c *Client) RegisterTool(tool *ToolDefinition) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools[tool.Name] = tool
}

// UnregisterTool removes a tool from the registry.
func (c *Client) UnregisterTool(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tools, name)
}

// GetTool returns a tool by name.
func (c *Client) GetTool(name string) (*ToolDefinition, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tool, ok := c.tools[name]
	return tool, ok
}

// ListTools returns all registered tools.
func (c *Client) ListTools() []*ToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	tools := make([]*ToolDefinition, 0, len(c.tools))
	for _, tool := range c.tools {
		tools = append(tools, tool)
	}
	return tools
}

// Chat sends a chat request and returns the response.
func (c *Client) Chat(ctx context.Context, providerID, modelID string, messages []Message, opts ...ChatOption) (*ChatResponse, error) {
	// Get provider info
	providers, err := c.registry.GetActiveProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get providers: %w", err)
	}

	var provider *ProviderInfo
	for _, p := range providers {
		if p.ID == providerID {
			provider = &p
			break
		}
	}
	if provider == nil {
		return nil, fmt.Errorf("provider %s not found", providerID)
	}

	// Build request
	req := &ChatRequest{
		Model:    modelID,
		Messages: messages,
	}

	// Apply options
	for _, opt := range opts {
		opt(req)
	}

	// Add tools if registered
	c.mu.RLock()
	if len(c.tools) > 0 {
		toolsJSON, err := c.buildToolsJSON()
		if err != nil {
			c.mu.RUnlock()
			return nil, fmt.Errorf("failed to build tools JSON: %w", err)
		}
		req.Tools = toolsJSON
	}
	c.mu.RUnlock()

	// Make request based on provider
	switch providerID {
	case "openai":
		return c.chatOpenAI(ctx, provider, req)
	case "anthropic":
		return c.chatAnthropic(ctx, provider, req)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerID)
	}
}

// ChatOption modifies a chat request.
type ChatOption func(*ChatRequest)

// WithTemperature sets the temperature.
func WithTemperature(temp float64) ChatOption {
	return func(r *ChatRequest) {
		r.Temperature = temp
	}
}

// WithMaxTokens sets the max tokens.
func WithMaxTokens(tokens int) ChatOption {
	return func(r *ChatRequest) {
		r.MaxTokens = tokens
	}
}

// ParseToolCalls parses tool calls from raw LLM response content.
// This handles various response formats from different providers.
func ParseToolCalls(content string) ([]ToolCall, error) {
	var toolCalls []ToolCall

	// Try to parse as JSON first
	// Handle OpenAI format: {"tool_calls":[{"type":"function","function":{"name":"tool_name","arguments":"..."}}]}
	type OpenAIToolCall struct {
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
		ID string `json:"id"`
	}

	type OpenAIResponse struct {
		ToolCalls []OpenAIToolCall `json:"tool_calls"`
	}

	var openAIResp OpenAIResponse
	if err := json.Unmarshal([]byte(content), &openAIResp); err == nil {
		if len(openAIResp.ToolCalls) > 0 {
			for _, tc := range openAIResp.ToolCalls {
				var params map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
					// Try parsing as map[string]any
					if err2 := json.Unmarshal([]byte(tc.Function.Arguments), &params); err2 != nil {
						params = map[string]interface{}{"_raw": tc.Function.Arguments}
					}
				}
				toolCalls = append(toolCalls, ToolCall{
					ID:     tc.ID,
					Name:   tc.Function.Name,
					Params: params,
				})
			}
			return toolCalls, nil
		}
	}

	// Try Anthropic format: tool_use
	type AnthropicToolUse struct {
		ID    string                 `json:"id"`
		Name  string                 `json:"name"`
		Input map[string]interface{} `json:"input"`
	}

	type AnthropicResponse struct {
		ToolUses []AnthropicToolUse `json:"tool_use,omitempty"`
	}

	var anthropicResp AnthropicResponse
	if err := json.Unmarshal([]byte(content), &anthropicResp); err == nil {
		if len(anthropicResp.ToolUses) > 0 {
			for _, tu := range anthropicResp.ToolUses {
				toolCalls = append(toolCalls, ToolCall{
					ID:     tu.ID,
					Name:   tu.Name,
					Params: tu.Input,
				})
			}
			return toolCalls, nil
		}
	}

	// Try direct array format: [{"name":"...","parameters":{}}]
	type DirectToolCall struct {
		Name   string                 `json:"name"`
		Params map[string]interface{} `json:"parameters"`
		ID     string                 `json:"id"`
	}

	var directCalls []DirectToolCall
	if err := json.Unmarshal([]byte(content), &directCalls); err == nil {
		for _, tc := range directCalls {
			toolCalls = append(toolCalls, ToolCall{
				ID:     tc.ID,
				Name:   tc.Name,
				Params: tc.Params,
			})
		}
		return toolCalls, nil
	}

	return nil, fmt.Errorf("no tool calls found in response")
}

// ExecuteTool executes a tool by name with given parameters.
func (c *Client) ExecuteTool(ctx context.Context, name string, params map[string]interface{}) (interface{}, error) {
	tool, ok := c.GetTool(name)
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}

	return tool.Handler(ctx, params)
}

// ExecuteTools executes multiple tools in sequence.
func (c *Client) ExecuteTools(ctx context.Context, toolCalls []ToolCall) []ToolCall {
	results := make([]ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		result, err := c.ExecuteTool(ctx, tc.Name, tc.Params)
		results[i] = ToolCall{
			ID:     tc.ID,
			Name:   tc.Name,
			Params: tc.Params,
			Result: result,
			Error:  err,
		}
	}
	return results
}

// buildToolsJSON builds the tools JSON for the request.
func (c *Client) buildToolsJSON() ([]json.RawMessage, error) {
	tools := c.ListTools()
	result := make([]json.RawMessage, 0, len(tools))

	for _, tool := range tools {
		toolJSON := map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			},
		}
		b, err := json.Marshal(toolJSON)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}

	return result, nil
}

// chatOpenAI makes a request to OpenAI API.
func (c *Client) chatOpenAI(ctx context.Context, provider *ProviderInfo, req *ChatRequest) (*ChatResponse, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("no API key for provider: %s", provider.ID)
	}

	apiURL := "https://api.openai.com/v1/chat/completions"
	if envURL := os.Getenv("OPENAI_BASE_URL"); envURL != "" {
		apiURL = envURL + "/chat/completions"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Choices) == 0 {
		return &ChatResponse{Model: response.Model}, nil
	}

	msg := response.Choices[0].Message
	toolCalls := make([]ToolCall, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &params); err != nil {
			params = map[string]interface{}{"_raw": tc.Function.Arguments}
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:     tc.ID,
			Name:   tc.Function.Name,
			Params: params,
		})
	}

	return &ChatResponse{
		Content:   msg.Content,
		ToolCalls: toolCalls,
		Model:     response.Model,
	}, nil
}

// chatAnthropic makes a request to Anthropic API.
func (c *Client) chatAnthropic(ctx context.Context, provider *ProviderInfo, req *ChatRequest) (*ChatResponse, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("no API key for provider: %s", provider.ID)
	}

	apiURL := "https://api.anthropic.com/v1/messages"
	if envURL := os.Getenv("ANTHROPIC_BASE_URL"); envURL != "" {
		apiURL = envURL
	}

	// Convert messages to Anthropic format
	type AnthropicMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	anthropicReq := map[string]interface{}{
		"model": req.Model,
		"messages": func() []AnthropicMessage {
			msgs := make([]AnthropicMessage, len(req.Messages))
			for i, m := range req.Messages {
				msgs[i] = AnthropicMessage{
					Role:    m.Role,
					Content: m.Content,
				}
			}
			return msgs
		}(),
		"max_tokens": 4096,
	}

	if req.Temperature > 0 {
		anthropicReq["temperature"] = req.Temperature
	}

	// Add tools if present
	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			var tool map[string]interface{}
			if err := json.Unmarshal(t, &tool); err == nil {
				tools = append(tools, tool)
			}
		}
		if len(tools) > 0 {
			anthropicReq["tools"] = tools
		}
	}

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var response struct {
		Content []struct {
			Type  string                 `json:"type"`
			ID    string                 `json:"id"`
			Name  string                 `json:"name,omitempty"`
			Input map[string]interface{} `json:"input,omitempty"`
			Text  string                 `json:"text,omitempty"`
		} `json:"content"`
		Model string `json:"model"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var content string
	toolCalls := make([]ToolCall, 0)
	for _, c := range response.Content {
		switch c.Type {
		case "text":
			content += c.Text
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:     c.ID,
				Name:   c.Name,
				Params: c.Input,
			})
		}
	}

	return &ChatResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Model:     response.Model,
	}, nil
}
