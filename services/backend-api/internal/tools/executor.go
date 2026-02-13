// Package tools provides tool execution functionality for AI function calling.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"go.uber.org/zap"
)

// Executor handles tool execution and registration.
type Executor struct {
	mu     sync.RWMutex
	logger *zap.Logger
	tools  map[string]*RegisteredTool
}

// RegisteredTool represents a registered tool with its handler.
type RegisteredTool struct {
	Name        string
	Description string
	Params      map[string]ParamSpec
	Handler     interface{}
	fn          reflect.Value
	fnType      reflect.Type
}

// ParamSpec defines the specification for a tool parameter.
type ParamSpec struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default,omitempty"`
}

// ToolCall represents a parsed tool call from LLM response.
type ToolCall struct {
	ID     string
	Name   string
	Params map[string]interface{}
}

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	ID     string
	Name   string
	Result interface{}
	Error  error
}

// NewExecutor creates a new tool executor.
func NewExecutor(logger *zap.Logger) *Executor {
	return &Executor{
		logger: logger,
		tools:  make(map[string]*RegisteredTool),
	}
}

// Register registers a tool with its handler function.
// The handler should be a function with the following signature:
// func(ctx context.Context, params map[string]interface{}) (interface{}, error)
// Or with concrete parameter types.
func (e *Executor) Register(name, description string, params map[string]ParamSpec, handler interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.tools[name]; exists {
		return fmt.Errorf("tool already registered: %s", name)
	}

	fn := reflect.ValueOf(handler)
	if fn.Kind() != reflect.Func {
		return fmt.Errorf("handler must be a function")
	}

	e.tools[name] = &RegisteredTool{
		Name:        name,
		Description: description,
		Params:      params,
		Handler:     handler,
		fn:          fn,
		fnType:      fn.Type(),
	}

	if e.logger != nil {
		e.logger.Debug("registered tool", zap.String("name", name))
	}
	return nil
}

// Unregister removes a tool from the executor.
func (e *Executor) Unregister(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.tools[name]; !exists {
		return fmt.Errorf("tool not found: %s", name)
	}

	delete(e.tools, name)
	return nil
}

// Get returns a tool by name.
func (e *Executor) Get(name string) (*RegisteredTool, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tool, ok := e.tools[name]
	return tool, ok
}

// List returns all registered tool names.
func (e *Executor) List() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	names := make([]string, 0, len(e.tools))
	for name := range e.tools {
		names = append(names, name)
	}
	return names
}

// GetToolSchema returns the JSON schema for all registered tools.
func (e *Executor) GetToolSchema() []map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	schemas := make([]map[string]interface{}, 0, len(e.tools))
	for _, tool := range e.tools {
		schemas = append(schemas, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters": map[string]interface{}{
					"type":       "object",
					"properties": tool.Params,
					"required":   requiredParams(tool.Params),
				},
			},
		})
	}
	return schemas
}

// requiredParams extracts required parameter names from the param spec.
func requiredParams(params map[string]ParamSpec) []string {
	var required []string
	for name, spec := range params {
		if spec.Required {
			required = append(required, name)
		}
	}
	return required
}

// Execute runs a tool call and returns the result.
func (e *Executor) Execute(ctx context.Context, call ToolCall) ToolResult {
	tool, ok := e.Get(call.Name)
	if !ok {
		return ToolResult{
			ID:    call.ID,
			Name:  call.Name,
			Error: fmt.Errorf("tool not found: %s", call.Name),
		}
	}

	// Validate required parameters
	for name, spec := range tool.Params {
		if spec.Required {
			if _, exists := call.Params[name]; !exists {
				return ToolResult{
					ID:    call.ID,
					Name:  call.Name,
					Error: fmt.Errorf("missing required parameter: %s", name),
				}
			}
		}
	}

	// Convert params to handler signature
	args, err := e.convertParams(ctx, call.Params, tool)
	if err != nil {
		return ToolResult{
			ID:    call.ID,
			Name:  call.Name,
			Error: fmt.Errorf("failed to convert parameters: %w", err),
		}
	}

	// Call the handler
	results := tool.fn.Call(args)

	var result interface{}
	var errVal error

	switch len(results) {
	case 1:
		if results[0].IsValid() {
			result = results[0].Interface()
		}
	case 2:
		first := results[0]
		second := results[1]
		if first.IsValid() && !first.IsNil() {
			if err, ok := first.Interface().(error); ok {
				errVal = err
			}
		}
		if second.IsValid() {
			result = second.Interface()
		}
	}

	return ToolResult{
		ID:     call.ID,
		Name:   call.Name,
		Result: result,
		Error:  errVal,
	}
}

// ExecuteMultiple runs multiple tool calls in sequence.
func (e *Executor) ExecuteMultiple(ctx context.Context, calls []ToolCall) []ToolResult {
	results := make([]ToolResult, len(calls))
	for i, call := range calls {
		results[i] = e.Execute(ctx, call)
	}
	return results
}

// convertParams converts the map parameters to the handler function's parameter types.
func (e *Executor) convertParams(ctx context.Context, params map[string]interface{}, tool *RegisteredTool) ([]reflect.Value, error) {
	numParams := tool.fnType.NumIn()
	args := make([]reflect.Value, 0, numParams)

	// First parameter must be context.Context
	if numParams > 0 {
		firstParam := tool.fnType.In(0)
		if firstParam.Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
			args = append(args, reflect.ValueOf(ctx))
		}
	}

	// Convert remaining params
	for i := 1; i < numParams; i++ {
		paramType := tool.fnType.In(i)
		paramName := ""

		if len(tool.Params) > 0 {
			idx := 0
			for name := range tool.Params {
				if idx == i-1 {
					paramName = name
					break
				}
				idx++
			}
			if paramName == "" {
				names := make([]string, 0, len(tool.Params))
				for name := range tool.Params {
					names = append(names, name)
				}
				if i-1 < len(names) {
					paramName = names[i-1]
				}
			}
		}

		var paramValue interface{}
		if paramName != "" {
			if v, exists := params[paramName]; exists {
				paramValue = v
			}
		} else if len(params) > 0 {
			for _, v := range params {
				paramValue = v
				break
			}
		}

		arg, err := convertValue(paramValue, paramType)
		if err != nil {
			return nil, fmt.Errorf("failed to convert parameter %s: %w", paramName, err)
		}
		args = append(args, arg)
	}

	return args, nil
}

// convertValue converts a value to the target type.
func convertValue(value interface{}, targetType reflect.Type) (reflect.Value, error) {
	if value == nil {
		return reflect.Zero(targetType), nil
	}

	// Handle string
	if str, ok := value.(string); ok {
		switch targetType.Kind() {
		case reflect.String:
			return reflect.ValueOf(str), nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			var i int64
			if _, err := fmt.Sscanf(str, "%d", &i); err != nil {
				return reflect.Zero(targetType), fmt.Errorf("failed to parse int: %w", err)
			}
			return reflect.ValueOf(int64(i)).Convert(targetType), nil
		case reflect.Float32, reflect.Float64:
			var f float64
			if _, err := fmt.Sscanf(str, "%f", &f); err != nil {
				return reflect.Zero(targetType), fmt.Errorf("failed to parse float: %w", err)
			}
			return reflect.ValueOf(f).Convert(targetType), nil
		}
	}

	// Handle numeric types
	switch value.(type) {
	case float64:
		val := value.(float64)
		switch targetType.Kind() {
		case reflect.Float32, reflect.Float64:
			return reflect.ValueOf(val).Convert(targetType), nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return reflect.ValueOf(int64(val)).Convert(targetType), nil
		}
	case int, int8, int16, int32, int64:
		val := reflect.ValueOf(value).Int()
		switch targetType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return reflect.ValueOf(val).Convert(targetType), nil
		case reflect.Float32, reflect.Float64:
			return reflect.ValueOf(float64(val)).Convert(targetType), nil
		}
	case bool:
		return reflect.ValueOf(value.(bool)).Convert(targetType), nil
	case []interface{}:
		// Handle array/slice
		slice := reflect.MakeSlice(targetType, 0, len(value.([]interface{})))
		for _, item := range value.([]interface{}) {
			elemType := targetType.Elem()
			elem, err := convertValue(item, elemType)
			if err != nil {
				return reflect.Value{}, err
			}
			slice = reflect.Append(slice, elem)
		}
		return slice, nil
	case map[string]interface{}:
		if targetType.Kind() == reflect.Map {
			m := reflect.MakeMap(targetType)
			keyType := targetType.Key()
			elemType := targetType.Elem()
			for k, v := range value.(map[string]interface{}) {
				key, err := convertValue(k, keyType)
				if err != nil {
					return reflect.Value{}, fmt.Errorf("failed to convert map key: %w", err)
				}
				elem, err := convertValue(v, elemType)
				if err != nil {
					return reflect.Value{}, err
				}
				m.SetMapIndex(key, elem)
			}
			return m, nil
		}
	}

	// Handle JSON raw message
	if targetType == reflect.TypeOf(json.RawMessage{}) {
		b, err := json.Marshal(value)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("failed to marshal JSON: %w", err)
		}
		return reflect.ValueOf(json.RawMessage(b)), nil
	}

	// Fallback: try direct assignment
	val := reflect.ValueOf(value)
	if val.Type().ConvertibleTo(targetType) {
		return val.Convert(targetType), nil
	}

	return reflect.Zero(targetType), nil
}

// ParseToolCalls parses tool calls from raw JSON content.
func ParseToolCalls(content string) ([]ToolCall, error) {
	var toolCalls []ToolCall

	// Try OpenAI format
	type OpenAIToolCall struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}

	type OpenAIResponse struct {
		ToolCalls []OpenAIToolCall `json:"tool_calls"`
	}

	var openResp OpenAIResponse
	if err := json.Unmarshal([]byte(content), &openResp); err == nil && len(openResp.ToolCalls) > 0 {
		for _, tc := range openResp.ToolCalls {
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
		return toolCalls, nil
	}

	// Try Anthropic format
	type AnthropicToolUse struct {
		ID    string                 `json:"id"`
		Name  string                 `json:"name"`
		Input map[string]interface{} `json:"input"`
	}

	type AnthropicResponse struct {
		ToolUses []AnthropicToolUse `json:"tool_use"`
	}

	var anthropicResp AnthropicResponse
	if err := json.Unmarshal([]byte(content), &anthropicResp); err == nil && len(anthropicResp.ToolUses) > 0 {
		for _, tu := range anthropicResp.ToolUses {
			toolCalls = append(toolCalls, ToolCall{
				ID:     tu.ID,
				Name:   tu.Name,
				Params: tu.Input,
			})
		}
		return toolCalls, nil
	}

	// Try direct array format
	type DirectToolCall struct {
		ID     string                 `json:"id"`
		Name   string                 `json:"name"`
		Params map[string]interface{} `json:"parameters"`
	}

	var directCalls []DirectToolCall
	if err := json.Unmarshal([]byte(content), &directCalls); err == nil && len(directCalls) > 0 {
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
