package llm

import (
	"encoding/json"
	"fmt"

	"github.com/irfndi/neuratrade/internal/skill"
)

// BuildToolDefinitions converts skill definitions into LLM tool definitions
// for function calling.
func BuildToolDefinitions(skills []*skill.Skill) []ToolDefinition {
	var tools []ToolDefinition

	for _, s := range skills {
		params := s.Parameters
		if params == nil {
			params = make(map[string]skill.Param)
		}

		properties := make(map[string]interface{})
		required := []string{}

		for name, param := range params {
			prop := map[string]interface{}{
				"type":        param.Type,
				"description": param.Description,
			}

			if len(param.Enum) > 0 {
				prop["enum"] = param.Enum
			}

			if param.Default != nil {
				prop["default"] = param.Default
			}

			properties[name] = prop

			if param.Required {
				required = append(required, name)
			}
		}

		schema := map[string]interface{}{
			"type":       "object",
			"properties": properties,
		}

		if len(required) > 0 {
			schema["required"] = required
		}

		tools = append(tools, ToolDefinition{
			Type: "function",
			Function: FunctionDefinition{
				Name:        s.ID,
				Description: s.Description,
				Parameters:  schema,
			},
		})
	}

	return tools
}

// BuildToolDefinition creates a single tool definition from parameters.
func BuildToolDefinition(name, description string, params map[string]FunctionParam, required []string) ToolDefinition {
	properties := make(map[string]interface{})

	for pname, param := range params {
		prop := map[string]interface{}{
			"type":        param.Type,
			"description": param.Description,
		}

		if len(param.Enum) > 0 {
			prop["enum"] = param.Enum
		}

		if param.Default != nil {
			prop["default"] = param.Default
		}

		properties[pname] = prop
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return ToolDefinition{
		Type: "function",
		Function: FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  schema,
		},
	}
}

type FunctionParam struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
}

type JSONSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// BuildJSONResponseFormat creates a ResponseFormat for structured JSON output.
func BuildJSONResponseFormat(schema *JSONSchema) *ResponseFormat {
	if schema == nil {
		return &ResponseFormat{Type: "json_object"}
	}

	return &ResponseFormat{
		Type: "json_schema",
		JSONSchema: map[string]interface{}{
			"name":   "response",
			"strict": true,
			"schema": schema,
		},
	}
}

// ParseStructuredOutput parses a structured JSON response into type T.
func ParseStructuredOutput[T any](resp *CompletionResponse) (T, error) {
	var result T

	if resp == nil || resp.Message.Content == "" {
		return result, fmt.Errorf("empty response")
	}

	if err := json.Unmarshal([]byte(resp.Message.Content), &result); err != nil {
		return result, fmt.Errorf("failed to parse structured output: %w", err)
	}

	return result, nil
}

// ParseToolCallArguments parses tool call JSON arguments into type T.
func ParseToolCallArguments[T any](tc ToolCall) (T, error) {
	var result T

	if len(tc.Arguments) == 0 {
		return result, fmt.Errorf("empty arguments")
	}

	if err := json.Unmarshal(tc.Arguments, &result); err != nil {
		return result, fmt.Errorf("failed to parse tool call arguments: %w", err)
	}

	return result, nil
}

// BuildSystemPrompt constructs a system prompt with tool descriptions.
func BuildSystemPrompt(skills []*skill.Skill, context string) string {
	prompt := "You are an AI trading assistant with access to the following tools:\n\n"

	for _, s := range skills {
		prompt += fmt.Sprintf("- **%s**: %s\n", s.ID, s.Description)
	}

	if context != "" {
		prompt += "\n\n## Context\n\n" + context
	}

	prompt += "\n\nUse the available tools to accomplish the user's request. Always think step by step."

	return prompt
}

// BuildConversationHistory trims history to fit within token budget.
func BuildConversationHistory(messages []Message, maxTokens int) []Message {
	if len(messages) <= 10 {
		return messages
	}

	systemMsg := messages[0]
	remaining := messages[1:]
	tokenBudget := maxTokens - 1000 - estimateTokens(systemMsg.Content)

	var recent []Message
	tokenCount := 0
	for i := len(remaining) - 1; i >= 0 && tokenCount < tokenBudget; i-- {
		tokens := estimateTokens(remaining[i].Content)
		if tokenCount+tokens > tokenBudget {
			break
		}
		tokenCount += tokens
		recent = append(recent, remaining[i])
	}

	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}

	return append([]Message{systemMsg}, recent...)
}

// estimateTokens estimates token count using 4 chars per token heuristic.
func estimateTokens(text string) int {
	return len(text) / 4
}

type ConversationBuilder struct {
	messages []Message
}

// NewConversationBuilder creates a builder initialized with a system prompt.
func NewConversationBuilder(systemPrompt string) *ConversationBuilder {
	return &ConversationBuilder{
		messages: []Message{
			{Role: RoleSystem, Content: systemPrompt},
		},
	}
}

// AddUser appends a user message.
func (b *ConversationBuilder) AddUser(content string) *ConversationBuilder {
	b.messages = append(b.messages, Message{Role: RoleUser, Content: content})
	return b
}

// AddAssistant appends an assistant message.
func (b *ConversationBuilder) AddAssistant(content string) *ConversationBuilder {
	b.messages = append(b.messages, Message{Role: RoleAssistant, Content: content})
	return b
}

// AddToolResult appends a tool result message.
func (b *ConversationBuilder) AddToolResult(toolID, content string) *ConversationBuilder {
	b.messages = append(b.messages, Message{Role: RoleTool, Content: content, ToolID: toolID})
	return b
}

// AddToolCall appends an assistant message with a tool call.
func (b *ConversationBuilder) AddToolCall(toolCall ToolCall, content string) *ConversationBuilder {
	b.messages = append(b.messages, Message{
		Role:     RoleAssistant,
		Content:  content,
		ToolCall: &toolCall,
	})
	return b
}

// Build returns the constructed message slice.
func (b *ConversationBuilder) Build() []Message {
	return b.messages
}
