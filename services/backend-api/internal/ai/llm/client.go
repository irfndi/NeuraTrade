// Package llm provides a unified interface for LLM inference across multiple providers.
// It supports OpenAI, Anthropic, Google, Mistral, and MLX local inference with structured output and tool calling.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/shopspring/decimal"
)

// Provider represents an LLM provider type
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderGoogle    Provider = "google"
	ProviderMistral   Provider = "mistral"
	ProviderMLX       Provider = "mlx"
)

// Role represents a message role
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a chat message
type Message struct {
	Role     Role      `json:"role"`
	Content  string    `json:"content"`
	ToolID   string    `json:"tool_id,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
}

// ToolCall represents a tool call from the LLM
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDefinition represents a tool that can be called by the LLM
type ToolDefinition struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition defines a function tool
type FunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ResponseFormat specifies how the LLM should format its response
type ResponseFormat struct {
	Type       string                 `json:"type"` // "text" or "json_object" or "json_schema"
	JSONSchema map[string]interface{} `json:"json_schema,omitempty"`
}

// CompletionRequest represents a completion request
type CompletionRequest struct {
	Messages       []Message         `json:"messages"`
	Model          string            `json:"model"`
	Tools          []ToolDefinition  `json:"tools,omitempty"`
	ResponseFormat *ResponseFormat   `json:"response_format,omitempty"`
	Temperature    *float64          `json:"temperature,omitempty"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	TopP           *float64          `json:"top_p,omitempty"`
	StopSequences  []string          `json:"stop,omitempty"`
	Stream         bool              `json:"stream,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// CompletionResponse represents a completion response
type CompletionResponse struct {
	ID        string     `json:"id"`
	Model     string     `json:"model"`
	Provider  Provider   `json:"provider"`
	Created   time.Time  `json:"created"`
	Message   Message    `json:"message"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Usage metrics
	Usage UsageMetrics `json:"usage"`

	// Cost tracking
	Cost CostMetrics `json:"cost"`

	// Latency tracking
	LatencyMs int64 `json:"latency_ms"`

	// Finish reason: "stop", "tool_calls", "length", "content_filter"
	FinishReason string `json:"finish_reason"`
}

// UsageMetrics tracks token usage
type UsageMetrics struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// CostMetrics tracks cost information
type CostMetrics struct {
	InputCost  decimal.Decimal `json:"input_cost"`
	OutputCost decimal.Decimal `json:"output_cost"`
	TotalCost  decimal.Decimal `json:"total_cost"`
}

// ClientConfig holds configuration for LLM clients
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	HTTPTimeout time.Duration
	MaxRetries  int
	ModelInfo   *ai.ModelInfo // Optional: for cost calculation
}

// Client is the interface for LLM inference clients
type Client interface {
	// Complete sends a completion request and returns the response
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

	// Stream sends a streaming completion request
	Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamEvent, error)

	// Provider returns the provider type
	Provider() Provider

	// Close releases any resources
	Close() error
}

// StreamEvent represents a streaming event
type StreamEvent struct {
	Type     StreamEventType `json:"type"`
	Delta    string          `json:"delta,omitempty"`
	ToolCall *ToolCall       `json:"tool_call,omitempty"`
	Done     bool            `json:"done,omitempty"`
	Error    error           `json:"error,omitempty"`
	Usage    *UsageMetrics   `json:"usage,omitempty"`
}

// StreamEventType represents the type of stream event
type StreamEventType string

const (
	StreamEventContentDelta  StreamEventType = "content_delta"
	StreamEventToolCallDelta StreamEventType = "tool_call_delta"
	StreamEventDone          StreamEventType = "done"
	StreamEventError         StreamEventType = "error"
	StreamEventUsage         StreamEventType = "usage"
)

// ClientFactory creates LLM clients
type ClientFactory struct {
	registry *ai.Registry
	configs  map[Provider]ClientConfig
}

// NewClientFactory creates a new client factory
func NewClientFactory(registry *ai.Registry) *ClientFactory {
	return &ClientFactory{
		registry: registry,
		configs:  make(map[Provider]ClientConfig),
	}
}

// Configure sets configuration for a provider
func (f *ClientFactory) Configure(provider Provider, config ClientConfig) {
	f.configs[provider] = config
}

// Create creates a client for the specified provider
func (f *ClientFactory) Create(ctx context.Context, provider Provider) (Client, error) {
	config, ok := f.configs[provider]
	if !ok {
		return nil, ErrProviderNotConfigured{Provider: provider}
	}

	switch provider {
	case ProviderOpenAI:
		return NewOpenAIClient(config), nil
	case ProviderAnthropic:
		return NewAnthropicClient(config), nil
	case ProviderMLX:
		return NewMLXClient(config), nil
	case ProviderGoogle:
		return nil, ErrUnsupportedProvider{Provider: provider}
	case ProviderMistral:
		return nil, ErrUnsupportedProvider{Provider: provider}
	default:
		return nil, ErrUnsupportedProvider{Provider: provider}
	}
}

// CreateForModel creates a client for a specific model, auto-detecting the provider
func (f *ClientFactory) CreateForModel(ctx context.Context, modelID string) (Client, *ai.ModelInfo, error) {
	modelInfo, err := f.registry.FindModel(ctx, modelID)
	if err != nil {
		return nil, nil, err
	}

	provider := Provider(modelInfo.ProviderID)
	client, err := f.Create(ctx, provider)
	if err != nil {
		return nil, nil, err
	}

	return client, modelInfo, nil
}

// Error types

// ErrProviderNotConfigured indicates a provider is not configured
type ErrProviderNotConfigured struct {
	Provider Provider
}

func (e ErrProviderNotConfigured) Error() string {
	return "provider not configured: " + string(e.Provider)
}

// ErrUnsupportedProvider indicates an unsupported provider
type ErrUnsupportedProvider struct {
	Provider Provider
}

func (e ErrUnsupportedProvider) Error() string {
	return "unsupported provider: " + string(e.Provider)
}

// ErrRateLimited indicates rate limiting from the provider
type ErrRateLimited struct {
	Provider   Provider
	RetryAfter time.Duration
}

func (e ErrRateLimited) Error() string {
	return "rate limited by " + string(e.Provider) + ", retry after " + e.RetryAfter.String()
}

// ErrContextLengthExceeded indicates the context length was exceeded
type ErrContextLengthExceeded struct {
	Provider    Provider
	MaxTokens   int
	InputTokens int
}

func (e ErrContextLengthExceeded) Error() string {
	return fmt.Sprintf("context length exceeded: max %d, input %d", e.MaxTokens, e.InputTokens)
}

// ErrContentFiltered indicates content was filtered by the provider
type ErrContentFiltered struct {
	Provider Provider
	Reason   string
	Category string
}

func (e ErrContentFiltered) Error() string {
	return "content filtered by " + string(e.Provider) + ": " + e.Reason
}
