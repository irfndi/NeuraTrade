package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/shopspring/decimal"
)

func TestNewOpenAIClient(t *testing.T) {
	config := ClientConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.openai.com/v1",
	}

	client := NewOpenAIClient(config)

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.Provider() != ProviderOpenAI {
		t.Errorf("Expected provider %s, got %s", ProviderOpenAI, client.Provider())
	}
}

func TestOpenAIClientComplete(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")

		resp := openAIResponse{
			ID:      "test-id",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "gpt-4",
			Choices: []openAIChoice{
				{
					Index: 0,
					Message: openAIMessage{
						Role:    "assistant",
						Content: "Hello, world!",
					},
					FinishReason: "stop",
				},
			},
			Usage: openAIUsage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIClient(ClientConfig{
		APIKey:      "test-key",
		BaseURL:     server.URL,
		HTTPTimeout: defaultTimeout,
	})

	req := &CompletionRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
		},
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if authHeader != "Bearer test-key" {
		t.Error("Expected Authorization header to be set")
	}
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if resp.Message.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", resp.Message.Content)
	}

	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Expected 15 tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestOpenAIClientToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openAIResponse{
			ID:      "test-id",
			Model:   "gpt-4",
			Created: 1234567890,
			Choices: []openAIChoice{
				{
					Index: 0,
					Message: openAIMessage{
						Role: "assistant",
						ToolCalls: []openAIToolCall{
							{
								ID:   "call-123",
								Type: "function",
								Function: openAIFunctionCall{
									Name:      "get_weather",
									Arguments: `{"location": "San Francisco"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: openAIUsage{
				PromptTokens:     20,
				CompletionTokens: 10,
				TotalTokens:      30,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOpenAIClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	req := &CompletionRequest{
		Model: "gpt-4",
		Messages: []Message{
			{Role: RoleUser, Content: "What's the weather?"},
		},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: FunctionDefinition{
					Name:        "get_weather",
					Description: "Get weather for a location",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"location": map[string]string{"type": "string"},
						},
					},
				},
			},
		},
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(resp.ToolCalls))
	}

	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got '%s'", resp.ToolCalls[0].Name)
	}

	if resp.FinishReason != "tool_calls" {
		t.Errorf("Expected finish reason 'tool_calls', got '%s'", resp.FinishReason)
	}
}

func TestNewAnthropicClient(t *testing.T) {
	config := ClientConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.anthropic.com/v1",
	}

	client := NewAnthropicClient(config)

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.Provider() != ProviderAnthropic {
		t.Errorf("Expected provider %s, got %s", ProviderAnthropic, client.Provider())
	}
}

func TestAnthropicClientComplete(t *testing.T) {
	var apiKeyHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKeyHeader = r.Header.Get("x-api-key")

		resp := anthropicResponse{
			ID:    "msg-123",
			Type:  "message",
			Role:  "assistant",
			Model: "claude-3-opus",
			Content: []anthropicContent{
				{
					Type: "text",
					Text: "Hello from Claude!",
				},
			},
			StopReason: "end_turn",
			Usage: anthropicUsage{
				InputTokens:  15,
				OutputTokens: 8,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewAnthropicClient(ClientConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	req := &CompletionRequest{
		Model: "claude-3-opus",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
		},
		MaxTokens: 1024,
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if apiKeyHeader != "test-key" {
		t.Error("Expected x-api-key header to be set")
	}

	if resp.Message.Content != "Hello from Claude!" {
		t.Errorf("Expected content 'Hello from Claude!', got '%s'", resp.Message.Content)
	}

	if resp.Usage.InputTokens != 15 {
		t.Errorf("Expected 15 input tokens, got %d", resp.Usage.InputTokens)
	}
}

func TestNewMLXClient(t *testing.T) {
	config := ClientConfig{
		BaseURL: "http://localhost:8080/v1",
	}

	client := NewMLXClient(config)

	if client == nil {
		t.Fatal("Expected client to be created")
	}

	if client.Provider() != ProviderMLX {
		t.Errorf("Expected provider %s, got %s", ProviderMLX, client.Provider())
	}
}

func TestMLXClientComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mlxResponse{
			ID:      "mlx-123",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "mlx-model",
			Choices: []mlxChoice{
				{
					Index: 0,
					Message: mlxMessage{
						Role:    "assistant",
						Content: "Local inference response",
					},
					FinishReason: "stop",
				},
			},
			Usage: mlxUsage{
				PromptTokens:     12,
				CompletionTokens: 6,
				TotalTokens:      18,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewMLXClient(ClientConfig{
		BaseURL: server.URL,
	})

	req := &CompletionRequest{
		Model: "mlx-model",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
		},
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if resp.Message.Content != "Local inference response" {
		t.Errorf("Expected content 'Local inference response', got '%s'", resp.Message.Content)
	}
}

func TestClientFactory(t *testing.T) {
	registry := &ai.Registry{}
	factory := NewClientFactory(registry)

	factory.Configure(ProviderOpenAI, ClientConfig{APIKey: "test-openai"})
	factory.Configure(ProviderAnthropic, ClientConfig{APIKey: "test-anthropic"})

	ctx := context.Background()

	openaiClient, err := factory.Create(ctx, ProviderOpenAI)
	if err != nil {
		t.Fatalf("Failed to create OpenAI client: %v", err)
	}
	if openaiClient.Provider() != ProviderOpenAI {
		t.Errorf("Expected OpenAI provider")
	}

	anthropicClient, err := factory.Create(ctx, ProviderAnthropic)
	if err != nil {
		t.Fatalf("Failed to create Anthropic client: %v", err)
	}
	if anthropicClient.Provider() != ProviderAnthropic {
		t.Errorf("Expected Anthropic provider")
	}
}

func TestCostCalculation(t *testing.T) {
	cost := CostMetrics{
		InputCost:  decimal.NewFromFloat(0.01),
		OutputCost: decimal.NewFromFloat(0.03),
		TotalCost:  decimal.NewFromFloat(0.04),
	}

	if cost.InputCost.String() != "0.01" {
		t.Errorf("Expected input cost 0.01, got %s", cost.InputCost.String())
	}

	if cost.TotalCost.String() != "0.04" {
		t.Errorf("Expected total cost 0.04, got %s", cost.TotalCost.String())
	}
}

func TestBuildToolDefinition(t *testing.T) {
	params := map[string]FunctionParam{
		"location": {
			Type:        "string",
			Description: "City name",
			Required:    true,
		},
	}

	tool := BuildToolDefinition("get_weather", "Get weather", params, []string{"location"})

	if tool.Function.Name != "get_weather" {
		t.Errorf("Expected function name 'get_weather', got '%s'", tool.Function.Name)
	}

	if tool.Type != "function" {
		t.Errorf("Expected tool type 'function', got '%s'", tool.Type)
	}
}

func TestConversationBuilder(t *testing.T) {
	builder := NewConversationBuilder("You are helpful.")
	builder.
		AddUser("Hello").
		AddAssistant("Hi there!").
		AddUser("How are you?")

	messages := builder.Build()

	if len(messages) != 4 {
		t.Errorf("Expected 4 messages, got %d", len(messages))
	}

	if messages[0].Role != RoleSystem {
		t.Error("First message should be system")
	}

	if messages[1].Role != RoleUser {
		t.Error("Second message should be user")
	}
}

const defaultTimeout = 30 * time.Second
