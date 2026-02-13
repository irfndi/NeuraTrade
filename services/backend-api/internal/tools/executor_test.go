package tools

import (
	"context"
	"testing"
)

func TestExecutor_Register(t *testing.T) {
	exec := NewExecutor(nil)

	err := exec.Register("test_tool", "A test tool", map[string]ParamSpec{
		"input": {Type: "string", Description: "Input string", Required: true},
	}, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return "result: " + params["input"].(string), nil
	})

	if err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	tool, ok := exec.Get("test_tool")
	if !ok {
		t.Fatal("tool not found after registration")
	}
	if tool.Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got '%s'", tool.Name)
	}
}

func TestExecutor_Register_Duplicate(t *testing.T) {
	exec := NewExecutor(nil)

	err := exec.Register("dup_tool", "A duplicate tool", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	err = exec.Register("dup_tool", "Another duplicate tool", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}
}

func TestExecutor_Unregister(t *testing.T) {
	exec := NewExecutor(nil)

	exec.Register("removeme", "To be removed", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	err := exec.Unregister("removeme")
	if err != nil {
		t.Fatalf("failed to unregister: %v", err)
	}

	_, ok := exec.Get("removeme")
	if ok {
		t.Fatal("tool still exists after unregister")
	}
}

func TestExecutor_Execute(t *testing.T) {
	exec := NewExecutor(nil)

	exec.Register("echo", "Echoes input", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return params, nil
	})

	result := exec.Execute(context.Background(), ToolCall{
		ID:     "call_1",
		Name:   "echo",
		Params: map[string]interface{}{"message": "hello"},
	})

	if result.Error != nil {
		t.Fatalf("execute returned error: %v", result.Error)
	}
}

func TestExecutor_Execute_NotFound(t *testing.T) {
	exec := NewExecutor(nil)

	result := exec.Execute(context.Background(), ToolCall{
		ID:   "call_1",
		Name: "nonexistent",
	})

	if result.Error == nil {
		t.Fatal("expected error for nonexistent tool, got nil")
	}
}

func TestExecutor_Execute_MissingRequired(t *testing.T) {
	exec := NewExecutor(nil)

	exec.Register("required_param", "Tool with required param", map[string]ParamSpec{
		"required_field": {Type: "string", Description: "Required field", Required: true},
	}, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return "ok", nil
	})

	result := exec.Execute(context.Background(), ToolCall{
		ID:     "call_1",
		Name:   "required_param",
		Params: map[string]interface{}{},
	})

	if result.Error == nil {
		t.Fatal("expected error for missing required param, got nil")
	}
}

func TestExecutor_ExecuteMultiple(t *testing.T) {
	exec := NewExecutor(nil)

	exec.Register("add", "Adds numbers", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return params, nil
	})

	exec.Register("multiply", "Multiplies numbers", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return params, nil
	})

	calls := []ToolCall{
		{ID: "1", Name: "add", Params: map[string]interface{}{"a": float64(2), "b": float64(3)}},
		{ID: "2", Name: "multiply", Params: map[string]interface{}{"a": float64(4), "b": float64(5)}},
	}

	results := exec.ExecuteMultiple(context.Background(), calls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestExecutor_List(t *testing.T) {
	exec := NewExecutor(nil)

	exec.Register("tool1", "First tool", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	exec.Register("tool2", "Second tool", nil, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	tools := exec.List()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestExecutor_GetToolSchema(t *testing.T) {
	exec := NewExecutor(nil)

	exec.Register("schema_tool", "Tool for schema test", map[string]ParamSpec{
		"required_param": {Type: "string", Description: "Required param", Required: true},
		"optional_param": {Type: "number", Description: "Optional param", Required: false},
	}, func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	schemas := exec.GetToolSchema()
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}

	schema := schemas[0]
	function := schema["function"].(map[string]interface{})
	if function["name"] != "schema_tool" {
		t.Errorf("expected name 'schema_tool', got '%s'", function["name"])
	}
}

func TestParseToolCalls_OpenAI(t *testing.T) {
	content := `{"tool_calls":[{"id":"call_123","type":"function","function":{"name":"get_price","arguments":"{\"symbol\":\"BTC/USDT\"}"}}]}`

	calls, err := ParseToolCalls(content)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "get_price" {
		t.Errorf("expected name 'get_price', got '%s'", calls[0].Name)
	}
	if calls[0].Params["symbol"] != "BTC/USDT" {
		t.Errorf("expected symbol 'BTC/USDT', got '%v'", calls[0].Params["symbol"])
	}
}

func TestParseToolCalls_Direct(t *testing.T) {
	content := `[{"id":"call_456","name":"place_order","parameters":{"symbol":"ETH/USDT","side":"buy","size":100}}]`

	calls, err := ParseToolCalls(content)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "place_order" {
		t.Errorf("expected name 'place_order', got '%s'", calls[0].Name)
	}
}

func TestParseToolCalls_Invalid(t *testing.T) {
	content := `{"not_tool_calls": true}`

	calls, err := ParseToolCalls(content)
	if err == nil {
		t.Fatal("expected error for invalid content, got nil")
	}
	if calls != nil {
		t.Errorf("expected nil calls, got %v", calls)
	}
}
