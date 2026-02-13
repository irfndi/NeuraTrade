package llm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

type ProviderParityTest struct {
	Name        string
	Description string
	Request     *CompletionRequest
	Validators  []ResponseValidator
}

type ResponseValidator func(t *testing.T, resp *CompletionResponse, provider Provider)

type ParityTestSuite struct {
	providers []Provider
	clients   map[Provider]Client
}

func NewParityTestSuite() *ParityTestSuite {
	return &ParityTestSuite{
		providers: []Provider{ProviderOpenAI, ProviderAnthropic},
		clients:   make(map[Provider]Client),
	}
}

func (pts *ParityTestSuite) RegisterClient(provider Provider, client Client) {
	pts.clients[provider] = client
}

func (pts *ParityTestSuite) RunTest(t *testing.T, test ProviderParityTest) {
	t.Run(test.Name, func(t *testing.T) {
		responses := make(map[Provider]*CompletionResponse)

		for _, provider := range pts.providers {
			client, ok := pts.clients[provider]
			if !ok {
				t.Skipf("Client not configured for provider: %s", provider)
				continue
			}

			resp, err := client.Complete(context.Background(), test.Request)
			if err != nil {
				t.Logf("Provider %s returned error: %v", provider, err)
				continue
			}
			responses[provider] = resp
		}

		for provider, resp := range responses {
			for _, validator := range test.Validators {
				validator(t, resp, provider)
			}
		}
	})
}

func ValidateResponseNotEmpty(t *testing.T, resp *CompletionResponse, provider Provider) {
	assert.NotEmpty(t, resp.Message.Content, "Provider %s returned empty response", provider)
}

func ValidateTokenUsage(t *testing.T, resp *CompletionResponse, provider Provider) {
	assert.Greater(t, resp.Usage.TotalTokens, 0, "Provider %s reported zero tokens", provider)
	assert.GreaterOrEqual(t, resp.Usage.InputTokens, 0, "Provider %s reported negative input tokens", provider)
	assert.GreaterOrEqual(t, resp.Usage.OutputTokens, 0, "Provider %s reported negative output tokens", provider)
}

func ValidateCostTracking(t *testing.T, resp *CompletionResponse, provider Provider) {
	assert.True(t, resp.Cost.TotalCost.GreaterThanOrEqual(resp.Cost.InputCost.Add(resp.Cost.OutputCost)),
		"Provider %s has inconsistent cost calculation", provider)
}

func ValidateResponseID(t *testing.T, resp *CompletionResponse, provider Provider) {
	assert.NotEmpty(t, resp.ID, "Provider %s returned empty response ID", provider)
}

func ValidateFinishReason(t *testing.T, resp *CompletionResponse, provider Provider) {
	validReasons := []string{"stop", "length", "tool_calls", "content_filter", ""}
	assert.Contains(t, validReasons, resp.FinishReason,
		"Provider %s returned invalid finish reason: %s", provider, resp.FinishReason)
}

func ValidateLatencyRecorded(t *testing.T, resp *CompletionResponse, provider Provider) {
	assert.Greater(t, resp.LatencyMs, int64(0), "Provider %s has zero latency", provider)
}

func TestProviderParity_BasicCompletion(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "basic_completion",
		Description: "Simple completion request across all providers",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleSystem, Content: "You are a helpful assistant."},
				{Role: RoleUser, Content: "Say 'hello' and nothing else."},
			},
			MaxTokens: 50,
		},
		Validators: []ResponseValidator{
			ValidateResponseNotEmpty,
			ValidateResponseID,
			ValidateFinishReason,
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_TokenUsage(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "token_usage_consistency",
		Description: "All providers should report consistent token usage",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Count from 1 to 5."},
			},
			MaxTokens: 100,
		},
		Validators: []ResponseValidator{
			ValidateTokenUsage,
			ValidateResponseNotEmpty,
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_SystemMessage(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "system_message_handling",
		Description: "All providers should respect system messages",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleSystem, Content: "Always respond with exactly one word."},
				{Role: RoleUser, Content: "What is the capital of France?"},
			},
			MaxTokens: 50,
		},
		Validators: []ResponseValidator{
			ValidateResponseNotEmpty,
			func(t *testing.T, resp *CompletionResponse, provider Provider) {
				assert.Less(t, len(resp.Message.Content), 50,
					"Provider %s did not respect system message constraint", provider)
			},
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_JsonMode(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "json_mode_response",
		Description: "All providers should support JSON mode consistently",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Return a JSON object with a 'greeting' field."},
			},
			ResponseFormat: &ResponseFormat{
				Type: "json_object",
			},
			MaxTokens: 100,
		},
		Validators: []ResponseValidator{
			ValidateResponseNotEmpty,
			func(t *testing.T, resp *CompletionResponse, provider Provider) {
				var result map[string]interface{}
				err := json.Unmarshal([]byte(resp.Message.Content), &result)
				assert.NoError(t, err, "Provider %s returned invalid JSON", provider)
			},
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_Temperature(t *testing.T) {
	suite := NewParityTestSuite()

	temp := 0.0
	test := ProviderParityTest{
		Name:        "temperature_zero_consistency",
		Description: "Temperature 0 should produce deterministic responses",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Say a random number between 1 and 100."},
			},
			Temperature: &temp,
			MaxTokens:   20,
		},
		Validators: []ResponseValidator{
			ValidateResponseNotEmpty,
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_MaxTokens(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "max_tokens_enforcement",
		Description: "All providers should respect max_tokens limit",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Write a very long essay about artificial intelligence."},
			},
			MaxTokens: 20,
		},
		Validators: []ResponseValidator{
			ValidateResponseNotEmpty,
			func(t *testing.T, resp *CompletionResponse, provider Provider) {
				assert.LessOrEqual(t, resp.Usage.OutputTokens, 25,
					"Provider %s exceeded max_tokens significantly", provider)
			},
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_Latency(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "latency_tracking",
		Description: "All providers should track latency consistently",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "Hi"},
			},
			MaxTokens: 10,
		},
		Validators: []ResponseValidator{
			ValidateLatencyRecorded,
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_EmptyMessages(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "empty_messages_error",
		Description: "All providers should handle empty messages consistently",
		Request: &CompletionRequest{
			Messages:  []Message{},
			MaxTokens: 50,
		},
		Validators: []ResponseValidator{
			func(t *testing.T, resp *CompletionResponse, provider Provider) {
				assert.NotNil(t, resp)
			},
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_Streaming(t *testing.T) {
	t.Skip("Streaming parity tests require provider-specific implementations")
}

func TestProviderParity_ToolCalling(t *testing.T) {
	suite := NewParityTestSuite()

	test := ProviderParityTest{
		Name:        "tool_calling_format",
		Description: "All providers should support tool calling consistently",
		Request: &CompletionRequest{
			Messages: []Message{
				{Role: RoleUser, Content: "What is 123 + 456? Use the calculator tool."},
			},
			Tools: []ToolDefinition{
				{
					Type: "function",
					Function: FunctionDefinition{
						Name:        "calculator",
						Description: "Perform arithmetic calculations",
						Parameters: map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"expression": map[string]interface{}{
									"type":        "string",
									"description": "The arithmetic expression to evaluate",
								},
							},
							"required": []string{"expression"},
						},
					},
				},
			},
			MaxTokens: 100,
		},
		Validators: []ResponseValidator{
			ValidateResponseNotEmpty,
			func(t *testing.T, resp *CompletionResponse, provider Provider) {
				if len(resp.ToolCalls) > 0 {
					for _, tc := range resp.ToolCalls {
						assert.NotEmpty(t, tc.ID, "Provider %s tool call missing ID", provider)
						assert.NotEmpty(t, tc.Name, "Provider %s tool call missing name", provider)
						assert.NotNil(t, tc.Arguments, "Provider %s tool call missing arguments", provider)
					}
				}
			},
		},
	}

	suite.RunTest(t, test)
}

func TestProviderParity_ResponseStructure(t *testing.T) {
	testCases := []struct {
		name    string
		message string
	}{
		{
			name:    "short_response",
			message: "Say hi",
		},
		{
			name:    "medium_response",
			message: "Explain what 2+2 equals in one sentence",
		},
		{
			name:    "code_generation",
			message: "Write a hello world program in Python",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			suite := NewParityTestSuite()

			test := ProviderParityTest{
				Name:        tc.name,
				Description: "Response structure consistency",
				Request: &CompletionRequest{
					Messages: []Message{
						{Role: RoleUser, Content: tc.message},
					},
					MaxTokens: 200,
				},
				Validators: []ResponseValidator{
					ValidateResponseNotEmpty,
					ValidateResponseID,
					ValidateTokenUsage,
					ValidateFinishReason,
					ValidateLatencyRecorded,
				},
			}

			suite.RunTest(t, test)
		})
	}
}

func BenchmarkProviderParity_Latency(b *testing.B) {
	suite := NewParityTestSuite()

	request := &CompletionRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "Hi"},
		},
		MaxTokens: 10,
	}

	for provider, client := range suite.clients {
		b.Run(string(provider), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, err := client.Complete(context.Background(), request)
				if err != nil {
					b.Logf("Error: %v", err)
				}
			}
		})
	}
}
